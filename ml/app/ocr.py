"""Fatura/irsaliye OCR: kural tabanli cikarim (kelime konumlari + etiket/deger kurallari +
miktar x fiyat = tutar aritmetigi), model EGITIMI YOK.

Tesseract (pytesseract) ile calisir; harici API, API anahtari veya internet baglantisi
GEREKTIRMEZ (Tesseract imajda kurulu, bkz. Dockerfile). Her alan icin bir ONERI + guven skoru
(0-1) + okunan ham metin doner; hicbir alan "dogru" diye sunulmaz. Insan onayi olmadan hicbir
veritabani kaydi olusturulmaz -- bu kural burada DEGIL, Go tarafinda (invoice_import confirm
akisi, bkz. docs/invoice-ocr.md) uygulanir; bu modul SADECE oneri uretir.

Hicbir girdi (bozuk/bos dosya, hicbir alanin okunamamasi) exception FIRLATMAZ: boyle durumlarda
bos oneri + dusuk/sifir guven donulur (cagiran taraf -- main.py -- bunu HTTP 200 olarak yanitlar).

Akis: on isleme (EXIF yonu, golge, egiklik, tablo cizgileri) -> Tesseract kelime kutucuklari ->
GORSEL satirlar (Tesseract tablo sutunlarini ayri bloklara bolebiliyor; ayni yukseklikteki
kelimeler tek satirda birlestirilir) -> etiket/deger aramasi (etiketin sagi, solu, alti, ustu;
deger tipine gore dogrulanir) + tablo basligindan sutun cikarimi -> dogrulama.
"""
from __future__ import annotations

import io
import logging
import re
from dataclasses import dataclass, field, replace
from datetime import date
from decimal import Decimal, InvalidOperation
from statistics import median

from PIL import Image, ImageFilter, ImageOps

from .config import OCR

log = logging.getLogger(__name__)


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
    return InvoiceResult(
        supplier_name=_field(None, 0.0, ""), document_no=_field(None, 0.0, ""),
        document_date=_field(None, 0.0, ""), currency=_field(None, 0.0, ""), lines=[],
        overall_confidence=0.0, raw_text="", warnings=[warning],
    )


# ---------------------------------------------------------------------------
# Turkce metin / sayi / tarih yardimcilari
# ---------------------------------------------------------------------------

_FOLD_MAP = {"ç": "c", "Ç": "C", "ğ": "g", "Ğ": "G", "ı": "i", "İ": "I", "ö": "o", "Ö": "O",
             "ş": "s", "Ş": "S", "ü": "u", "Ü": "U"}


def _ascii_fold(s: str) -> str:
    """Turkce karakterleri ASCII esdegerine cevirir, UZUNLUGU DEGISTIRMEZ (indeksler gecerli kalir)."""
    return "".join(_FOLD_MAP.get(c, c) for c in s)


def _key(s: str) -> str:
    """Eslesme anahtari: Turkce harfler sadelestirilmis, BUYUK harf. Uzunlugu korur, yani anahtar
    uzerinde bulunan bir eslesmenin indeksleri ham metinde de gecerlidir."""
    return "".join(u if len(u := c.upper()) == 1 else c for c in _ascii_fold(s))


_TR_DECIMAL_HIT = re.compile(r"\d,\d{2}(?!\d)")
_US_DECIMAL_HIT = re.compile(r"\d\.\d{2}(?!\d)")


def detect_number_format(text: str) -> str:
    """Belge genelinden ondalik ayiracini sezer: 'tr' (1.543,50) veya 'us' (1,543.50)."""
    tr_hits = len(_TR_DECIMAL_HIT.findall(text))
    us_hits = len(_US_DECIMAL_HIT.findall(text))
    return "us" if us_hits > tr_hits else "tr"


_CURRENCY_RE = re.compile(r"[€₺$]|(?<![A-Za-z])(?:EURO|EUR|YTL|TRY|TL|USD)(?![A-Za-z])", re.IGNORECASE)
_DIGIT_FIX = str.maketrans({"O": "0", "o": "0", "l": "1", "I": "1", "|": "1"})


def _clean_number(raw: str) -> str | None:
    """Para birimi ve noktalama artiklarini atar, OCR'in rakam yerine okudugu harfleri (O->0,
    l/I->1) duzeltir. Sayi degilse None."""
    s = _CURRENCY_RE.sub("", raw or "").strip().strip("()[]{}*:;").replace(" ", "").replace("'", "")
    neg = s.startswith("-")
    s = s.lstrip("+-").rstrip(".,")
    if not s or not any(c.isdigit() for c in s) or not re.fullmatch(r"[\d.,OolI|]+", s):
        return None
    s = s.translate(_DIGIT_FIX)
    if not s[0].isdigit():
        return None
    return "-" + s if neg else s


def parse_amount(raw: str, number_format: str) -> Decimal | None:
    """'1.543,50' / '1,543.50' / '12,5' / '1 543,50 €' -> Decimal. Ayristirilamazsa None (hata firlatmaz).

    Iki ayirac birden varsa sondaki ondaliktir. Tek ayirac varsa ve ardindan 3 hane GELMIYORSA
    ondaliktir (12,5 / 12.50); tam 3 hane geliyorsa (1.500 / 25,000) belge geneli bicim karar verir.
    """
    s = _clean_number(raw)
    if s is None:
        return None
    neg = s.startswith("-")
    s = s.lstrip("-")
    dots, commas = s.count("."), s.count(",")
    if dots and commas:
        dec = "." if s.rfind(".") > s.rfind(",") else ","
    elif dots + commas == 1:
        sep = "." if dots else ","
        native = "," if number_format == "tr" else "."
        dec = sep if len(s.rpartition(sep)[2]) != 3 or sep == native else None
    else:
        dec = None  # ayiracsiz ya da yalniz binlik ayiracli (1.234.567)
    if dec is None:
        s = s.replace(".", "").replace(",", "")
    else:
        s = s.replace("," if dec == "." else ".", "").replace(dec, ".")
    try:
        value = Decimal(s)
    except InvalidOperation:
        return None
    return -value if neg else value


_MONTHS = {
    "OCAK": 1, "SUBAT": 2, "MART": 3, "NISAN": 4, "MAYIS": 5, "HAZIRAN": 6, "TEMMUZ": 7,
    "AGUSTOS": 8, "EYLUL": 9, "EKIM": 10, "KASIM": 11, "ARALIK": 12,
    "JANUARY": 1, "FEBRUARY": 2, "MARCH": 3, "APRIL": 4, "MAY": 5, "JUNE": 6, "JULY": 7,
    "AUGUST": 8, "SEPTEMBER": 9, "OCTOBER": 10, "NOVEMBER": 11, "DECEMBER": 12,
}


def _month(name: str) -> int | None:
    """Tam ya da en az 3 harflik kisaltilmis ay adi (KAS, Kasım, NOV...) -> ay numarasi."""
    n = name.rstrip(".")
    if len(n) < 3:
        return None
    return next((m for full, m in _MONTHS.items() if full.startswith(n)), None)


_DATE_DMY_RE = re.compile(r"(?<![\d.,/])(\d{1,2})\s?[./\-,]\s?(\d{1,2})\s?[./\-,]\s?(\d{4}|\d{2})(?!\d)")
_DATE_YMD_RE = re.compile(r"(?<![\d.,/])(20\d{2})\s?[./\-]\s?(\d{1,2})\s?[./\-]\s?(\d{1,2})(?!\d)")
_DATE_TEXT_RE = re.compile(r"(?<!\d)(\d{1,2})\s*\.?\s*([A-Z]{3,9})\.?\s*,?\s*(\d{4})(?!\d)")  # _key metni uzerinde


def _mkdate(y: int, m: int, d: int) -> date | None:
    if y < 100:
        y += 2000
    if not 2000 <= y <= 2099:
        return None
    try:
        return date(y, m, d)
    except ValueError:
        return None


def _iter_dates(text: str) -> list[tuple[int, int, date, str]]:
    """Metindeki gecerli tarihler: (baslangic, bitis, tarih, ham metin), metin sirasiyla.

    01.11.2026 / 01/11/2026 / 01-11-26 / 01,11.2026 (OCR virgulu) / 2026-11-01 / 1 Kasım 2026 /
    01 KAS 2026. Gun.ay gecersizse ay/gun (ABD) denenir.
    """
    found: list[tuple[int, int, date, str]] = []
    for m in _DATE_DMY_RE.finditer(text):
        a, b, y = int(m.group(1)), int(m.group(2)), int(m.group(3))
        d = _mkdate(y, b, a) or _mkdate(y, a, b)
        if d:
            found.append((m.start(), m.end(), d, m.group(0)))
    for m in _DATE_YMD_RE.finditer(text):
        d = _mkdate(int(m.group(1)), int(m.group(2)), int(m.group(3)))
        if d:
            found.append((m.start(), m.end(), d, m.group(0)))
    for m in _DATE_TEXT_RE.finditer(_key(text)):
        mo = _month(m.group(2))
        d = _mkdate(int(m.group(3)), mo, int(m.group(1))) if mo else None
        if d:
            found.append((m.start(), m.end(), d, text[m.start():m.end()]))
    found.sort(key=lambda f: f[0])
    return found


def parse_turkish_date(text: str) -> tuple[date, str] | None:
    """Metindeki ILK tarihi bulur (bkz. _iter_dates). Bulamazsa None (hata firlatmaz)."""
    found = _iter_dates(text or "")
    return (found[0][2], found[0][3]) if found else None


def _is_date_token(s: str) -> bool:
    s = s.strip(" ()[]:;")
    return bool(_DATE_DMY_RE.fullmatch(s) or _DATE_YMD_RE.fullmatch(s))


# ---------------------------------------------------------------------------
# Goruntu yukleme ve on isleme
# ---------------------------------------------------------------------------


def _load_image(content: bytes, content_type: str, filename: str) -> Image.Image:
    name = (filename or "").lower()
    if (content_type or "") == "application/pdf" or name.endswith(".pdf"):
        try:
            from pdf2image import convert_from_bytes
        except ImportError as e:  # pragma: no cover - poppler/pdf2image her zaman imajda kurulu
            raise UnsupportedFileError(f"PDF destegi yuklenemedi: {e}") from e
        pages = convert_from_bytes(content, first_page=1, last_page=1, dpi=300)
        if not pages:
            raise UnsupportedFileError("PDF'den sayfa okunamadi")
        return pages[0]
    try:
        img = Image.open(io.BytesIO(content))
        img.load()
        return img
    except Exception as e:
        raise UnsupportedFileError(f"dosya goruntu olarak acilamadi: {e}") from e


# Tesseract ~300 DPI'da en iyi okur: A4 icin uzun kenar ~3300 px. Kucuk goruntuler buyutulur, cok
# buyuk telefon fotograflari sure icin kucultulur.
_MIN_LONG_SIDE = 2500
_MAX_LONG_SIDE = 3600


def preprocess_image(img: Image.Image) -> Image.Image:
    """Deterministik on isleme: EXIF yonu, gri ton, 90/180 derece yon (OSD), olcek, golge
    giderme, egiklik duzeltme, tablo cizgilerini silme, kontrast. Her adim best-effort."""
    img = ImageOps.exif_transpose(img)  # telefon fotograflari yonu EXIF etiketinde tasir
    if img.mode in ("RGBA", "LA") or (img.mode == "P" and "transparency" in img.info):
        rgba = img.convert("RGBA")
        bg = Image.new("RGBA", rgba.size, (255, 255, 255, 255))
        img = Image.alpha_composite(bg, rgba)  # saydam arka plan siyaha donmesin
    img = img.convert("L")
    try:
        import pytesseract

        osd = pytesseract.image_to_osd(img)
        m = re.search(r"Rotate:\s*(\d+)", osd)
        if m and int(m.group(1)):
            img = img.rotate(-int(m.group(1)), expand=True, fillcolor=255)
    except Exception:
        pass  # OSD guveni dusukse/calismazsa dondurme atlanir, akis bozulmaz
    long_side = max(img.size)
    if long_side < _MIN_LONG_SIDE or long_side > _MAX_LONG_SIDE:
        ratio = (_MIN_LONG_SIDE if long_side < _MIN_LONG_SIDE else _MAX_LONG_SIDE) / long_side
        img = img.resize((max(1, int(img.width * ratio)), max(1, int(img.height * ratio))), Image.LANCZOS)
    for step in (_normalize_illumination, _deskew, _remove_rules):
        try:
            img = step(img)
        except Exception:
            pass
    return ImageOps.autocontrast(img, cutoff=1)


def _otsu(arr) -> int:
    import numpy as np

    hist = np.bincount(arr.ravel(), minlength=256).astype(float)
    total = hist.sum()
    if total == 0:
        return 128
    levels = np.arange(256)
    w0 = np.cumsum(hist)
    w1 = total - w0
    m0 = np.cumsum(hist * levels)
    mean_all = m0[-1]
    with np.errstate(divide="ignore", invalid="ignore"):
        between = (mean_all * w0 / total - m0) ** 2 / (w0 * w1)
    between[~np.isfinite(between)] = 0
    return int(between.argmax())


def _normalize_illumination(img: Image.Image) -> Image.Image:
    """Golge/isik gradyanini giderir: kagit arka plani (yazidan arindirilmis bulanik kopya) ile
    bolunur. Temiz taramada arka plan zaten beyaz oldugundan goruntu degismez."""
    import numpy as np

    small = img.reduce(8) if min(img.size) >= 160 else img
    bg = small.filter(ImageFilter.MaxFilter(5)).filter(ImageFilter.GaussianBlur(2)).resize(img.size, Image.BILINEAR)
    a = np.asarray(img, dtype=np.float32)
    b = np.maximum(np.asarray(bg, dtype=np.float32), 1.0)
    return Image.fromarray(np.clip(a / b * 255.0, 0, 255).astype(np.uint8))


def _deskew(img: Image.Image) -> Image.Image:
    """+-10 dereceye kadar egikligi duzeltir: yazi satirlarinin yatay izdusumu en keskin oldugu
    aci aranir (once 0.5, sonra 0.1 derece adimla)."""
    import numpy as np

    small = img.copy()
    small.thumbnail((1200, 1200))
    arr = np.asarray(small, dtype=np.uint8)
    ink = Image.fromarray(((arr < _otsu(arr)) * 255).astype(np.uint8))

    def sharpness(angle: float) -> float:
        rotated = np.asarray(ink.rotate(angle, resample=Image.NEAREST, fillcolor=0), dtype=np.float32)
        return float(np.sum(np.diff(rotated.sum(axis=1)) ** 2))

    best = max((a / 2 for a in range(-20, 21)), key=sharpness)
    best = max((best + a / 10 for a in range(-5, 6)), key=sharpness)
    if abs(best) < 0.2:
        return img
    return img.rotate(best, resample=Image.BICUBIC, expand=True, fillcolor=255)


def _long_runs(ink, k: int):
    """Her satirda en az k piksel boyunca kesintisiz suren murekkep piksellerini isaretler."""
    import numpy as np

    h, w = ink.shape
    if w <= k:
        return np.zeros_like(ink)
    c = np.zeros((h, w + 1), dtype=np.int32)
    np.cumsum(ink, axis=1, out=c[:, 1:])
    full = (c[:, k:] - c[:, :-k]) == k  # [j, j+k) penceresi tamamen murekkep
    d = np.zeros((h, w + 1), dtype=np.int32)
    d[:, : full.shape[1]] += full
    d[:, k: k + full.shape[1]] -= full
    return np.cumsum(d, axis=1)[:, :w] > 0


def _remove_rules(img: Image.Image) -> Image.Image:
    """Tablo/kutu cizgilerini siler: harflere yapisik cizgiler Tesseract'in rakamlari '|1' gibi
    okumasina ve hucreleri birlestirmesine yol aciyor. Yalniz harf boyunu cok asan duz cizgiler."""
    import numpy as np

    a = np.asarray(img, dtype=np.uint8)
    ink = a < _otsu(a)
    h, w = ink.shape
    mask = _long_runs(ink, max(40, w // 25)) | _long_runs(ink.T, max(40, h // 30)).T
    if not mask.any():
        return img
    # cizginin kenarlarindaki yumusatilmis gri pikseller de gitsin (kalan soluk iz Tesseract'i yaniltiyor)
    mask_img = Image.fromarray((mask * 255).astype(np.uint8)).filter(ImageFilter.MaxFilter(5))
    out = a.copy()
    out[np.asarray(mask_img) > 0] = 255
    return Image.fromarray(out)


# ---------------------------------------------------------------------------
# Tesseract kelimeleri ve gorsel satirlar
# ---------------------------------------------------------------------------


@dataclass
class _Word:
    text: str
    conf: float
    left: int
    top: int
    width: int
    height: int
    line: tuple = ()  # Tesseract (sayfa, blok, paragraf, satir): ayni Tesseract satirindaki kelimeler

    @property
    def right(self) -> int:
        return self.left + self.width

    @property
    def bottom(self) -> int:
        return self.top + self.height

    @property
    def cx(self) -> float:
        return self.left + self.width / 2


_JUNK_RE = re.compile(r"^[|_\-—–=~.,:;'\"`!*•·°¬]+$")  # cizgi/noktalama artiklari


def run_tesseract(img: Image.Image, psm: int = 3) -> list[_Word]:
    import pytesseract

    data = pytesseract.image_to_data(img, lang=OCR.languages, config=f"--psm {psm}",
                                     output_type=pytesseract.Output.DICT)
    words: list[_Word] = []
    for i, txt in enumerate(data["text"]):
        txt = (txt or "").strip()
        if not txt or _JUNK_RE.match(txt):
            continue
        try:
            conf = float(data["conf"][i]) / 100.0
        except (TypeError, ValueError):
            continue
        if conf < 0.1:
            continue
        words.append(_Word(txt, conf, data["left"][i], data["top"][i], data["width"][i], data["height"][i],
                           (data["page_num"][i], data["block_num"][i], data["par_num"][i], data["line_num"][i])))
    return words


class _Row:
    """Ayni yukseklikteki kelimeler (soldan saga). Genis bosluklar hucreleri ayirir; metin
    icinde hucre arasi 3 bosluk, kelime arasi 1 boslukla temsil edilir."""

    def __init__(self, words: list[_Word]):
        self.words = sorted(words, key=lambda w: w.left)
        self.height = float(median(w.height for w in self.words))
        self.top = min(w.top for w in self.words)
        self.bottom = max(w.bottom for w in self.words)
        gap_limit = max(1.2 * self.height, 6.0)
        parts: list[str] = []
        self.spans: list[tuple[int, int]] = []
        self.cells: list[list[int]] = [[]]
        self.cell_of: list[int] = []
        pos = 0
        for i, w in enumerate(self.words):
            if i:
                wide = w.left - self.words[i - 1].right > gap_limit
                if wide:
                    self.cells.append([])
                sep = "   " if wide else " "
                parts.append(sep)
                pos += len(sep)
            self.cells[-1].append(i)
            self.cell_of.append(len(self.cells) - 1)
            self.spans.append((pos, pos + len(w.text)))
            parts.append(w.text)
            pos += len(w.text)
        self.text = "".join(parts)
        self.key = _key(self.text)

    def word_range(self, start: int, end: int) -> list[int]:
        return [i for i, (a, b) in enumerate(self.spans) if a < end and b > start]

    def cell_words(self, ci: int) -> list[_Word]:
        return [self.words[i] for i in self.cells[ci]]


def _x_overlap(a_left: float, a_right: float, b_left: float, b_right: float) -> float:
    return min(a_right, b_right) - max(a_left, b_left)


def _build_rows(words: list[_Word]) -> list[_Row]:
    """Tesseract satirlarini gorsel satirlarda birlestirir: Tesseract tablo sutunlarini ya da
    etiket/deger kutularini ayri bloklara bolebiliyor; dikeyde ortusen ve yatayda cakismayan
    satirlar ayni gorsel satirdir."""
    lines: dict[tuple, list[_Word]] = {}
    for w in words:
        lines.setdefault(w.line, []).append(w)
    groups = sorted(lines.values(), key=lambda ws: median(w.top + w.height / 2 for w in ws))
    rows: list[list[_Word]] = []
    for ws in groups:
        top, bottom = min(w.top for w in ws), max(w.bottom for w in ws)
        for row in reversed(rows[-6:]):
            rtop, rbottom = min(w.top for w in row), max(w.bottom for w in row)
            overlap = min(bottom, rbottom) - max(top, rtop)
            if overlap < 0.5 * min(bottom - top, rbottom - rtop):
                continue
            if any(_x_overlap(a.left, a.right, b.left, b.right) > 0.3 * min(a.width, b.width) for a in ws for b in row):
                continue
            row.extend(ws)
            break
        else:
            rows.append(list(ws))
    built = [_Row(r) for r in rows if r]
    built.sort(key=lambda r: (r.top + r.bottom) / 2)
    return built


def _words_from_text(text: str, y: int = 0, char_w: int = 12, height: int = 20) -> list[_Word]:
    """Duz metin satirini sahte kelime kutucuklarina cevirir (parse_line_row ve testler icin)."""
    return [_Word(m.group(0), 1.0, m.start() * char_w, y, len(m.group(0)) * char_w, height, (0, y))
            for m in re.finditer(r"\S+", text or "")]


def _mean_conf(words: list[_Word], fallback: float = 0.0) -> float:
    return sum(w.conf for w in words) / len(words) if words else fallback


# ---------------------------------------------------------------------------
# Etiket -> deger aramasi (sag, sol, alt, ust)
# ---------------------------------------------------------------------------

_SEP_STRIP = " :;.#-=_|"
_LABEL_TOKEN_RE = re.compile(r"^[A-Za-zÇĞİÖŞÜçğıöşü.]+:$")  # "Tarih:", "No:" -> yeni bir etiket basliyor


def _right_tokens(row: _Row, last: int, match_end: int) -> list[tuple[str, _Word]]:
    toks: list[tuple[str, _Word]] = []
    a, b = row.spans[last]
    if match_end < b:  # etiketle deger bitisik: "No:ABC123"
        tail = row.text[match_end:b].strip(_SEP_STRIP)
        if tail:
            toks.append((tail, row.words[last]))
    ci = row.cell_of[last]
    rest = [i for i in row.cells[ci] if i > last]
    # etiket hucresinde gecerli deger olmayabilir ("KDV(%1)", OCR'in bozdugu "Tutan"): yan hucre de
    # aday; secici ilk GECERLI degeri alir, yeni bir etiket ("Tarih:") gorunce durulur
    if ci + 1 < len(row.cells):
        nxt = row.cells[ci + 1]
        if row.words[nxt[0]].left - row.words[last].right < 20 * row.height:
            rest = rest + nxt
    for i in rest:
        w = row.words[i]
        if toks and _LABEL_TOKEN_RE.match(w.text):
            break
        toks.append((w.text, w))
    return toks


def _left_tokens(row: _Row, first: int, match_start: int) -> list[tuple[str, _Word]]:
    ci = row.cell_of[first]
    toks = [(row.words[i].text, row.words[i]) for i in row.cells[ci] if i < first]
    a, _b = row.spans[first]
    if match_start > a:  # "ABC123Fatura"
        head = row.text[a:match_start].strip(_SEP_STRIP)
        if head:
            toks.append((head, row.words[first]))
    if not toks and ci > 0:
        toks = [(w.text, w) for w in row.cell_words(ci - 1)]
    return toks


def _vertical_tokens(rows: list[_Row], ri: int, first: int, last: int, direction: int,
                     skip_rows: set[int]) -> list[tuple[str, _Word]]:
    """Etiketin hemen altindaki (direction=+1) ya da ustundeki (-1) satirda, etiketle yatayda
    ortusen hucre."""
    row = rows[ri]
    x0, x1 = row.words[first].left, row.words[last].right
    h = row.height
    for k in (1, 2):
        j = ri + direction * k
        if not 0 <= j < len(rows) or j in skip_rows:
            break
        r2 = rows[j]
        dist = r2.top - row.bottom if direction > 0 else row.top - r2.bottom
        if dist > 2.5 * h:
            break
        best, best_ov = None, -h
        for ci in range(len(r2.cells)):
            ws = r2.cell_words(ci)
            ov = _x_overlap(x0, x1, ws[0].left, ws[-1].right)
            if ov > best_ov:
                best, best_ov = ws, ov
        if best:
            return [(w.text, w) for w in best]
    return []


@dataclass
class _Found:
    score: float
    value: object
    words: list[_Word]
    source: str


def _label_value(rows: list[_Row], hits: list[tuple[int, int, int, float]], pick,
                 skip_rows: set[int] = frozenset()) -> _Found | None:
    """hits: (satir, eslesme bas, eslesme son, etiket agirligi). Her etiket icin sag, sol, alt ve
    ust adaylari pick ile dogrulanir; en yuksek puanli deger doner.

    Etiketten sonra ':' varsa deger buyuk olasilikla sagdadir; yoksa sol da guclu bir adaydir
    ("ABC123   Fatura No" duzeni)."""
    best: _Found | None = None
    for ri, ms, me, weight in hits:
        if weight <= 0:
            continue
        row = rows[ri]
        idx = row.word_range(ms, me)
        if not idx:
            continue
        first, last = idx[0], idx[-1]
        has_sep = row.key[me:me + 3].lstrip()[:1] in (":", "=", "#")
        options = (
            (1.0 if has_sep else 0.9, _right_tokens(row, last, me), False),
            (0.5 if has_sep else 0.85, _left_tokens(row, first, ms), True),
            (0.8, _vertical_tokens(rows, ri, first, last, +1, skip_rows), False),
            (0.6, _vertical_tokens(rows, ri, first, last, -1, skip_rows), False),
        )
        for dscore, toks, from_end in options:
            got = pick(toks, from_end) if toks else None
            if got is None:
                continue
            score = weight * dscore
            if best is None or score > best.score + 1e-9:
                best = _Found(score, got[0], got[1], got[2])
    return best


def _hits(rows: list[_Row], pattern: re.Pattern, weight, skip_rows: set[int] = frozenset()):
    """pattern'in tum eslesmeleri; weight sabit ya da (eslesme, satir) -> agirlik fonksiyonu."""
    out = []
    for ri, row in enumerate(rows):
        if ri in skip_rows:
            continue
        for m in pattern.finditer(row.key):
            out.append((ri, m.start(), m.end(), weight(m, row) if callable(weight) else weight))
    return out


# ---------------------------------------------------------------------------
# Belge no, tarih, tedarikci, para birimi, toplamlar
# ---------------------------------------------------------------------------

_DOC_NO_LABELS = (
    (re.compile(r"(?<![A-Z])FATURA\s*(?:NO|N0|NR|NUMARASI|NUMARA|#)(?![A-Z0-9])"), 1.0),
    (re.compile(r"(?<![A-Z])BELGE\s*(?:NO|N0|NR|NUMARASI)(?![A-Z0-9])"), 0.95),
    (re.compile(r"(?<![A-Z])SERI\s*[-/]?\s*SIRA\s*(?:NO|N0|NR)(?![A-Z0-9])"), 0.95),
    (re.compile(r"(?<![A-Z])FAT\.?\s*NO(?![A-Z0-9])"), 0.9),
    (re.compile(r"(?<![A-Z])(?:SEVK\s*)?IRSALIYE(?:SI)?\s*(?:NO|N0|NR|NUMARASI)(?![A-Z0-9])"), 0.9),
    (re.compile(r"(?<![A-Z])INVOICE\s*(?:NO|NUMBER|NR|#)(?![A-Z0-9])"), 0.9),
    (re.compile(r"(?<![A-Z])(?:EVRAK|FIS|DOKUMAN|DOCUMENT)\s*(?:NO|NR|NUMARASI|NUMBER)(?![A-Z0-9])"), 0.8),
)
# Matbu fatura: "Seri: A  Sira No: 123456" -> "A-123456"
_SERI_RE = re.compile(r"(?<![A-Z])SERI\s*[:\-]?\s*([A-Z]{1,3})\s+(?:SIRA\s*)?(?:NO|N0|NR)?\s*[:.\-]?\s*(\d{4,})(?!\d)")
_DOC_TOKEN_RE = re.compile(r"^[A-Z0-9](?:[A-Z0-9\-/_.]*[A-Z0-9])?$")
_UUID_RE = re.compile(r"^[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}$")
_AMOUNT_LIKE_RE = re.compile(r"^\d{1,3}(?:[.,]\d{3})*[.,]\d{2}$")
# e-Fatura/e-Arsiv numarasi: 3 karakter on ek + yil + 9 hane sira (ABC2026000000123)
_GIB_RE = re.compile(r"^[A-Z0-9]{3}20\d{11}$")
_GIB_TAIL_FIX = str.maketrans({"O": "0", "D": "0", "Q": "0", "I": "1", "L": "1", "|": "1", "S": "5", "B": "8",
                               "Z": "2", "G": "6"})


def _gib_number(text: str) -> str | None:
    """16 karakterlik GIB numarasi; sira kismindaki OCR harf/rakam karisikliklarini duzeltir."""
    k = _key(text.strip(_SEP_STRIP + "()"))
    if len(k) != 16:
        return None
    fixed = k[:3] + k[3:].translate(_GIB_TAIL_FIX)
    return fixed if _GIB_RE.match(fixed) else None


def _doc_no_value(text: str) -> str | None:
    t = text.strip(_SEP_STRIP + "()[]{}'\"")
    k = _key(t)
    if not 3 <= len(t) <= 40 or not any(c.isdigit() for c in k):
        return None
    if not _DOC_TOKEN_RE.match(k) or _UUID_RE.match(k) or _AMOUNT_LIKE_RE.match(t) or _is_date_token(t):
        return None
    return _gib_number(t) or t


def _pick_doc_no(toks, from_end):
    for text, w in (reversed(toks) if from_end else toks):
        v = _doc_no_value(text)
        if v:
            return v, [w], text
    return None


def _extract_document_no(rows: list[_Row], skip_rows: set[int], fallback_conf: float) -> FieldResult:
    hits = [h for pattern, weight in _DOC_NO_LABELS for h in _hits(rows, pattern, weight, skip_rows)]
    found = _label_value(rows, hits, _pick_doc_no, skip_rows)
    for ri, row in enumerate(rows):
        if ri in skip_rows:
            continue
        for m in _SERI_RE.finditer(row.key):
            score = 0.9
            if found is None or score > found.score:
                ws = [row.words[i] for i in row.word_range(m.start(), m.end())]
                found = _Found(score, f"{m.group(1)}-{m.group(2)}", ws, row.text[m.start():m.end()])
    if found is None:  # etiketsiz: belgede GIB bicimli bir numara
        for ri, row in enumerate(rows):
            if ri in skip_rows:
                continue
            for w in row.words:
                v = _gib_number(w.text)
                if v:
                    found = _Found(0.75, v, [w], w.text)
                    break
            if found:
                break
    if found is None:
        return _field(None, 0.0, "")
    return _field(str(found.value), _mean_conf(found.words, fallback_conf) * min(1.0, found.score), found.source)


_DATE_LABEL_RE = re.compile(
    r"(?<![A-Z])(?:(FATURA|BELGE|DUZENLE(?:N)?ME|IRSALIYE|EVRAK|ISLEM|FIILI\s*SEVK|SEVK|SIPARIS|TESLIM|KAYIT"
    r"|VADE|SON\s*ODEME|ODEME|SON\s*KULLANMA|SON\s*TUKETIM|URETIM|DOGUM)\s*)?TARIHI?(?![A-Z])"
    r"|(?<![A-Z])(?:(INVOICE|DUE|DELIVERY|ORDER|EXPIRY)\s*)?DATE(?![A-Z])"
)
# Belgenin kendi tarihi olmayan tarihler (vade, SKT...) 0 agirlik alir.
_DATE_LABEL_WEIGHT = {
    None: 0.9, "FATURA": 1.0, "BELGE": 1.0, "DUZENLEME": 1.0, "DUZENLENME": 1.0, "INVOICE": 1.0,
    "IRSALIYE": 0.95, "EVRAK": 0.95, "ISLEM": 0.9, "SEVK": 0.7, "FIILI SEVK": 0.7, "KAYIT": 0.6,
    "DELIVERY": 0.5, "SIPARIS": 0.4, "TESLIM": 0.4, "ORDER": 0.4,
}
_NON_DOC_DATE_RE = re.compile(r"(?<![A-Z])(?:VADE|ODEME|S\.?\s?K\.?\s?T|S\.?\s?T\.?\s?T|TETT|SON\s*KULLANMA"
                              r"|SON\s*TUKETIM|URETIM|EXP)")


def _date_label_weight(m: re.Match, _row: _Row) -> float:
    g = m.group(1) or m.group(2)
    return _DATE_LABEL_WEIGHT.get(re.sub(r"\s+", " ", g) if g else None, 0.0)


def _pick_date(toks, from_end):
    text = " ".join(t for t, _ in toks)
    found = _iter_dates(text)
    if not found:
        return None
    _s, _e, d, src = found[-1] if from_end else found[0]
    words = [w for t, w in toks if t in src] or [toks[0][1]]
    return d.isoformat(), words, src


def _extract_document_date(rows: list[_Row], skip_rows: set[int], fallback_conf: float) -> FieldResult:
    found = _label_value(rows, _hits(rows, _DATE_LABEL_RE, _date_label_weight, skip_rows), _pick_date, skip_rows)
    if found is None:  # etiketsiz: tablo disinda, vade/SKT yaninda olmayan ilk tarih
        for ri, row in enumerate(rows):
            if ri in skip_rows:
                continue
            for s, e, d, src in _iter_dates(row.text):
                if _NON_DOC_DATE_RE.search(row.key[max(0, s - 25):s]):
                    continue
                ws = [row.words[i] for i in row.word_range(s, e)]
                found = _Found(0.75, d.isoformat(), ws, src)
                break
            if found:
                break
    if found is None:
        return _field(None, 0.0, "")
    return _field(str(found.value), _mean_conf(found.words, fallback_conf) * min(1.0, found.score), found.source)


_SUPPLIER_LABEL_RE = re.compile(r"(?<![A-Z])(?:SATICI|TEDARIKCI|GONDEREN|DUZENLEYEN|FIRMA\s*(?:ADI|UNVANI)"
                                r"|UNVANI?|SUPPLIER|SELLER|VENDOR)(?![A-Z])")
_BUYER_RE = re.compile(r"(?<![A-Z])(?:SAYIN|ALICI|MUSTERI|TESLIM\s*ALAN|BILL\s*TO|SHIP\s*TO|CUSTOMER|BUYER)(?![A-Z])")
_NOT_NAME_RE = re.compile(
    r"(?<![A-Z])(?:E\s?-?\s?FATURA|E\s?-?\s?ARSIV|FATURA|IRSALIYE|SEVK|ORNEK|NUSHA|ASIL|SURET|VKN|V\.\s?K\.\s?N|TCKN"
    r"|VERGI|V\.\s?D|TEL|TELEFON|FAX|FAKS|GSM|E\s?-?\s?POSTA|E\s?-?\s?MAIL|MAIL|WEB|WWW|HTTPS?|MERSIS|SICIL"
    r"|ADRES|MAH|MAHALLESI|CAD|CADDESI|SOK|SOKAK|SK|BULVARI?|BLV|KAT|DAIRE|POSTA\s*KODU|IBAN|BANKA|SUBE"
    r"|ETTN|SENARYO|OZELLESTIRME|TIPI|TARIH|TARIHI|SAAT|SAYFA|TEMELFATURA|TICARIFATURA|KDV|TOPLAM|TUTAR|NO"
    r"|ULKE|TURKIYE|IL|ILCE|SAYIN|ALICI|MUSTERI|SATICI|TEDARIKCI)(?![A-Z])"
)
_COMPANY_RE = re.compile(
    r"(?<![A-Z])(?:A\.?\s?S\.?|ANONIM|LTD\.?|LIMITED|STI\.?|SIRKETI|SIRKET|TIC\.?|TICARET|SAN\.?|SANAYI|GIDA"
    r"|HOLDING|KOOP\.?|KOOPERATIFI?|ENTEGRE|KASAP|KASAPLIK|CIFTLIGI|CIFTLIK|TARIM|HAYVANCILIK|SUT|ET|ETCILIK"
    r"|BESI|URUNLERI|PAZARLAMA|PAZ\.?|ITHALAT|IHRACAT|INC|GMBH|LLC|CORP|SARKUTERI|MARKET|TOPTAN|DAGITIM"
    r"|MEZBAHA|MEZBAHACILIK)(?![A-Z])"
)
_LEGAL_END_RE = re.compile(r"(?:STI\.?|A\.?\s?S\.?|LTD\.?|SIRKETI|INC\.?|GMBH|LLC)\s*$")


def _name_cell_ok(text: str) -> bool:
    letters = sum(c.isalpha() for c in text)
    compact = len(text.replace(" ", ""))
    if letters < 3 or letters < 0.6 * compact or "@" in text:
        return False
    k = _key(text)
    if _NOT_NAME_RE.search(k):
        return False
    return not (re.search(r"\S\s*/\s*\S", text) and not _COMPANY_RE.search(k))  # "BORNOVA / IZMIR"


def _pick_name(toks, _from_end):
    text = " ".join(t for t, _ in toks).strip(_SEP_STRIP.replace(".", ""))  # "Ltd. Sti." noktasi kalir
    if sum(c.isalpha() for c in text) >= 3 and not _LABEL_TOKEN_RE.match(text):
        return text, [w for _, w in toks], text
    return None


def _extract_supplier(rows: list[_Row], limit: int, skip_rows: set[int], fallback_conf: float) -> FieldResult:
    """Once acik etiket (Satici:, Tedarikci:...), yoksa belge ust kismindaki firma adi: firma
    ekleri (A.S., Ltd. Sti., Gida...) ve buyuk yazi puan kazanir; adres/vergi/iletisim satirlari ve
    alici (SAYIN ...) blogu elenir."""
    head = [ri for ri in range(limit) if ri not in skip_rows]
    # "Alici Unvani:" alicinin adidir, tedarikcinin degil
    seller_weight = lambda m, row: 0.0 if _BUYER_RE.search(row.key[:m.start()]) else 1.0  # noqa: E731
    labeled = _label_value(rows, [h for h in _hits(rows, _SUPPLIER_LABEL_RE, seller_weight, skip_rows) if h[0] < limit],
                           _pick_name, skip_rows)
    if labeled and labeled.score >= 0.8:
        return _field(str(labeled.value), _mean_conf(labeled.words, fallback_conf), labeled.source)

    heights = [w.height for ri in head for w in rows[ri].words]
    med_h = median(heights) if heights else 1.0
    page_w = max((w.right for ri in head for w in rows[ri].words), default=1)
    buyer_zones = []
    for ri in head:
        for m in _BUYER_RE.finditer(rows[ri].key):
            idx = rows[ri].word_range(m.start(), m.end())
            if idx:
                w = rows[ri].words[idx[0]]
                buyer_zones.append((w.left - 2 * med_h, w.left + 0.45 * page_w, w.top - med_h, w.top + 7 * med_h))

    best = None
    for rank, ri in enumerate(head):
        row = rows[ri]
        for ci in range(len(row.cells)):
            ws = row.cell_words(ci)
            text = " ".join(w.text for w in ws).strip(" :;,-")
            if not _name_cell_ok(text):
                continue
            if any(x0 <= ws[0].left <= x1 and y0 <= ws[0].top <= y1 for x0, x1, y0, y1 in buyer_zones):
                continue
            company = bool(_COMPANY_RE.search(_key(text)))
            score = 1.0 - 0.6 * rank / max(1, len(head)) + (2.0 if company else 0.0)
            if median(w.height for w in ws) > 1.15 * med_h:
                score += 0.7
            if best is None or score > best[0] + 1e-9:
                best = (score, ri, ci, company)
    if best is None:
        if labeled:
            return _field(str(labeled.value), _mean_conf(labeled.words, fallback_conf) * labeled.score, labeled.source)
        return _field(None, 0.0, "")

    _score, ri, ci, company = best
    words = rows[ri].cell_words(ci)
    text = " ".join(w.text for w in words)
    # Iki satira bolunmus unvan: "AKYAZI ET URUNLERI GIDA" / "SANAYI VE TICARET LTD. STI."
    below = _neighbor_name_cell(rows, ri, words, +1, skip_rows)
    if below and not _LEGAL_END_RE.search(_key(text)) and _COMPANY_RE.search(_key(below[1])):
        words, text = words + below[0], f"{text} {below[1]}"
    above = _neighbor_name_cell(rows, ri, words, -1, skip_rows)
    if above and _CONTINUATION_START_RE.match(_key(text)) and not _LEGAL_END_RE.search(_key(above[1])):
        words, text = above[0] + words, f"{above[1]} {text}"
    return _field(text, _mean_conf(words, fallback_conf) * (1.0 if company else 0.7), text)


# Unvanin ikinci satiri bu kelimelerle baslar: "... GIDA" / "SANAYI VE TICARET LTD. STI."
_CONTINUATION_START_RE = re.compile(r"(?:SAN\.?|SANAYI|TIC\.?|TICARET|LTD\.?|LIMITED|ANONIM|SIRKETI|STI\.?|VE"
                                    r"|ITH\.?|IHR\.?|PAZ\.?|PAZARLAMA|A\.?\s?S\.?)(?![A-Z])")


def _neighbor_name_cell(rows: list[_Row], ri: int, words: list[_Word], direction: int, skip_rows: set[int]):
    j = ri + direction
    if not 0 <= j < len(rows) or j in skip_rows:
        return None
    row, r2 = rows[ri], rows[j]
    if (r2.top - row.bottom if direction > 0 else row.top - r2.bottom) > 1.0 * row.height:
        return None
    h = median(w.height for w in words)
    for ci in range(len(r2.cells)):
        ws = r2.cell_words(ci)
        text = " ".join(w.text for w in ws)
        if abs(ws[0].left - words[0].left) > 3 * h and _x_overlap(ws[0].left, ws[-1].right,
                                                                   words[0].left, words[-1].right) <= 0:
            continue
        # yukseklik karsilastirilmaz: Tesseract kutu yukseklikleri ayni satirda bile 18-47 px oynuyor
        if _name_cell_ok(text):
            return ws, text
    return None


def _extract_currency(text: str, fallback_conf: float) -> FieldResult:
    key = _key(text)
    counts = {
        "EUR": text.count("€") + len(re.findall(r"(?<![A-Z])EURO?(?![A-Z])", key)),
        "TRY": text.count("₺") + len(re.findall(r"(?<![A-Z])(?:TL|TRY|YTL)(?![A-Z])", key)),
        "USD": text.count("$") + len(re.findall(r"(?<![A-Z])USD(?![A-Z])", key)),
    }
    value, hits = max(counts.items(), key=lambda kv: kv[1])
    if hits == 0:
        return _field("EUR", fallback_conf * 0.5, "")
    source = {"EUR": "€", "TRY": "TL", "USD": "USD"}[value]
    return _field(value, min(0.95, fallback_conf + 0.1), source)


_TOPLAM = r"TOP[L1I|]AM"  # Tesseract "Toplam"i sik sik "Topiam"/"Top1am" okur
_TOTAL_LABELS = (  # once ozel, sonra genel etiket; ozel bir etiketle cakisan genel eslesme atlanir
    ("payable", re.compile(rf"(?<![A-Z])(?:ODENECEK\s*(?:TUTAR|{_TOPLAM})|GENEL\s*{_TOPLAM}|VERGILER\s*DAHIL\s*"
                           rf"(?:{_TOPLAM}\s*)?(?:TUTAR)?|KDV\s*DAHIL\s*(?:{_TOPLAM}\s*)?(?:TUTAR)?|GRAND\s*TOTAL"
                           r"|TOTAL\s*DUE|AMOUNT\s*DUE)(?![A-Z])")),
    ("subtotal", re.compile(rf"(?<![A-Z])(?:MAL\s*/?\s*HIZMET\s*{_TOPLAM}(?:\s*TUTA(?:RI?|N))?|ARA\s*{_TOPLAM}"
                            rf"|KDV\s*MATRAHI|MATRAH|NET\s*{_TOPLAM}|{_TOPLAM}\s*MAL\s*BEDELI"
                            rf"|KDV\s*HARIC\s*(?:{_TOPLAM}\s*)?(?:TUTAR)?|SUB\s*-?\s*TOTAL)(?![A-Z])")),
    ("discount", re.compile(rf"(?<![A-Z])(?:{_TOPLAM}\s*(?:ISKONTO|INDIRIM)|(?:ISKONTO|INDIRIM)\s*{_TOPLAM}I?)"
                            r"(?![A-Z])")),
    ("vat", re.compile(rf"(?<![A-Z])(?:HESAPLANAN\s*KDV|{_TOPLAM}\s*KDV|KDV\s*{_TOPLAM}I|KDV\s*TUTARI|VAT|TAX)"
                       r"(?![A-Z])")),
    ("total", re.compile(rf"(?<![A-Z])(?:{_TOPLAM}(?:\s*TUTAR)?|TOTAL)(?![A-Z])")),
)


def _pick_amount(number_format: str):
    def pick(toks, from_end):
        seq = list(reversed(toks)) if from_end else list(toks)
        for i, (text, w) in enumerate(seq):
            core = text.strip("()[]{}*")
            if "%" in core or _is_date_token(core):
                continue
            # bosluklu binlik: "1 018,00"
            if not from_end and re.fullmatch(r"\d{1,3}", core) and i + 1 < len(seq) and \
                    re.fullmatch(r"\d{3}[.,]\d{2}\S*", seq[i + 1][0]):
                v = parse_amount(core + seq[i + 1][0], number_format)
                if v is not None:
                    return v, [w, seq[i + 1][1]], f"{text} {seq[i + 1][0]}"
            v = parse_amount(core, number_format)
            if v is not None:
                return v, [w], text
        return None
    return pick


def _extract_totals(rows: list[_Row], skip_rows: set[int], number_format: str) -> dict[str, Decimal]:
    taken: dict[int, list[tuple[int, int]]] = {}
    out: dict[str, Decimal] = {}
    pick = _pick_amount(number_format)
    for name, pattern in _TOTAL_LABELS:
        hits = []
        for ri, ms, me, w in _hits(rows, pattern, 1.0, skip_rows):
            if any(ms < e and me > s for s, e in taken.get(ri, [])):
                continue
            taken.setdefault(ri, []).append((ms, me))
            hits.append((ri, ms, me, w))
        if name == "vat":  # birden fazla KDV orani: her satirin KDV'si toplanir
            values = [f.value for h in hits if (f := _label_value(rows, [h], pick, skip_rows))]
            if values:
                out[name] = sum(values, Decimal(0))
            continue
        found = _label_value(rows, hits[-1:] if name == "total" else hits, pick, skip_rows)
        if found is not None:
            out[name] = found.value
    return out


# ---------------------------------------------------------------------------
# Urun satirlari
# ---------------------------------------------------------------------------

# "TUTA(?:RI?|N)" / "ORA[NM]I?": Tesseract "Tutari"yi sik sik "Tutan", "Orani"yi "Oram" okur.
_COLUMN_PATTERNS = (  # ayni konumda ILK eslesen kazanir: uzun/ozel ifadeler once
    ("total", r"MAL\s*/?\s*HIZMET\s*TUTA(?:RI?|N)"),
    ("vat_amount", r"KDV\s*TUTA(?:RI?|N)"),
    ("vat_rate", r"KDV(?:\s*(?:ORA[NM]I?|%))?"),
    ("disc_amount", r"(?:ISKONTO|ISK\.?|INDIRIM)\s*TUTA(?:RI?|N)"),
    ("disc_rate", r"(?:ISKONTO|ISK\.?|INDIRIM)(?:\s*(?:ORA[NM]I?|%))?"),
    ("price", r"(?:BIRIM|B\.)\s*FIYATI?|(?:(?:LISTE|NET|SATIS)\s*)?FIYATI?|UNIT\s*PRICE|PRICE"),
    ("total", r"(?:(?:NET|TOPLAM|SATIR|BRUT)\s*)?TUTA(?:RI?|N)|TOPLAM|AMOUNT|TOTAL"),
    ("code", r"(?:URUN|STOK|MALZEME|MAL)\s*KODU|BARKOD|KODU?"),
    ("name", r"MAL\s*/?\s*HIZMET(?:\s*(?:ADI|CINSI|ACIKLAMASI))?|(?:URUN|MALZEME|STOK|HIZMET)(?:\s*(?:ADI|ACIKLAMASI"
             r"|CINSI|ISMI))?|ACIKLAMA(?:SI)?|CINSI|DESCRIPTION|ITEM|ARTICLE|MAL"),
    ("qty", r"MIKTARI?|MIK\.?|QTY|QUANTITY|ADET|KG|KILO"),
    ("unit", r"BIRIMI?|BR\.?|OLCU(?:\s*BIRIMI)?|UNIT"),
    ("lot", r"LOT(?:\s*NO)?|PARTI(?:\s*NO)?|SARJ(?:\s*NO)?|CHARGE|BATCH"),
    ("expiry", r"S\.?\s*K\.?\s*T\.?|SON\s*KUL+(?:ANMA)?(?:\s*TAR(?:IHI)?)?\.?|S\.?\s*T\.?\s*T\.?"
               r"|SON\s*TUKETIM(?:\s*TAR(?:IHI)?)?\.?|T\.?\s*E\.?\s*T\.?\s*T\.?|EXP(?:IRY)?(?:\s*DATE)?"),
    ("line_no", r"SIRA(?:\s*NO)?|S\.?\s*NO|NO|#"),
)
_COLUMN_CATS = [cat for cat, _ in _COLUMN_PATTERNS]
_COLUMN_RE = re.compile("|".join(f"(?<![A-Z])(?P<c{i}>{pat})(?![A-Z])" for i, (_, pat) in enumerate(_COLUMN_PATTERNS)))
_HEADER_FRAGMENTS = {"ORANI", "ORAN", "ORAM", "TUTARI", "TUTAR", "TUTAN", "FIYATI", "FIYAT", "NO", "ADI", "KODU",
                     "BIRIMI", "TARIHI", "TAR.", "(EUR)", "(TL)", "(€)", "%", "MIKTARI", "ACIKLAMASI", "CINSI", "(KG)"}
_NUMERIC_COLS = {"qty", "price", "total", "vat_rate", "vat_amount", "disc_rate", "disc_amount", "line_no"}
_TABLE_END_RE = re.compile(r"(?<![A-Z])(?:TOP[L1I|]AM|YALNIZ|IBAN|BANKA|NOT\s*:|ACIKLAMA\s*:|TESLIM\s*EDEN|IMZA"
                           r"|KASE|MATRAH|HESAPLANAN|ODENECEK|SUBTOTAL|TOTAL)(?![A-Z])")
# Tek sayili, miktarsiz satirda bu kelimelerin bozuk hali bile tablonun bittigini gosterir
_TOTALS_HINT_RE = re.compile(r"TOP|TUTA|KDV|MATRAH|ISKONTO|ODENECEK|GENEL|YEKUN|VERGI")

_UNITS = {
    "KG": "kg", "KGS": "kg", "KGM": "kg", "KILO": "kg", "KILOGRAM": "kg", "GR": "gr", "GRAM": "gr", "GRM": "gr",
    "TON": "ton", "TNE": "ton", "ADET": "adet", "AD": "adet", "ADT": "adet", "C62": "adet", "NIU": "adet",
    "PCS": "adet", "PCE": "adet", "TANE": "adet", "LT": "lt", "LTR": "lt", "LITRE": "lt", "ML": "ml",
    "PAKET": "paket", "PKT": "paket", "PK": "paket", "KOLI": "koli", "KUTU": "kutu", "KASA": "kasa",
}
_SHORT_UNITS = {"G": "gr", "L": "lt"}  # tek harf: yalnizca sayiya bitisikken ("5L", "500G")
_QTY_UNIT_RE = re.compile(r"([\d.,OolI]*\d[\d.,OolI]*)\s*([A-Za-zÇĞİÖŞÜçğıöşü]+\.?)")

_LOT_RE = re.compile(r"(?<![A-Z])(?:LOT|PARTI|SARJ|CHARGE|BATCH)(?:\s*(?:NO|NR|NUMARASI))?\s*[:.#\-]?\s*"
                     r"([A-Z0-9][A-Z0-9\-/._]*[A-Z0-9]|[A-Z0-9])(?![A-Z0-9])")
_SKT_LABEL_RE = re.compile(r"(?<![A-Z])(?:S\.?\s?K\.?\s?T|S\.?\s?T\.?\s?T|T\.?\s?E\.?\s?T\.?\s?T"
                           r"|SON\s*KUL+ANMA(?:\s*TAR(?:IHI)?)?|SON\s*TUKETIM(?:\s*TAR(?:IHI)?)?"
                           r"|EXP(?:IRY)?(?:\s*DATE)?|BEST\s*BEFORE)\.?(?![A-Z])\s*[:\-]?\s*")


def _unit(text: str, attached: bool = False) -> str | None:
    k = _key(text).strip(".()")
    return _UNITS.get(k) or (_SHORT_UNITS.get(k) if attached else None)


@dataclass
class _Tok:
    word: _Word
    text: str
    kind: str  # num | pct | unit | unit_used | cur | date | junk | text
    value: Decimal | None = None
    unit: str | None = None
    zone: str | None = None
    orig: "_Tok | None" = None  # virgul duzeltmesiyle uretilen kopyanin satirdaki asli
    unit_tok: "_Tok | None" = None  # sayiya bitisik birim kelimesi ("12" -> "KG")


def _tokenize(word: _Word, number_format: str, zone: str | None) -> _Tok:
    t = word.text
    core = t.strip("()[]{}*;")
    if core in ("€", "₺", "$") or _CURRENCY_RE.fullmatch(core):
        return _Tok(word, t, "cur", zone=zone)
    if "%" in core:
        return _Tok(word, t, "pct", parse_amount(core.replace("%", ""), number_format), zone=zone)
    if _is_date_token(core):
        return _Tok(word, t, "date", zone=zone)
    u = _unit(core)
    if u:
        return _Tok(word, t, "unit", unit=u, zone=zone)
    m = _QTY_UNIT_RE.fullmatch(core)
    if m and _unit(m.group(2), attached=True):  # "25,000kg", "5L"
        v = parse_amount(m.group(1), number_format)
        if v is not None:
            return _Tok(word, t, "num", v, unit=_unit(m.group(2), attached=True), zone=zone)
    v = parse_amount(core, number_format)
    if v is not None:
        kind = "pct" if zone in ("vat_rate", "disc_rate") else "num"
        return _Tok(word, t, kind, v, zone=zone)
    return _Tok(word, t, "text", zone=zone)


def _merge_split_numbers(toks: list[_Tok], number_format: str) -> list[_Tok]:
    """Bosluklu binlik ayiraci: "1 543,50" Tesseract'ta iki kelime ("1", "543,50") gelir."""
    out: list[_Tok] = []
    for t in toks:
        prev = out[-1] if out else None
        if (prev and prev.kind == "num" and t.kind == "num" and prev.zone == t.zone and prev.unit is None
                and re.fullmatch(r"\d{1,3}", prev.text) and re.match(r"\d{3}(?:[.,]\d+)?\D*$", t.text)
                and t.word.left - prev.word.right < 0.6 * max(prev.word.height, t.word.height)):
            v = parse_amount(prev.text + t.text, number_format)
            if v is not None:
                merged = _Word(prev.text + " " + t.text, min(prev.word.conf, t.word.conf), prev.word.left,
                               prev.word.top, t.word.right - prev.word.left, prev.word.height, prev.word.line)
                out[-1] = _Tok(merged, merged.text, "num", v, unit=t.unit, zone=t.zone)
                continue
        out.append(t)
    return out


def _tol(total: Decimal) -> Decimal:
    return max(Decimal("0.02"), abs(total) * Decimal("0.0005"))


def _consistent(q, p, t, disc_pct=None, disc_amount=None) -> bool:
    calc = q * p
    if disc_pct:
        calc = calc * (1 - disc_pct / 100)
    if disc_amount:
        calc -= disc_amount
    return abs(calc - t) <= _tol(t)


def _alternatives(t: _Tok, divisors: tuple[int, ...]) -> list[tuple[Decimal, int | None]]:
    """Tokenin degeri; ayiraci hic okunmamis bir sayiysa ("14,80" -> "1480") virgul kaybi adaylari da."""
    alts: list[tuple[Decimal, int | None]] = [(t.value, None)]
    digits = _clean_number(t.text)
    if t.value and digits and re.fullmatch(r"\d{3,}", digits):
        # 1480 / 100 -> 14.80 (2 ondalik), 15000 / 1000 -> 15.000 (3 ondalik)
        alts += [((t.value / dv).quantize(Decimal(1).scaleb(-(len(str(dv)) - 1))), dv) for dv in divisors]
    return alts


def _best_triple(nums: list[_Tok], pcts: list[Decimal]):
    """miktar x birim fiyat (x (1 - iskonto)) = tutar tutan uclu. Sutun ipuclari (birimli miktar,
    fiyat/tutar sutunu, soldan saga sira) esitlikte belirleyicidir; virgul kaybi duzeltmesi ceza alir.
    Donus: (puan, qi, pi, ti, iskonto, (q, p, t degerleri), (q, p, t bolenleri))."""
    best = None
    n = len(nums)
    alts_q = [_alternatives(t, (1000, 100)) for t in nums]  # miktar 3 ondalikli olabilir (25,000)
    alts_m = [_alternatives(t, (100,)) for t in nums]
    for ti in range(n):
        for tv, tdv in alts_m[ti]:
            if not tv or tv <= 0:
                continue
            for qi in range(ti):
                for pi in range(ti):
                    if qi == pi:
                        continue
                    for qv, qdv in alts_q[qi]:
                        for pv, pdv in alts_m[pi]:
                            if not qv or not pv or qv <= 0 or pv <= 0:
                                continue
                            for d in [None, *pcts]:
                                if d is not None and not 0 < d < 100:
                                    continue
                                if not _consistent(qv, pv, tv, d):
                                    continue
                                fixes = sum(1 for dv in (qdv, pdv, tdv) if dv)
                                score = ((1 if nums[qi].unit else 0) + (2 if nums[qi].zone == "qty" else 0)
                                         - (2 if nums[qi].zone == "name" else 0)
                                         + (2 if nums[pi].zone == "price" else 0)
                                         + (2 if nums[ti].zone == "total" else 0)
                                         + (1 if qi < pi else 0) + (1 if ti == n - 1 else 0)
                                         + (0.5 if d is None else 0) - 1.5 * fixes)
                                if best is None or score > best[0]:
                                    best = (score, qi, pi, ti, d, (qv, pv, tv), (qdv, pdv, tdv))
    return best


@dataclass
class _Item:
    name: str = ""
    name_words: list[_Word] = field(default_factory=list)
    qty: _Tok | None = None
    unit: str | None = None
    unit_words: list[_Word] = field(default_factory=list)
    price: _Tok | None = None
    total: _Tok | None = None
    disc_pct: Decimal | None = None
    consistent: bool = False
    lot: str | None = None
    lot_words: list[_Word] = field(default_factory=list)
    skt: date | None = None
    skt_raw: str = ""
    skt_words: list[_Word] = field(default_factory=list)
    notes: list[str] = field(default_factory=list)


@dataclass
class _Col:
    cat: str
    left: float
    right: float
    words: list[tuple[int, _Word]] = field(default_factory=list)  # (satir, kelime)

    @property
    def text(self) -> str:
        return " ".join(w.text for _, w in sorted(self.words, key=lambda rw: (rw[0], rw[1].left)))

    def add(self, ri: int, w: _Word) -> None:
        self.words.append((ri, w))
        self.left, self.right = min(self.left, w.left), max(self.right, w.right)


def _zone_of(cols: list[_Col], x: float) -> str:
    for i, c in enumerate(cols[:-1]):
        nxt = cols[i + 1]
        boundary = (c.right + nxt.left) / 2 if c.right <= nxt.left else (c.left + c.right + nxt.left + nxt.right) / 4
        if x < boundary:
            return c.cat
    return cols[-1].cat


def _row_lot_skt(row: _Row, cols: list[_Col] | None, item: _Item) -> set[int]:
    """LOT:/Parti/SKT: etiketli degerler ya da (baslikta varsa) Lot/SKT sutunlari; kullanilan
    kelime indeksleri doner. Etiketsiz bir tarih/kod UYDURULMAZ."""
    used: set[int] = set()
    for m in _LOT_RE.finditer(row.key):
        val = row.text[m.start(1):m.end(1)]
        if any(c.isdigit() for c in val):
            idx = row.word_range(m.start(), m.end())
            item.lot, item.lot_words = val, [row.words[i] for i in idx]
            used.update(idx)
            break
    for m in _SKT_LABEL_RE.finditer(row.key):
        dates = _iter_dates(row.text[m.end():m.end() + 30])
        if dates and dates[0][0] <= 2:
            s, e, d, src = dates[0]
            idx = row.word_range(m.start(), m.end() + e)
            item.skt, item.skt_raw, item.skt_words = d, src, [row.words[i] for i in idx]
            used.update(idx)
            break
    if cols:
        lot_idx = [i for i, w in enumerate(row.words) if i not in used and _zone_of(cols, w.cx) == "lot"]
        if item.lot is None and lot_idx:
            val = " ".join(row.words[i].text for i in lot_idx).strip(_SEP_STRIP)
            if any(c.isdigit() for c in val):
                item.lot, item.lot_words = val, [row.words[i] for i in lot_idx]
                used.update(lot_idx)
        exp_idx = [i for i, w in enumerate(row.words) if i not in used and _zone_of(cols, w.cx) == "expiry"]
        if item.skt is None and exp_idx:
            text = " ".join(row.words[i].text for i in exp_idx)
            found = parse_turkish_date(text)
            if found:
                item.skt, item.skt_raw, item.skt_words = found[0], found[1], [row.words[i] for i in exp_idx]
                used.update(exp_idx)
    return used


def _parse_row(row: _Row, cols: list[_Col] | None, number_format: str) -> tuple[str, _Item]:
    """Bir satiri ayristirir: ("item" | "meta" | "text" | "empty", _Item).

    Tablo basligi varsa (cols) kelimeler sutunlara atanir; yoksa sira sezgisi kullanilir. Her iki
    durumda da miktar x fiyat = tutar tutan uclu varsa o kazanir (sutun kaymasina dayanikli)."""
    item = _Item()
    used = _row_lot_skt(row, cols, item)
    toks = [_tokenize(w, number_format, _zone_of(cols, w.cx) if cols else None)
            for i, w in enumerate(row.words) if i not in used]
    toks = _merge_split_numbers(toks, number_format)
    for i, t in enumerate(toks[:-1]):  # "% 1": yuzde isareti ayri kelime
        if t.kind == "pct" and t.value is None and toks[i + 1].kind == "num":
            toks[i + 1].kind = "pct"
    has_name_col = bool(cols) and any(c.cat == "name" for c in cols)
    if has_name_col:
        # urun adi yalniz ad sutununun komsu sutunlarina tasar (baslik genis hucrede ortalandigindan
        # adin basi "Sira No" bolgesine, uzun ad "Miktar" bolgesine dusebilir); uzaktaki sutunlarda ya
        # da rakamli yazi bozuk okunmus sayi/yuzdedir ("%0" -> "O", "%1" -> "Y1"): ada eklenmez
        spill = {cols[j].cat for i, c in enumerate(cols) if c.cat == "name" for j in (i - 1, i + 1)
                 if 0 <= j < len(cols)}
        for t in toks:
            if t.kind == "text" and t.zone in _NUMERIC_COLS:
                letters = sum(ch.isalpha() for ch in t.text)
                if t.zone in spill and letters >= 2 and not any(ch.isdigit() for ch in t.text):
                    t.zone = "name"
                else:
                    t.kind = "junk"

    def is_name(t: _Tok) -> bool:
        if not cols:
            return True
        if has_name_col:
            return t.zone == "name"
        return t.kind == "text" and t.zone not in ("unit", "lot", "expiry", "code")
    # sayiya bitisik birim kelimesi: "25,000 kg", "10 KG"
    for i, t in enumerate(toks):
        if t.kind == "num" and t.unit is None and i + 1 < len(toks) and toks[i + 1].kind == "unit":
            t.unit, t.unit_tok = toks[i + 1].unit, toks[i + 1]
            toks[i + 1].kind = "unit_used"
    leading_no = None
    if toks and toks[0].kind == "num" and toks[0].zone in (None, "line_no", "name") and toks[0].unit is None \
            and re.fullmatch(r"\d{1,3}", toks[0].text) and len(toks) > 1 and toks[1].kind == "text":
        leading_no = toks[0]  # sira numarasi
    excluded_zones = {"line_no", "code", "vat_rate", "vat_amount", "disc_rate", "disc_amount", "lot", "expiry"}
    nums = [t for t in toks if t.kind == "num" and t is not leading_no and t.zone not in excluded_zones]
    if cols:
        nums = [t for t in nums if t.zone != "name" or t.unit]  # addaki sayilar ("1 kg'lik") miktar degildir
    pcts = [t.value for t in toks if t.kind == "pct" and t.value is not None and t.zone != "vat_rate"]

    # 1) sutunlar  2) aritmetik uclu  3) sira sezgisi
    q = p = tot = None
    disc = None
    if cols:
        # "Miktar" basligi okunamadiysa birimli sayi ("15,000 kg") miktardir
        q = next((t for t in nums if t.zone == "qty"), None) or next((t for t in nums if t.unit), None)
        p = next((t for t in nums if t.zone == "price" and t is not q), None)
        tot = next((t for t in reversed(nums) if t.zone == "total" and t is not q and t is not p), None)
        disc_pcts = [t.value for t in toks if t.kind == "pct" and t.zone == "disc_rate" and t.value]
        disc_amts = [t.value for t in toks if t.kind == "num" and t.zone == "disc_amount" and t.value]
        if q and p and tot:
            if _consistent(q.value, p.value, tot.value):
                item.consistent = True
            elif disc_pcts and _consistent(q.value, p.value, tot.value, disc_pct=disc_pcts[0]):
                item.consistent, disc = True, disc_pcts[0]
            elif disc_amts and _consistent(q.value, p.value, tot.value, disc_amount=disc_amts[0]):
                item.consistent, disc = True, disc_amts[0] / (q.value * p.value) * 100
    if not item.consistent:
        triple = _best_triple(nums, pcts)
        if triple:
            _s, qi, pi, ti, d, values, divisors = triple
            picked = []
            for k, v, dv in zip((qi, pi, ti), values, divisors):
                if dv:
                    item.notes.append(f"ONDALIK_DUZELTILDI: '{nums[k].text}' okundu, ondalik ayirac okunamadigi "
                                      f"icin {v} olarak yorumlandi (miktar x fiyat = tutar tutuyor)")
                picked.append(replace(nums[k], value=v, orig=nums[k]) if dv else nums[k])
            q, p, tot = picked
            disc, item.consistent = d, True
    if not item.consistent and not cols:
        q = next((t for t in nums if t.unit), None)
        after = [t for t in nums if q and nums.index(t) > nums.index(q)]
        p, tot = (after[0], after[-1]) if len(after) >= 2 else (None, None)

    if q is None and p is None and tot is None:
        name_words = [t.word for t in toks if t.kind in ("text", "num", "unit", "unit_used")
                      and t is not leading_no and is_name(t)]
        if name_words:
            item.name_words = name_words
            item.name = " ".join(w.text for w in name_words)
            return "text", item
        return ("meta" if item.lot or item.skt else "empty"), item

    item.qty, item.price, item.total, item.disc_pct = q, p, tot, disc
    if q is not None:
        unit_tok = next((t for t in toks if t.kind in ("unit", "unit_used", "text") and t.zone == "unit"), None) \
            if cols else None
        item.unit = q.unit or (unit_tok and (unit_tok.unit or _unit(unit_tok.text) or _key(unit_tok.text).lower()))
        unit_tok = unit_tok or q.unit_tok
        item.unit_words = [q.word] + ([unit_tok.word] if unit_tok else [])
    chosen = [t.orig or t for t in (q, p, tot) if t is not None]
    # secilen sayilar ve miktara bitisik birim ada girmez; addaki "1 kg" ise kalir
    consumed = {id(t) for t in chosen} | {id(t.unit_tok) for t in chosen if t.unit_tok}
    first_num = min((toks.index(t) for t in chosen), default=len(toks))
    name_toks = []
    for i, t in enumerate(toks):
        if id(t) in consumed or t is leading_no or t.kind in ("cur", "pct", "date", "junk"):
            continue
        if cols:
            if is_name(t):
                name_toks.append(t)
        elif i < first_num:
            name_toks.append(t)
    item.name_words = [t.word for t in name_toks]
    item.name = " ".join(w.text for w in item.name_words).strip(_SEP_STRIP)
    return "item", item


def _column_hits(row: _Row) -> list[tuple[str, int, int]]:
    hits = []
    for m in _COLUMN_RE.finditer(row.key):
        idx = row.word_range(m.start(), m.end())
        if idx:
            hits.append((_COLUMN_CATS[int(m.lastgroup[1:])], idx[0], idx[-1]))
    return hits


def _is_header(row: _Row) -> bool:
    hits = _column_hits(row)
    cats = {c for c, _, _ in hits} - {"line_no", "code"}
    if len(cats) < 3 or not cats & {"qty", "price", "total"}:
        return False
    covered = {i for _, a, b in hits for i in range(a, b + 1)}
    alpha = [i for i, w in enumerate(row.words) if any(ch.isalpha() for ch in w.text)]
    if sum(1 for i in alpha if i in covered) < 0.6 * len(alpha):
        return False
    return sum(1 for w in row.words if any(ch.isdigit() for ch in w.text) and "%" not in w.text) <= 1


def _header_like(row: _Row) -> bool:
    """Rakamsiz ve kelimelerinin cogu baslik kelimesi/parcasi olan satir (yigilmis baslik parcasi
    olabilir: "Birim" ustte, "Fiyati" altta). Urun adi satiri bu kosulu saglamaz."""
    if sum(1 for w in row.words if any(ch.isdigit() for ch in w.text) and "%" not in w.text) > 1:
        return False
    hit_words = {i for _, a, b in _column_hits(row) for i in range(a, b + 1)}
    good = sum(1 for i, w in enumerate(row.words) if i in hit_words or _key(w.text) in _HEADER_FRAGMENTS)
    return any(any(ch.isalpha() for ch in w.text) for w in row.words) and good >= 0.5 * len(row.words)


def _header_block(rows: list[_Row], i: int) -> list[int] | None:
    """i'de baslayan 1-3 satirlik tablo basligi. Iki satirli basliklar ortalanmis tek satirlik
    basliklarla birlikte uc gorsel satira dagilabilir; her biri tek basina baslik sayilmayabilir."""
    if not _header_like(rows[i]):
        return None
    block = [i]
    for j in range(i + 1, min(i + 3, len(rows))):
        prev = rows[block[-1]]
        if rows[j].top - prev.bottom > 0.8 * prev.height or not _header_like(rows[j]):
            break
        block.append(j)
    for n in range(len(block), 0, -1):
        if _is_header(_Row([w for k in block[:n] for w in rows[k].words])):
            return block[:n]
    return None


def _header_columns(rows: list[_Row], block: list[int]) -> list[_Col]:
    """Baslik sutunlari. En cok baslik iceren satir ana satirdir; bloktaki diger satirlarin parcalari
    ("Birim" / "Fiyat", "Mal Hizmet" / "Tutari") yatay ortusmeyle eklenir ve sutun birlesik metinle
    yeniden siniflandirilir."""
    hi = max(block, key=lambda k: (len(_column_hits(rows[k])), -k))
    row = rows[hi]
    hits = _column_hits(row)
    cols = []
    for cat, a, b in hits:
        col = _Col(cat, row.words[a].left, row.words[b].right)
        for w in row.words[a:b + 1]:
            col.add(hi, w)
        cols.append(col)
    hit_idx = {i for _, a, b in hits for i in range(a, b + 1)}
    extra = [(hi, w) for i, w in enumerate(row.words) if i not in hit_idx]  # eslesmeyen parca ("Orani")
    extra += [(j, w) for j in block if j != hi for w in rows[j].words]

    def nearest(w: _Word) -> _Col | None:
        near = [c for c in cols if _x_overlap(c.left - row.height, c.right + row.height, w.left, w.right) > 0]
        return min(near, key=lambda c: abs((c.left + c.right) / 2 - w.cx)) if near else None

    leftover = []
    for j, w in extra:  # 1. tur: mevcut sutuna ekle ya da kendi basina baslik olan parcadan sutun ac
        c = nearest(w)
        m = None if c else _COLUMN_RE.search(_key(w.text))
        if c:
            c.add(j, w)
        elif m:
            col = _Col(_COLUMN_CATS[int(m.lastgroup[1:])], w.left, w.right)
            col.add(j, w)
            cols.append(col)
        else:
            leftover.append((j, w))
    for j, w in leftover:  # 2. tur: yeni acilan sutunlarin alt/ust parcalari
        c = nearest(w)
        if c:
            c.add(j, w)
    for c in cols:  # birlesik metinle yeniden siniflandir: "Birim Fiyat" -> fiyat, "Mal Hizmet Tutari" -> tutar
        m = _COLUMN_RE.search(_key(c.text))
        if m:
            c.cat = _COLUMN_CATS[int(m.lastgroup[1:])]
    if not any(c.cat == "name" for c in cols):
        # "Mal Hizmet" basligi okunamadi: ilk sayi sutununun solunda kalan bosluk ad sutunudur
        numeric = [c for c in cols if c.cat in ("qty", "unit", "price", "total")]
        if numeric:
            first = min(numeric, key=lambda c: c.left)
            x0 = max((c.right for c in cols if c.right <= first.left), default=0) + row.height
            x1 = first.left - row.height
            if x1 > x0:
                cols.append(_Col("name", x0, x1))
    cols.sort(key=lambda c: c.left)
    return cols


def _extract_items(rows: list[_Row], number_format: str) -> tuple[list[_Item], set[int], int | None]:
    """(kalemler, tablo satirlari, ilk baslik satiri). Baslik varsa yalniz baslik ile toplam
    satirlari arasi okunur; yoksa her satir sira sezgisiyle denenir (miktar+birim ve iki tutar ya da
    tutan bir aritmetik uclu sart)."""
    blocks: dict[int, list[int]] = {}
    i = 0
    while i < len(rows):
        block = _header_block(rows, i)
        if block:
            blocks[i] = block
        i = block[-1] + 1 if block else i + 1
    items: list[_Item] = []
    table_rows: set[int] = set()
    if not blocks:
        for ri, row in enumerate(rows):
            kind, item = _parse_row(row, None, number_format)
            if kind == "item" and item.qty is not None and item.price is not None and item.total is not None:
                items.append(item)
                table_rows.add(ri)
            elif kind == "meta" and items and ri - 1 in table_rows:
                _attach_meta(items[-1], item)
                table_rows.add(ri)
        return items, table_rows, None

    cols: list[_Col] | None = None
    pending: list[_Item] = []
    pending_meta: list[_Item] = []
    last_bottom = 0
    ri = 0
    while ri < len(rows):
        row = rows[ri]
        if ri in blocks:
            _flush_pending(items, pending)
            cols = _header_columns(rows, blocks[ri])
            table_rows |= set(blocks[ri])
            last_bottom = max(rows[j].bottom for j in blocks[ri])
            ri = blocks[ri][-1] + 1
            continue
        if cols is None:
            ri += 1
            continue
        if _TABLE_END_RE.search(row.key) or row.top - last_bottom > 10 * row.height:
            _flush_pending(items, pending)
            cols = None
            ri += 1
            continue
        kind, item = _parse_row(row, cols, number_format)
        if kind == "item" and sum(x is not None for x in (item.qty, item.price, item.total)) < 2:
            if items and _TOTALS_HINT_RE.search(row.key):  # bozuk okunmus toplam satiri: "Topiam Tutan 472,50"
                _flush_pending(items, pending)
                cols = None
            ri += 1  # tek sayili satir kalem degildir
            continue
        table_rows.add(ri)
        last_bottom = row.bottom
        if kind == "item":
            if not item.name and pending:  # ad, sayilarin ustundeki satirda
                item.name_words = [w for p in pending for w in p.name_words]
                item.name = " ".join(p.name for p in pending)
                pending = []
            _flush_pending(items, pending)
            pending = []
            for meta in pending_meta:
                _attach_meta(item, meta)
            pending_meta = []
            items.append(item)
        elif kind == "text":
            if item.lot or item.skt:
                (_attach_meta(items[-1], item) if items else pending_meta.append(item))
            pending.append(item)
        elif kind == "meta":
            (_attach_meta(items[-1], item) if items else pending_meta.append(item))
        ri += 1
    _flush_pending(items, pending)
    return items, table_rows, min(blocks)


def _flush_pending(items: list[_Item], pending: list[_Item]) -> None:
    """Kalem satirindan sonra gelen yalniz yazi satirlari: alt satira tasmis urun adi."""
    if items and pending:
        last = items[-1]
        last.name_words = last.name_words + [w for p in pending for w in p.name_words]
        last.name = " ".join([last.name] + [p.name for p in pending]).strip()
    pending.clear()


def _attach_meta(target: _Item, meta: _Item) -> None:
    if target.lot is None and meta.lot:
        target.lot, target.lot_words = meta.lot, meta.lot_words
    if target.skt is None and meta.skt:
        target.skt, target.skt_raw, target.skt_words = meta.skt, meta.skt_raw, meta.skt_words


def _fmt_price(v: Decimal) -> str:
    """En fazla 4, en az 2 ondalik: 10.125 -> '10.125', 11.25 -> '11.25'."""
    s = f"{v.quantize(Decimal('0.0001')):f}".rstrip("0")
    whole, _, frac = s.partition(".")
    return f"{whole}.{frac.ljust(2, '0')}"


def _fmt_pct(v: Decimal) -> str:
    return f"{v.quantize(Decimal('0.01')).normalize():f}"


def _line_result(item: _Item, fallback_conf: float) -> LineResult:
    def conf(words: list[_Word]) -> float:
        c = _mean_conf(words, fallback_conf)
        # aritmetik tutuyorsa sayilar dogrulanmis demektir; virgul duzeltmesi yapildiysa kullanici baksin
        return max(c, 0.9) if item.consistent and not item.notes else c

    warnings: list[str] = list(item.notes)
    q, p, t = (x.value if x else None for x in (item.qty, item.price, item.total))
    price_value = str(p) if p is not None else None
    price_source = item.price.text if item.price else ""
    if q is not None and p is not None and t is not None:
        if item.consistent and item.disc_pct:
            # Iskontolu satirda maliyet net fiyattir; backend de tutari miktar x birim fiyatla karsilastirir.
            price_value = _fmt_price(t / q)
            pct = _fmt_pct(item.disc_pct)
            price_source = f"{item.price.text} (%{pct} iskonto)"
            warnings.append(f"ISKONTO_UYGULANDI: liste birim fiyati {p}, %{pct} iskonto; "
                            f"net birim fiyat {price_value} onerildi")
        elif not item.consistent:
            computed = (q * p).quantize(Decimal("0.01"))
            warnings.append(f"TUTAR_UYUSMAZLIGI: {q} x {p} = {computed}, okunan satir tutari {t}")
    mismatch = [w for w in warnings if w.startswith("TUTAR_UYUSMAZLIGI")]
    return LineResult(
        product_name=_field(item.name or None, _mean_conf(item.name_words, fallback_conf) if item.name else 0.0,
                            item.name),
        quantity=_field(str(q) if q is not None else None, conf([item.qty.word]) if item.qty else 0.0,
                        item.qty.text if item.qty else ""),
        unit=_field(item.unit, conf(item.unit_words) if item.unit else 0.0,
                    " ".join(w.text for w in item.unit_words) if item.unit else ""),
        unit_price=_field(price_value, conf([item.price.word]) if item.price else 0.0, price_source),
        line_total=_field(str(t) if t is not None else None, conf([item.total.word]) if item.total else 0.0,
                          item.total.text if item.total else "", mismatch),
        lot_no=_field(item.lot, _mean_conf(item.lot_words, fallback_conf) if item.lot else 0.0, item.lot or ""),
        expiry_date=_field(item.skt.isoformat() if item.skt else None,
                           _mean_conf(item.skt_words, fallback_conf) if item.skt else 0.0, item.skt_raw),
        warnings=warnings,
    )


def parse_line_row(raw_line: str) -> dict | None:
    """Tek bir duz metin satirini (OCR'siz) urun satiri olarak ayristirir; urun satiri degilse None.
    Kelime konumu olmadigindan tablo basligi kullanilmaz (sira sezgisi + aritmetik)."""
    words = _words_from_text(raw_line)
    if not words:
        return None
    kind, item = _parse_row(_Row(words), None, detect_number_format(raw_line))
    if kind != "item" or item.qty is None or item.price is None or item.total is None:
        return None
    m = _QTY_UNIT_RE.fullmatch(item.qty.text.strip("()[]{}*;"))
    qty_raw = m.group(1) if m and _unit(m.group(2), attached=True) else item.qty.text  # "25,000kg" -> "25,000"
    return {
        "name": item.name, "qty_raw": qty_raw,
        "unit": item.unit, "price_raw": item.price.text, "total_raw": item.total.text,
        "lot_raw": item.lot, "skt_raw": item.skt_raw or None, "raw_line": raw_line,
    }


# ---------------------------------------------------------------------------
# Giris noktasi
# ---------------------------------------------------------------------------


def _extract_from_words(words: list[_Word]) -> InvoiceResult:
    """OCR'dan bagimsiz cekirdek: kelime kutucuklarindan oneri uretir."""
    rows = _build_rows(words)
    if not rows:
        return _empty_result("METIN_BULUNAMADI: goruntuden hicbir metin okunamadi")
    raw_text = "\n".join(r.text for r in rows)
    number_format = detect_number_format(raw_text)
    overall_word_conf = _mean_conf([w for r in rows for w in r.words])

    items, table_rows, header_row = _extract_items(rows, number_format)
    # baslik bloku: tablo basligindan once; tablo yoksa sayfanin ust %45'i
    if header_row is not None:
        limit = header_row
    else:
        page_bottom = max(r.bottom for r in rows)
        limit = max(sum(1 for r in rows if r.top < 0.45 * page_bottom), min(len(rows), 6))
    supplier = _extract_supplier(rows, limit, table_rows, overall_word_conf)
    doc_no = _extract_document_no(rows, table_rows, overall_word_conf)
    doc_date = _extract_document_date(rows, table_rows, overall_word_conf)
    currency = _extract_currency(raw_text, overall_word_conf)
    line_results = [_line_result(it, overall_word_conf) for it in items]

    doc_warnings: list[str] = []
    totals = _extract_totals(rows, table_rows, number_format)
    line_totals = [Decimal(lr.line_total.value) for lr in line_results if lr.line_total.value is not None]
    if line_results and len(line_totals) == len(line_results) and totals:
        line_sum = sum(line_totals, Decimal(0))
        vat, disc = totals.get("vat"), totals.get("discount")
        candidates = []
        if "subtotal" in totals:
            sub = totals["subtotal"]
            candidates += [sub] + ([sub - disc, sub + disc] if disc else [])
        for key in ("payable", "total"):
            if key in totals:
                g = totals[key]
                candidates += [g] + ([g - vat] if vat else []) + ([g - vat + disc] if vat and disc else [])
        if candidates and not any(abs(line_sum - c) <= Decimal("0.02") for c in candidates):
            declared = totals.get("subtotal")
            if declared is None:
                g = totals.get("payable", totals.get("total"))
                declared = g - vat if vat else g
            doc_warnings.append(f"TOPLAM_UYUSMAZLIGI: satir toplami {line_sum} != beyan edilen toplam {declared}")
    if not line_results:
        doc_warnings.append("SATIR_BULUNAMADI: hicbir urun satiri okunamadi, manuel giris gerekebilir")
    return _finalize(InvoiceResult(
        supplier_name=supplier, document_no=doc_no, document_date=doc_date, currency=currency,
        lines=line_results, overall_confidence=0.0, raw_text=raw_text, warnings=doc_warnings,
    ))


def _finalize(r: InvoiceResult) -> InvoiceResult:
    """Genel guveni (alan guvenlerinin ortalamasi) ve DUSUK_GUVEN uyarisini hesaplar."""
    field_confidences = [r.supplier_name.confidence, r.document_no.confidence, r.document_date.confidence,
                         r.currency.confidence]
    for lr in r.lines:
        field_confidences += [lr.product_name.confidence, lr.quantity.confidence, lr.unit_price.confidence,
                              lr.line_total.confidence]
    r.overall_confidence = round(sum(field_confidences) / len(field_confidences), 4)
    r.warnings = [w for w in r.warnings if not w.startswith("DUSUK_GUVEN")]
    if r.overall_confidence < OCR.confidence_medium:
        r.warnings.append("DUSUK_GUVEN: genel guven dusuk, tum alanlari dikkatlice kontrol edin")
    return r


def _lines_score(r: InvoiceResult) -> float:
    """Kalem tablosu kalitesi: miktar x fiyat = tutar tutan satir 3, tutmayan 1 puan; satir toplami
    belge toplamini tutmuyorsa (sahte/eksik satir isareti) ceza."""
    consistent = sum(1 for lr in r.lines if lr.line_total.value and not lr.line_total.warnings)
    penalty = 4 if any(w.startswith("TOPLAM_UYUSMAZLIGI") for w in r.warnings) else 0
    return 3 * consistent + (len(r.lines) - consistent) - penalty + r.overall_confidence


def _complete(r: InvoiceResult) -> bool:
    return (bool(r.lines) and all(f.value for f in (r.supplier_name, r.document_no, r.document_date))
            and all(lr.line_total.value and not lr.line_total.warnings for lr in r.lines)
            and not any(w.startswith("TOPLAM_UYUSMAZLIGI") for w in r.warnings))


def _merge(results: list[InvoiceResult]) -> InvoiceResult:
    """Farkli Tesseract sayfa duzeni modlarinin sonuclari: kalem tablosu en iyi tutan moddan, baslik
    alanlarinin her biri o alani en yuksek guvenle okuyan moddan alinir (modlar farkli yerde yanilir)."""
    base = max(results, key=_lines_score)

    def best(attr: str) -> FieldResult:
        found = [getattr(r, attr) for r in results if getattr(r, attr).value]
        if not found:
            return getattr(base, attr)

        def rank(f: FieldResult):  # digerini kapsayan deger daha tamdir: "AKYAZI GIDA" < "AKYAZI GIDA ... SIRKETI"
            k = _key(f.value)
            return sum(1 for g in found if len(g.value) < len(f.value) and _key(g.value) in k), f.confidence

        return max(found, key=rank)

    return _finalize(replace(base, supplier_name=best("supplier_name"), document_no=best("document_no"),
                             document_date=best("document_date"), currency=best("currency"),
                             warnings=list(base.warnings)))


# Tek blok (6) tablolari satir satir okur; sutun (4) ve otomatik (3) duzen farkli yerlerde yanilir
# (ornek: 6 bir unvan satirini "et" okurken 4 dogru okur). Ilk iki mod her zaman calisir; birlesik
# sonuc eksiksizse (tum alanlar + aritmetigi ve belge toplamini tutan kalemler) 3 denenmez.
_PSM_SEQUENCE = (6, 4, 3)
_PSM_MIN_RUNS = 2


def extract_invoice(content: bytes, content_type: str, filename: str) -> InvoiceResult:
    """Fatura/irsaliye goruntusunden (veya PDF'in ilk sayfasindan) alan onerileri cikarir.

    HATA FIRLATMAZ: goruntu acilamazsa / OCR basarisiz olursa / hicbir alan okunamazsa bos
    oneri + dusuk (0.0) guven doner (bkz. docs/invoice-ocr.md, "Sinirliliklar").
    """
    try:
        img = preprocess_image(_load_image(content, content_type, filename))
    except Exception as e:
        return _empty_result(f"OCR_BASARISIZ: dosya islenemedi ({e})")
    results: list[InvoiceResult] = []
    error: Exception | None = None
    for i, psm in enumerate(_PSM_SEQUENCE):
        try:
            results.append(_extract_from_words(run_tesseract(img, psm)))
        except Exception as e:  # kullaniciya bos oneri doner, hata loglanir (sessizce yutulmaz)
            log.exception("fatura OCR cikarimi basarisiz (psm=%s)", psm)
            error = error or e
            continue
        if i + 1 >= _PSM_MIN_RUNS and _complete(_merge(results)):
            break
    if not results:
        return _empty_result(f"OCR_BASARISIZ: dosya islenemedi ({error})")
    return _merge(results)
