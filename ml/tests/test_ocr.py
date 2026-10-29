"""Fatura/irsaliye OCR testleri (bkz. docs/invoice-ocr.md).

Goruntu uretimi icin ml/invoicegen (ayni modul docs/samples/ ornek faturalarini da uretir).
"""
from decimal import Decimal
from io import BytesIO

import pytest

from app import ocr
from invoicegen.cli import build_clean, build_low_quality, build_mismatch


def _png_bytes(img) -> bytes:
    buf = BytesIO()
    img.save(buf, format="PNG")
    return buf.getvalue()


# ---------------------------------------------------------------- sayi / tarih ayristirma


@pytest.mark.parametrize(
    "raw,fmt,expected",
    [
        ("1.543,50", "tr", Decimal("1543.50")),
        ("287,50", "tr", Decimal("287.50")),
        ("25,000", "tr", Decimal("25.000")),
        ("1,543.50", "us", Decimal("1543.50")),
        ("287.50", "us", Decimal("287.50")),
    ],
)
def test_parse_amount(raw, fmt, expected):
    assert ocr.parse_amount(raw, fmt) == expected


def test_parse_amount_bos_girdi_none_doner_hata_firlatmaz():
    assert ocr.parse_amount("", "tr") is None
    assert ocr.parse_amount("abc", "tr") is None


def test_detect_number_format_tr():
    assert ocr.detect_number_format("Tutar: 1.543,50 TOPLAM: 287,50") == "tr"


def test_detect_number_format_us():
    assert ocr.detect_number_format("Amount: 1,543.50 TOTAL: 287.50") == "us"


@pytest.mark.parametrize(
    "text,expected",
    [
        ("Tarih: 01.11.2026", (2026, 11, 1)),
        ("Tarih: 01/11/2026", (2026, 11, 1)),
        ("1 Kasım 2026 tarihinde", (2026, 11, 1)),
        ("9 Mart 2026", (2026, 3, 9)),
    ],
)
def test_parse_turkish_date_formats(text, expected):
    found = ocr.parse_turkish_date(text)
    assert found is not None
    d, _source = found
    assert (d.year, d.month, d.day) == expected


def test_parse_turkish_date_bulunamazsa_none():
    assert ocr.parse_turkish_date("bu metinde tarih yok") is None


# ---------------------------------------------------------------- satir ayristirma (metin, OCR'siz)


def test_parse_line_row_temel_satir():
    row = "Dana Eti          25,000 kg      11,50 €      287,50 €      LOT: KZ-2026-001      SKT: 01.11.2026"
    parsed = ocr.parse_line_row(row)
    assert parsed is not None
    assert parsed["name"] == "Dana Eti"
    assert parsed["qty_raw"] == "25,000" and parsed["unit"] == "kg"
    assert parsed["price_raw"].replace(" ", "") == "11,50€" or parsed["price_raw"] == "11,50"
    assert parsed["lot_raw"] == "KZ-2026-001"
    assert parsed["skt_raw"] == "01.11.2026"


def test_parse_line_row_baslik_satiri_eslesmez():
    assert ocr.parse_line_row("Ürün          Miktar      Birim Fiyat     Tutar        Lot         SKT") is None


def test_parse_line_row_bos_satir_eslesmez():
    assert ocr.parse_line_row("") is None
    assert ocr.parse_line_row("GENEL TOPLAM: 287,50 €") is None


# ---------------------------------------------------------------- uctan uca (gercek Tesseract gerektirir)


@pytest.fixture(scope="module")
def clean_result():
    return ocr.extract_invoice(_png_bytes(build_clean()), "image/png", "invoice_clean.png")


@pytest.fixture(scope="module")
def mismatch_result():
    return ocr.extract_invoice(_png_bytes(build_mismatch()), "image/png", "invoice_mismatch.png")


@pytest.fixture(scope="module")
def low_quality_result():
    return ocr.extract_invoice(_png_bytes(build_low_quality()), "image/png", "invoice_low_quality.png")


def test_temiz_fatura_kritik_alanlar_okunur(clean_result):
    r = clean_result
    assert r.overall_confidence > 0
    assert r.supplier_name.value and "Atasoy" in r.supplier_name.value
    assert r.document_date.value == "2026-11-02"
    assert len(r.lines) == 2
    names = [l.product_name.value for l in r.lines]
    assert any("Dana" in (n or "") for n in names)
    for line in r.lines:
        assert line.quantity.value is not None
        assert line.unit_price.value is not None
        assert line.line_total.value is not None
        assert line.warnings == []  # miktar x birim fiyat = tutar, tutarsizlik yok


def test_temiz_fatura_lot_ve_skt_okunur(clean_result):
    lot_values = [l.lot_no.value for l in clean_result.lines]
    assert "ET-2026-010" in lot_values
    skt_values = [l.expiry_date.value for l in clean_result.lines]
    assert "2027-03-15" in skt_values


def test_tutar_uyusmazligi_ocr_uzerinden_yakalanir(mismatch_result):
    r = mismatch_result
    assert len(r.lines) == 1
    line = r.lines[0]
    # 25 x 11,50 = 287,50 ama belgede 351,00 yaziyor -- KZ-2026-001 senaryosu
    assert line.quantity.value == "25.000"
    assert line.unit_price.value == "11.50"
    assert line.line_total.value == "351.00"
    assert any("TUTAR_UYUSMAZLIGI" in w for w in line.warnings)


def test_dusuk_kaliteli_belge_dusuk_guven_ve_bos_alanlar(low_quality_result):
    r = low_quality_result
    # LOT/SKT bu belgede hic yazilmadi: sistem UYDURMAZ, bos + guven 0 doner
    for line in r.lines:
        assert line.lot_no.value is None
        assert line.expiry_date.value is None
    assert r.overall_confidence < 1.0


# ---------------------------------------------------------------- hata firlatmama garantisi


def test_bozuk_dosya_hata_firlatmaz_dusuk_guvenli_bos_yanit_doner():
    result = ocr.extract_invoice(b"bu bir resim degil, rastgele bayt dizisidir", "image/png", "bozuk.png")
    assert result.overall_confidence == 0.0
    assert result.lines == []
    assert result.supplier_name.value is None
    assert result.warnings


def test_bos_dosya_hata_firlatmaz():
    result = ocr.extract_invoice(b"", "image/png", "bos.png")
    assert result.overall_confidence == 0.0
    assert result.lines == []
