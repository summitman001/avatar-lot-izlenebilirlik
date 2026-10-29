"""Demo fatura/irsaliye gorselleri cizer (docs/samples/ ve ml/tests/test_ocr.py tarafindan kullanilir).

Hayali firma adlari kullanilir; gercek bir firmanin faturasi DEGILDIR. Sabit, OCR'a uygun bir
sutun duzeni (etiketler + genis bosluklarla ayrilmis sutunlar) cizer -- bkz. app/ocr.py
parse_line_row (sutunlari 2+ bosluk/tab ile ayirir).
"""
from __future__ import annotations

from dataclasses import dataclass

from PIL import Image, ImageDraw, ImageFilter, ImageFont

FONT_PATH = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
FONT_BOLD_PATH = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"

WIDTH, HEIGHT = 1700, 900

# Satir basi 2 satir: 1) ad + miktar + birim fiyat + tutar (genis bosluklu 4 sutun), 2) LOT:/SKT:
# (varsa, kendi satirinda -- aralarinda buyuk bosluk). Tek satirda 6 sutunu sikistirmak komsu
# sutunlarin OCR'da birbirine karismasina (kelime sinirlarinin kaybolmasina) yol aciyordu.
COL_X = [60, 620, 1000, 1280]
HEADERS = ["Ürün", "Miktar", "Birim Fiyat", "Tutar"]
META_LOT_X = 100
META_SKT_X = 780


def _font(size: int, bold: bool = False) -> ImageFont.ImageFont:
    path = FONT_BOLD_PATH if bold else FONT_PATH
    try:
        return ImageFont.truetype(path, size)
    except OSError:  # pragma: no cover - imajda fonts-dejavu-core her zaman kurulu
        return ImageFont.load_default()


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
