"""Demo fatura/irsaliye gorselleri cizer (docs/samples/ ve ml/tests/test_ocr.py tarafindan kullanilir).

Hayali firma adlari kullanilir; gercek bir firmanin faturasi DEGILDIR. Iki duzen var:
- render_invoice: sade, genis bosluklu sutunlar (docs/samples/ ornekleri).
- render_efatura: GIB e-Fatura goruntusune benzeyen duzen (bilgi kutusu, SAYIN blogu, ETTN,
  iki satirli tablo basliklari, KDV/iskonto sutunlari, izgara cizgileri, KDV'li toplam kutusu).
  Gercek faturalardaki duzen cesitliligini (degerin etiketin sagi/solu/alti, ayri birim sutunu,
  alt satira tasan urun adi, ad icinde LOT/SKT) test etmek icin secenekleri vardir.
"""
from __future__ import annotations

import io
from dataclasses import dataclass, field

from PIL import Image, ImageDraw, ImageFilter, ImageFont

# Docker imajinda DejaVu; Windows/macOS gelistirici makinesinde sistem fontu (Turkce harfli).
_FONT_CANDIDATES = {
    False: ("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", "DejaVuSans.ttf", "arial.ttf", "Arial.ttf"),
    True: ("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", "DejaVuSans-Bold.ttf", "arialbd.ttf",
           "Arial Bold.ttf"),
}

WIDTH, HEIGHT = 1700, 900

# Satir basi 2 satir: 1) ad + miktar + birim fiyat + tutar (genis bosluklu 4 sutun), 2) LOT:/SKT:
# (varsa, kendi satirinda -- aralarinda buyuk bosluk). Tek satirda 6 sutunu sikistirmak komsu
# sutunlarin OCR'da birbirine karismasina (kelime sinirlarinin kaybolmasina) yol aciyordu.
COL_X = [60, 620, 1000, 1280]
HEADERS = ["Ürün", "Miktar", "Birim Fiyat", "Tutar"]
META_LOT_X = 100
META_SKT_X = 780


def _font(size: int, bold: bool = False) -> ImageFont.ImageFont:
    for path in _FONT_CANDIDATES[bold]:
        try:
            return ImageFont.truetype(path, size)
        except OSError:
            continue
    return ImageFont.load_default(size)  # pragma: no cover - en azindan olceklenebilir varsayilan font


@dataclass
class Line:
    name: str
    qty: str
    unit: str
    unit_price: str
    line_total: str
    lot: str | None = None
    skt: str | None = None


def render_invoice(
    supplier: str,
    doc_no: str,
    doc_date: str,
    lines: list[Line],
    grand_total: str,
    *,
    degraded: bool = False,
) -> Image.Image:
    height = max(HEIGHT, 420 + 90 * len(lines))
    img = Image.new("L", (WIDTH, height), color=255)
    draw = ImageDraw.Draw(img)

    y = 50
    draw.text((60, y), supplier, font=_font(38, bold=True), fill=0)
    y += 70
    draw.text((60, y), f"Fatura No: {doc_no}      Tarih: {doc_date}", font=_font(28), fill=0)
    y += 60
    draw.line((60, y, WIDTH - 60, y), fill=0, width=2)
    y += 30
    for x, h in zip(COL_X, HEADERS):
        draw.text((x, y), h, font=_font(26, bold=True), fill=0)
    y += 50
    draw.line((60, y, WIDTH - 60, y), fill=0, width=1)
    y += 20
    for line in lines:
        draw.text((COL_X[0], y), line.name, font=_font(26), fill=0)
        draw.text((COL_X[1], y), f"{line.qty} {line.unit}", font=_font(26), fill=0)
        draw.text((COL_X[2], y), f"{line.unit_price} €", font=_font(26), fill=0)
        draw.text((COL_X[3], y), f"{line.line_total} €", font=_font(26), fill=0)
        y += 44
        if line.lot:
            draw.text((META_LOT_X, y), f"LOT: {line.lot}", font=_font(24), fill=0)
        if line.skt:
            draw.text((META_SKT_X, y), f"SKT: {line.skt}", font=_font(24), fill=0)
        if line.lot or line.skt:
            y += 40
        y += 25
    y += 20
    draw.line((60, y, WIDTH - 60, y), fill=0, width=2)
    y += 30
    draw.text((60, y), f"GENEL TOPLAM: {grand_total} €", font=_font(32, bold=True), fill=0)

    if degraded:
        img = img.rotate(6, expand=True, fillcolor=255, resample=Image.BICUBIC)
        img = img.filter(ImageFilter.GaussianBlur(2.0))
        noise = Image.effect_noise(img.size, 34).convert("L")
        img = Image.blend(img, noise, 0.18)
    return img.convert("RGB")


# ---------------------------------------------------------------------------
# e-Fatura benzeri duzen
# ---------------------------------------------------------------------------


@dataclass
class EfLine:
    name: str  # hucreye sigmazsa alt satira kayar
    qty: str
    unit: str
    unit_price: str
    total: str  # mal hizmet tutari (iskonto sonrasi, KDV haric)
    disc_pct: str = "%0"
    disc_amount: str = "0,00"
    vat_pct: str = "%1"
    vat_amount: str = "0,00"


@dataclass
class EfInvoice:
    seller: list[str]  # ilk satir(lar) unvan (kalin), sonra adres/vergi bilgileri
    seller_name_lines: int
    doc_no: str
    doc_date: str
    lines: list[EfLine]
    totals: list[tuple[str, str]]
    buyer: list[str] = field(default_factory=lambda: [
        "Avatar Gıda Sanayi ve Ticaret A.Ş.", "Kemalpaşa Cad. No:12 Bornova / İZMİR",
        "Vergi Dairesi: BORNOVA  VKN: 1234567890"])
    currency: str = "TL"
    info_layout: str = "right"  # right | left | below
    unit_column: bool = False


_W, _H = 1654, 2339  # A4, 200 DPI
_INK = 0


def _text_w(draw: ImageDraw.ImageDraw, text: str, font) -> int:
    l, _t, r, _b = draw.textbbox((0, 0), text, font=font)
    return r - l


def _wrap(draw, text: str, font, width: int) -> list[str]:
    out, cur = [], ""
    for word in text.split():
        trial = f"{cur} {word}".strip()
        if _text_w(draw, trial, font) <= width or not cur:
            cur = trial
        else:
            out.append(cur)
            cur = word
    return out + ([cur] if cur else [])


def render_efatura(inv: EfInvoice) -> Image.Image:
    img = Image.new("L", (_W, _H), color=255)
    d = ImageDraw.Draw(img)
    f_body, f_bold, f_small = _font(23), _font(23, bold=True), _font(21)
    f_title = _font(46, bold=True)

    # Satici blogu (sol ust)
    y = 70
    for i, text in enumerate(inv.seller):
        d.text((70, y), text, font=f_bold if i < inv.seller_name_lines else f_small, fill=_INK)
        y += 34
    # Baslik
    d.text((690, 240), "e-FATURA", font=f_title, fill=_INK)

    # Bilgi kutusu (sag ust)
    info = [("Özelleştirme No:", "TR1.2"), ("Senaryo:", "TEMELFATURA"), ("Fatura Tipi:", "SATIŞ"),
            ("Fatura No:", inv.doc_no), ("Fatura Tarihi:", inv.doc_date), ("Düzenleme Saati:", "14:35:08")]
    bx0, bx1, by = 1040, 1590, 70
    if inv.info_layout == "below":
        # etiketler ustte, degerler altta (iki sutunlu kutu)
        pairs = [info[i:i + 2] for i in range(0, len(info), 2)]
        for pair in pairs:
            for j, (label, value) in enumerate(pair):
                x = bx0 + 12 + j * 280
                d.text((x, by + 6), label.rstrip(":"), font=f_bold, fill=_INK)
                d.text((x, by + 40), value, font=f_body, fill=_INK)
            d.rectangle((bx0, by, bx1, by + 76), outline=_INK, width=2)
            d.line((bx0 + 275, by, bx0 + 275, by + 76), fill=_INK, width=2)
            by += 76
    else:
        for label, value in info:
            lx, vx = (bx0 + 12, bx0 + 270) if inv.info_layout == "right" else (bx0 + 300, bx0 + 12)
            d.text((lx, by + 7), label if inv.info_layout == "right" else label.rstrip(":"), font=f_bold, fill=_INK)
            d.text((vx, by + 7), value, font=f_body, fill=_INK)
            d.rectangle((bx0, by, bx1, by + 40), outline=_INK, width=2)
            d.line((bx0 + 260 if inv.info_layout == "right" else bx0 + 290, by,
                    bx0 + 260 if inv.info_layout == "right" else bx0 + 290, by + 40), fill=_INK, width=2)
            by += 40

    # Alici blogu
    y = 470
    d.text((70, y), "SAYIN", font=f_bold, fill=_INK)
    y += 36
    for text in inv.buyer:
        d.text((70, y), text, font=f_small, fill=_INK)
        y += 32
    d.text((70, y + 20), "ETTN: 3f2a9c1e-7b4d-4e8a-9c1f-2d3e4f5a6b7c", font=f_small, fill=_INK)

    # Kalem tablosu
    cols = [("Sıra|No", 70, 140, "c"), ("Mal Hizmet", 140, 580, "l")]
    if inv.unit_column:
        cols += [("Miktar", 580, 690, "r"), ("Birim", 690, 780, "c")]
    else:
        cols += [("Miktar", 580, 780, "r")]
    cols += [("Birim|Fiyat", 780, 930, "r"), ("İskonto|Oranı", 930, 1050, "r"), ("İskonto|Tutarı", 1050, 1190, "r"),
             ("KDV|Oranı", 1190, 1290, "r"), ("KDV|Tutarı", 1290, 1420, "r"), ("Mal Hizmet|Tutarı", 1420, 1590, "r")]
    ty = 720
    head_h = 70
    d.rectangle((70, ty, 1590, ty + head_h), fill=225)
    for title, x0, x1, _al in cols:
        parts = title.split("|")
        for k, part in enumerate(parts):
            tw = _text_w(d, part, f_bold)
            d.text(((x0 + x1 - tw) / 2, ty + 6 + k * 30 + (15 if len(parts) == 1 else 0)), part, font=f_bold, fill=_INK)
    y = ty + head_h
    for n, ln in enumerate(inv.lines, start=1):
        name_lines = _wrap(d, ln.name, f_body, 420)
        row_h = 18 + 32 * len(name_lines)
        qty_text = ln.qty if inv.unit_column else f"{ln.qty} {ln.unit}"
        cur = f" {inv.currency}"
        values = [str(n), None, qty_text] + ([ln.unit] if inv.unit_column else []) + [
            ln.unit_price + cur, ln.disc_pct, ln.disc_amount + cur, ln.vat_pct, ln.vat_amount + cur, ln.total + cur]
        for (title, x0, x1, al), val in zip(cols, values):
            if val is None:
                for k, part in enumerate(name_lines):
                    d.text((x0 + 8, y + 9 + 32 * k), part, font=f_body, fill=_INK)
                continue
            tw = _text_w(d, val, f_body)
            x = x1 - 8 - tw if al == "r" else ((x0 + x1 - tw) / 2 if al == "c" else x0 + 8)
            d.text((x, y + 9), val, font=f_body, fill=_INK)
        y += row_h
        d.line((70, y, 1590, y), fill=_INK, width=2)
    # izgara
    d.rectangle((70, ty, 1590, y), outline=_INK, width=2)
    d.line((70, ty + head_h, 1590, ty + head_h), fill=_INK, width=2)
    for _t, x0, _x1, _a in cols[1:]:
        d.line((x0, ty, x0, y), fill=_INK, width=2)

    # Toplam kutusu (sag alt)
    y += 40
    for label, value in inv.totals:
        d.rectangle((1000, y, 1590, y + 40), outline=_INK, width=2)
        d.line((1400, y, 1400, y + 40), fill=_INK, width=2)
        d.text((1012, y + 8), label, font=f_bold, fill=_INK)
        val = f"{value} {inv.currency}"
        d.text((1580 - _text_w(d, val, f_body), y + 8), val, font=f_body, fill=_INK)
        y += 40
    d.text((70, y + 40), "Not: Ödeme 30 gün içinde yapılacaktır.", font=f_small, fill=_INK)
    return img.convert("RGB")


def photo_degrade(img: Image.Image, angle: float = 2.5, seed: int = 7) -> Image.Image:
    """Telefonla cekilmis fatura benzetimi: egiklik, kose golgesi, bulaniklik, gurultu, JPEG."""
    import numpy as np

    g = img.convert("L").rotate(angle, expand=True, fillcolor=255, resample=Image.BICUBIC)
    a = np.asarray(g, dtype=np.float32)
    h, w = a.shape
    light = np.linspace(1.0, 0.68, w)[None, :] * np.linspace(1.0, 0.82, h)[:, None]
    a = a * light * 0.95 + 6
    a = np.asarray(Image.fromarray(np.clip(a, 0, 255).astype(np.uint8)).filter(ImageFilter.GaussianBlur(1.1)),
                   dtype=np.float32)
    a += np.random.default_rng(seed).normal(0, 7, a.shape)
    buf = io.BytesIO()
    Image.fromarray(np.clip(a, 0, 255).astype(np.uint8)).save(buf, format="JPEG", quality=60)
    return Image.open(io.BytesIO(buf.getvalue())).convert("RGB")
