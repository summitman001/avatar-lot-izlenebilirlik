"""Fatura/irsaliye OCR: kural tabanli cikarim (regex + pozisyon sezgileri), model EGITIMI YOK.

Tesseract (pytesseract) ile calisir; harici API, API anahtari veya internet baglantisi
GEREKTIRMEZ (Tesseract imajda kurulu, bkz. Dockerfile). Her alan icin bir ONERI + guven skoru
(0-1) + okunan ham metin doner; hicbir alan "dogru" diye sunulmaz. Insan onayi olmadan hicbir
veritabani kaydi olusturulmaz -- bu kural burada DEGIL, Go tarafinda (invoice_import confirm
akisi, bkz. docs/invoice-ocr.md) uygulanir; bu modul SADECE oneri uretir.

Hicbir girdi (bozuk/bos dosya, hicbir alanin okunamamasi) exception FIRLATMAZ: boyle durumlarda
bos oneri + dusuk/sifir guven donulur (cagiran taraf -- main.py -- bunu HTTP 200 olarak yanitlar).
"""
from __future__ import annotations

import io
import re
from dataclasses import dataclass, field
from datetime import date
from decimal import Decimal, InvalidOperation

from PIL import Image, ImageOps

from .config import OCR


class UnsupportedFileError(Exception):
    """Dosya goruntu/PDF olarak hic acilamadi (bozuk veya desteklenmeyen format)."""


# ---------------------------------------------------------------------------
# Sonuc tipleri
# ---------------------------------------------------------------------------


@dataclass
class FieldResult:
    value: str | None
    confidence: float
    source_text: str
    warnings: list[str] = field(default_factory=list)


@dataclass
class LineResult:
    product_name: FieldResult
    quantity: FieldResult
    unit: FieldResult
    unit_price: FieldResult
    line_total: FieldResult
    lot_no: FieldResult
    expiry_date: FieldResult
    warnings: list[str] = field(default_factory=list)


@dataclass
class InvoiceResult:
    supplier_name: FieldResult
    document_no: FieldResult
    document_date: FieldResult
    currency: FieldResult
    lines: list[LineResult]
    overall_confidence: float
    raw_text: str
    warnings: list[str]


def _field(value: str | None, confidence: float, source: str, warnings: list[str] | None = None) -> FieldResult:
    return FieldResult(value=value, confidence=round(max(0.0, min(1.0, confidence)), 4), source_text=source,
                        warnings=warnings or [])


def _empty_result(warning: str) -> InvoiceResult:
    empty = _field(None, 0.0, "")
    return InvoiceResult(
        supplier_name=empty, document_no=_field(None, 0.0, ""), document_date=_field(None, 0.0, ""),
        currency=_field(None, 0.0, ""), lines=[], overall_confidence=0.0, raw_text="", warnings=[warning],
    )


# ---------------------------------------------------------------------------
# Turkce metin / sayi / tarih yardimcilari
# ---------------------------------------------------------------------------

_FOLD_MAP = {"ç": "c", "Ç": "C", "ğ": "g", "Ğ": "G", "ı": "i", "İ": "I", "ö": "o", "Ö": "O",
             "ş": "s", "Ş": "S", "ü": "u", "Ü": "U"}


def _ascii_fold(s: str) -> str:
    """Turkce karakterleri ASCII esdegerine cevirir, UZUNLUGU DEGISTIRMEZ (indeksler gecerli kalir)."""
    return "".join(_FOLD_MAP.get(c, c) for c in s)


_TR_DECIMAL_HIT = re.compile(r"\d,\d{2}(?!\d)")
_US_DECIMAL_HIT = re.compile(r"\d\.\d{2}(?!\d)")


def detect_number_format(text: str) -> str:
    """Belge genelinden ondalik ayiracini sezer: 'tr' (1.543,50) veya 'us' (1,543.50)."""
    tr_hits = len(_TR_DECIMAL_HIT.findall(text))
    us_hits = len(_US_DECIMAL_HIT.findall(text))
    return "us" if us_hits > tr_hits else "tr"


_NUMBER_CLEAN = re.compile(r"[^0-9.,]")


def parse_amount(raw: str, number_format: str) -> Decimal | None:
    """'1.543,50' (tr) veya '1,543.50' (us) -> Decimal. Ayristirilamazsa None (hata firlatmaz)."""
    s = _NUMBER_CLEAN.sub("", (raw or "").strip())
    if not s:
        return None
    if number_format == "tr":
        s = s.replace(".", "").replace(",", ".")
    else:
        s = s.replace(",", "")
    try:
        return Decimal(s)
    except InvalidOperation:
        return None


_DATE_NUMERIC = re.compile(r"\b(\d{1,2})[./-](\d{1,2})[./-](\d{4})\b")
_TR_MONTHS = {
    "ocak": 1, "subat": 2, "şubat": 2, "mart": 3, "nisan": 4, "mayis": 5, "mayıs": 5,
    "haziran": 6, "temmuz": 7, "agustos": 8, "ağustos": 8, "eylul": 9, "eylül": 9,
    "ekim": 10, "kasim": 11, "kasım": 11, "aralik": 12, "aralık": 12,
}
_DATE_TEXT = re.compile(r"\b(\d{1,2})\s+([A-Za-zÇĞİÖŞÜçğıöşü]+)\s+(\d{4})\b")


def parse_turkish_date(text: str) -> tuple[date, str] | None:
    """Metinde "01.11.2026" / "01/11/2026" / "1 Kasim 2026" arar. Bulamazsa None (hata firlatmaz)."""
    m = _DATE_NUMERIC.search(text or "")
    if m:
        d, mo, y = int(m.group(1)), int(m.group(2)), int(m.group(3))
        try:
            return date(y, mo, d), m.group(0)
        except ValueError:
            pass
    m = _DATE_TEXT.search(text or "")
    if m:
        mo = _TR_MONTHS.get(m.group(2).lower())
        if mo:
            try:
                return date(int(m.group(3)), mo, int(m.group(1))), m.group(0)
            except ValueError:
                pass
    return None


# ---------------------------------------------------------------------------
# Goruntu on isleme ve Tesseract cagrisi
# ---------------------------------------------------------------------------


def _load_image(content: bytes, content_type: str, filename: str) -> Image.Image:
    name = (filename or "").lower()
    if (content_type or "") == "application/pdf" or name.endswith(".pdf"):
        try:
            from pdf2image import convert_from_bytes
        except ImportError as e:  # pragma: no cover - poppler/pdf2image her zaman imajda kurulu
            raise UnsupportedFileError(f"PDF destegi yuklenemedi: {e}") from e
        pages = convert_from_bytes(content, first_page=1, last_page=1)
        if not pages:
            raise UnsupportedFileError("PDF'den sayfa okunamadi")
        return pages[0]
    try:
        img = Image.open(io.BytesIO(content))
        img.load()
        return img
    except Exception as e:
        raise UnsupportedFileError(f"dosya goruntu olarak acilamadi: {e}") from e


def preprocess_image(img: Image.Image) -> Image.Image:
    """Basit ve deterministik: gri tonlama, otomatik dondurme (OSD, best-effort), kontrast, boyutlandirma."""
    img = img.convert("L")
    try:
        import pytesseract

        osd = pytesseract.image_to_osd(img)
        m = re.search(r"Rotate:\s*(\d+)", osd)
        if m and int(m.group(1)):
            img = img.rotate(-int(m.group(1)), expand=True, fillcolor=255)
    except Exception:
        pass  # OSD guveni dusukse/calismazsa dondurme atlanir, akis bozulmaz
    img = ImageOps.autocontrast(img)
    if max(img.size) < 1600:
        ratio = 1600 / max(img.size)
        img = img.resize((max(1, int(img.width * ratio)), max(1, int(img.height * ratio))), Image.LANCZOS)
    return img


# Ayni satirdaki iki kelimenin arasinda bu kadar (piksel) bosluk varsa ayri bir SUTUN sayilir
# (parse_line_row'un bekledigi "2+ bosluk" ayracini, gercek kelime konumlarindan yeniden kurar).
# Tesseract'in kendi image_to_string duzen cikarimi genis sutun araliklarini TEK bosluga
# indirebiliyor (komsu sutunlarin OCR'da birbirine karismasi); bu yuzden satir ayristirma icin
# image_to_data'nin kelime kutucuklarina (left/width) dayanan bu yeniden kurulmus metin kullanilir.
_COLUMN_GAP_PX = 45


def run_tesseract(img: Image.Image) -> tuple[str, list[dict], list[str]]:
    import pytesseract

    raw_text = pytesseract.image_to_string(img, lang=OCR.languages)
    data = pytesseract.image_to_data(img, lang=OCR.languages, output_type=pytesseract.Output.DICT)
    words: list[dict] = []
    rows: dict[tuple[int, int, int], list[dict]] = {}
    for i, txt in enumerate(data["text"]):
        txt = txt.strip()
        if not txt:
            continue
        try:
            conf = float(data["conf"][i])
        except (TypeError, ValueError):
            continue
        if conf < 0:
            continue
        w = {"text": txt, "conf": conf / 100.0, "left": data["left"][i], "width": data["width"][i]}
        words.append(w)
        key = (data["block_num"][i], data["par_num"][i], data["line_num"][i])
        rows.setdefault(key, []).append(w)

    layout_lines: list[str] = []
    for key in sorted(rows.keys()):
        row = sorted(rows[key], key=lambda w: w["left"])
        parts: list[str] = []
        prev_right: int | None = None
        for w in row:
            if prev_right is not None:
                parts.append("   " if w["left"] - prev_right > _COLUMN_GAP_PX else " ")
            prev_right = w["left"] + w["width"]
            parts.append(w["text"])
        layout_lines.append("".join(parts))
    return raw_text, words, layout_lines


def _avg_conf(words: list[dict], source_text: str, fallback: float) -> float:
    tokens = {t.strip(".,:;€") for t in re.findall(r"\S+", (source_text or "").lower())}
    tokens.discard("")
    if not tokens:
        return fallback
    confs = [w["conf"] for w in words if w["text"].lower().strip(".,:;€") in tokens]
    if not confs:
        return fallback
    return sum(confs) / len(confs)


# ---------------------------------------------------------------------------
# Alan cikarimi
# ---------------------------------------------------------------------------

_DOC_NO_RE = re.compile(r"(FATURA NO|IRSALIYE NO|BELGE NO)\s*[:\-]?\s*([A-Z0-9\-/]{3,})", re.IGNORECASE)
_TOTAL_LABEL_RE = re.compile(r"(GENEL TOPLAM|TOPLAM)\s*[:\-]?\s*([\d.,]+)", re.IGNORECASE)
_LABEL_DATE_RE = re.compile(r"TARIH\s*[:\-]?", re.IGNORECASE)
_TRY_RE = re.compile(r"\bTL\b|\bTRY\b|₺", re.IGNORECASE)


def _extract_supplier(lines_text: list[str], words: list[dict], fallback_conf: float) -> FieldResult:
    for raw_line in lines_text[:5]:
        stripped = raw_line.strip()
        if len(stripped) < 3:
            continue
        has_letters = bool(re.search(r"[A-Za-zÇĞİÖŞÜçğıöşü]{3,}", stripped))
        if not has_letters:
            continue  # salt sayisal/tarih satirlarini (tarih, belge no vb.) atla
        return _field(stripped, _avg_conf(words, stripped, fallback_conf), stripped)
    return _field(None, 0.0, "")


def _extract_document_no(raw_text: str, words: list[dict], fallback_conf: float) -> FieldResult:
    folded = _ascii_fold(raw_text).upper()
    m = _DOC_NO_RE.search(folded)
    if not m:
        return _field(None, 0.0, "")
    value = raw_text[m.start(2):m.end(2)]
    source = raw_text[m.start(0):m.end(0)]
    return _field(value, _avg_conf(words, value, fallback_conf), source)


def _extract_document_date(raw_text: str, words: list[dict], fallback_conf: float) -> FieldResult:
    folded = _ascii_fold(raw_text).upper()
    m = _LABEL_DATE_RE.search(folded)
    found = None
    if m:
        found = parse_turkish_date(raw_text[m.end():m.end() + 40])
    if not found:
        found = parse_turkish_date(raw_text)
    if not found:
        return _field(None, 0.0, "")
    dt, source = found
    conf = _avg_conf(words, source, fallback_conf) * (1.0 if m else 0.85)
    return _field(dt.isoformat(), conf, source)


def _extract_currency(raw_text: str, fallback_conf: float) -> FieldResult:
    eur_hits = raw_text.count("€")
    try_hits = len(_TRY_RE.findall(raw_text))
    if eur_hits == 0 and try_hits == 0:
        return _field("EUR", fallback_conf * 0.5, "")
    value = "EUR" if eur_hits >= try_hits else "TRY"
    return _field(value, min(0.95, fallback_conf + 0.1), "€" if value == "EUR" else "TL")


def _extract_grand_total(raw_text: str, number_format: str) -> Decimal | None:
    folded = _ascii_fold(raw_text).upper()
    matches = list(_TOTAL_LABEL_RE.finditer(folded))
    if not matches:
        return None
    m = matches[-1]  # belgenin en altindaki toplam genelde genel toplamdir
    return parse_amount(raw_text[m.start(2):m.end(2)], number_format)


# ---------------------------------------------------------------------------
# Satir (urun) ayristirma
# ---------------------------------------------------------------------------

_QTY_UNIT_RE = re.compile(r"^(?P<qty>[\d.,]+)\s*(?P<unit>kg|adet|lt)$", re.IGNORECASE)
_LOT_RE = re.compile(r"^(?:LOT|PART[İI]|CHARGE)\s*[:\-]?\s*(?P<code>[A-Za-z0-9\-]+)$", re.IGNORECASE)
_SKT_RE = re.compile(r"^(?:SKT|SON\s*KULLANMA|TETT)\s*[:\-]?\s*(?P<date>.+)$", re.IGNORECASE)
_MONEY_TOKEN_RE = re.compile(r"^[\d.,]+€?$")


def parse_line_row(raw_line: str) -> dict | None:
    """Bir OCR satirini sutunlara ayirir (sutunlar 2+ bosluk/tab ile ayrilmis, bkz. invoicegen).

    Beklenen urun satiri: ad + miktar/birim (ornek "25,000 kg") + 2 parasal deger (birim fiyat,
    satir tutari) + (ayni satirda gecmisse) opsiyonel LOT:/SKT: sutunlari -- generator'de bunlar
    kendi satirlarinda (bkz. extract_invoice, _extract_lot_skt), ama baska bir OCR duzeninde ayni
    satirda gelirse de yakalanir. Esleşmezse None (satir atlanir -- baslik/toplam satirlari elenir).
    """
    cols = [c.strip() for c in re.split(r"\s{2,}|\t", raw_line.strip()) if c.strip()]
    if len(cols) < 3:
        return None
    qty = unit = lot_token = skt_token = None
    numeric_cols: list[str] = []
    name_cols: list[str] = []
    for c in cols:
        m = _QTY_UNIT_RE.match(c)
        if m and qty is None:
            qty, unit = m.group("qty"), m.group("unit").lower()
            continue
        m = _LOT_RE.match(c)
        if m:
            lot_token = m.group("code")
            continue
        m = _SKT_RE.match(c)
        if m:
            skt_token = m.group("date").strip()
            continue
        if _MONEY_TOKEN_RE.match(c.replace(" ", "")):
            numeric_cols.append(c)
            continue
        name_cols.append(c)
    if qty is None or len(numeric_cols) < 2 or not name_cols:
        return None
    return {
        "name": " ".join(name_cols),
        "qty_raw": qty, "unit": unit,
        "price_raw": numeric_cols[0], "total_raw": numeric_cols[-1],
        "lot_raw": lot_token, "skt_raw": skt_token,
        "raw_line": raw_line,
    }


def _extract_lot_skt(raw_line: str) -> tuple[str | None, str | None]:
    """Bir satirin tamami LOT:/SKT: etiketli sutunlardan ibaret mi diye bakar (urun satirindan
    SONRAKI satir, bkz. invoicegen meta satiri). Ikisi de yoksa (None, None) -- satir basit olarak
    yoksayilir, urun satirina HICBIR SEY eklenmez (uydurma yok)."""
    lot = skt = None
    for col in re.split(r"\s{2,}|\t", raw_line.strip()):
        col = col.strip()
        if not col:
            continue
        m = _LOT_RE.match(col)
        if m:
            lot = m.group("code")
            continue
        m = _SKT_RE.match(col)
        if m:
            skt = m.group("date").strip()
    return lot, skt


def _build_line_result(parsed: dict, number_format: str, words: list[dict], fallback_conf: float) -> LineResult:
    warnings: list[str] = []
    qty = parse_amount(parsed["qty_raw"], number_format)
    price = parse_amount(parsed["price_raw"], number_format)
    total = parse_amount(parsed["total_raw"], number_format)
    if qty is not None and price is not None and total is not None:
        computed = (qty * price).quantize(Decimal("0.01"))
        if abs(computed - total.quantize(Decimal("0.01"))) > Decimal("0.02"):
            warnings.append(
                f"TUTAR_UYUSMAZLIGI: {qty} x {price} = {computed}, okunan satir tutari {total}"
            )

    conf = _avg_conf(words, parsed["raw_line"], fallback_conf)
    skt_date = None
    skt_conf = 0.0
    if parsed["skt_raw"]:
        found = parse_turkish_date(parsed["skt_raw"])
        if found:
            skt_date = found[0].isoformat()
            skt_conf = conf

    return LineResult(
        product_name=_field(parsed["name"] or None, conf, parsed["name"]),
        quantity=_field(str(qty) if qty is not None else None, conf, parsed["qty_raw"]),
        unit=_field(parsed["unit"], conf, parsed["unit"] or ""),
        unit_price=_field(str(price) if price is not None else None, conf, parsed["price_raw"]),
        line_total=_field(str(total) if total is not None else None, conf, parsed["total_raw"], list(warnings)),
        lot_no=_field(parsed["lot_raw"], conf if parsed["lot_raw"] else 0.0, parsed["lot_raw"] or ""),
        expiry_date=_field(skt_date, skt_conf, parsed["skt_raw"] or ""),
        warnings=warnings,
    )


# ---------------------------------------------------------------------------
# Giris noktasi
# ---------------------------------------------------------------------------


def extract_invoice(content: bytes, content_type: str, filename: str) -> InvoiceResult:
    """Fatura/irsaliye goruntusunden (veya PDF'in ilk sayfasindan) alan onerileri cikarir.

    HATA FIRLATMAZ: goruntu acilamazsa / OCR basarisiz olursa / hicbir alan okunamazsa bos
    oneri + dusuk (0.0) guven doner (bkz. docs/invoice-ocr.md, "Sinirliliklar").
    """
    try:
        img = _load_image(content, content_type, filename)
        img = preprocess_image(img)
        raw_text, words, layout_lines = run_tesseract(img)
    except Exception as e:
        return _empty_result(f"OCR_BASARISIZ: dosya islenemedi ({e})")

    if not raw_text.strip():
        return _empty_result("METIN_BULUNAMADI: goruntuden hicbir metin okunamadi")

    number_format = detect_number_format(raw_text)
    overall_word_conf = (sum(w["conf"] for w in words) / len(words)) if words else 0.0
    lines_text = [l for l in raw_text.splitlines() if l.strip()]

    supplier = _extract_supplier(lines_text, words, overall_word_conf)
    doc_no = _extract_document_no(raw_text, words, overall_word_conf)
    doc_date = _extract_document_date(raw_text, words, overall_word_conf)
    currency = _extract_currency(raw_text, overall_word_conf)

    # Satir ayristirma raw_text DEGIL layout_lines (kelime kutucuklarindan yeniden kurulmus sutun
    # duzeni) uzerinden yapilir -- bkz. run_tesseract. Bir urun satirini HEMEN izleyen satir
    # sadece LOT:/SKT: etiketli sutunlardan ibaretse o satirin meta bilgisi sayilir (invoicegen
    # bunlari ayri bir satira cizer, bkz. docs/invoice-ocr.md).
    line_results = []
    i = 0
    while i < len(layout_lines):
        parsed = parse_line_row(layout_lines[i])
        if parsed:
            if i + 1 < len(layout_lines):
                lot, skt = _extract_lot_skt(layout_lines[i + 1])
                if lot or skt:
                    parsed["lot_raw"] = parsed["lot_raw"] or lot
                    parsed["skt_raw"] = parsed["skt_raw"] or skt
                    i += 1
            line_results.append(_build_line_result(parsed, number_format, words, overall_word_conf))
        i += 1

    doc_warnings: list[str] = []
    declared_total = _extract_grand_total(raw_text, number_format)
    if declared_total is not None and line_results:
        computed_total = sum(
            (Decimal(lr.line_total.value) for lr in line_results if lr.line_total.value is not None),
            Decimal(0),
        )
        if abs(computed_total - declared_total) > Decimal("0.02"):
            doc_warnings.append(
                f"TOPLAM_UYUSMAZLIGI: satir toplami {computed_total} != beyan edilen genel toplam {declared_total}"
            )
    if not line_results:
        doc_warnings.append("SATIR_BULUNAMADI: hicbir urun satiri okunamadi, manuel giris gerekebilir")

    field_confidences = [supplier.confidence, doc_no.confidence, doc_date.confidence, currency.confidence]
    for lr in line_results:
        field_confidences += [lr.product_name.confidence, lr.quantity.confidence, lr.unit_price.confidence,
                               lr.line_total.confidence]
    overall = round(sum(field_confidences) / len(field_confidences), 4) if field_confidences else 0.0
    if overall < OCR.confidence_medium:
        doc_warnings.append("DUSUK_GUVEN: genel guven dusuk, tum alanlari dikkatlice kontrol edin")

    return InvoiceResult(
        supplier_name=supplier, document_no=doc_no, document_date=doc_date, currency=currency,
        lines=line_results, overall_confidence=overall, raw_text=raw_text, warnings=doc_warnings,
    )
