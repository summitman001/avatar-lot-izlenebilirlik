"""Fatura/irsaliye OCR testleri (bkz. docs/invoice-ocr.md).

Goruntu uretimi icin ml/invoicegen (ayni modul docs/samples/ ornek faturalarini da uretir).
"""
import re
from decimal import Decimal
from io import BytesIO

import pytest

from app import ocr
from invoicegen.cli import build_clean, build_efatura, build_efatura_photo, build_low_quality, build_mismatch


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


# ---------------------------------------------------------------- yerlesim kurallari (OCR'siz, kelime kutucuklariyla)
#
# Her (y, [(x, metin), ...]) satirindaki her parca AYRI bir Tesseract satiri gibi verilir: Tesseract
# etiket/deger kutularini ve tablo sutunlarini ayri bloklara bolebiliyor, kurallar buna dayanikli olmali.


def _layout(lines):
    words = []
    for li, (y, segments) in enumerate(lines):
        for si, (x, text) in enumerate(segments):
            for m in re.finditer(r"\S+", text):
                words.append(ocr._Word(m.group(0), 0.95, x + m.start() * 12, y, len(m.group(0)) * 12, 20, (li, si)))
    return ocr._extract_from_words(words)


def test_belge_no_ve_tarih_etiketin_solunda():
    r = _layout([
        (20, [(40, "AKYAZI ET GIDA SAN. TIC. LTD. STI.")]),
        (60, [(40, "Organize Sanayi Bolgesi No:14"), (900, "AKY2026000000145"), (1150, "Fatura No")]),
        (90, [(900, "01.11.2026"), (1150, "Fatura Tarihi")]),
    ])
    assert r.document_no.value == "AKY2026000000145"
    assert r.document_date.value == "2026-11-01"


@pytest.mark.parametrize("label", ["Fatura No:", "Fatura No.", "Fatura Numarası:", "FATURA NO :", "Fatura No"])
def test_belge_no_etiket_yazim_cesitleri(label):
    r = _layout([(20, [(40, "Yayla Et Ltd. Sti.")]), (60, [(700, label), (950, "YAY2026000000012")])])
    assert r.document_no.value == "YAY2026000000012"


def test_belge_no_ve_tarih_etiketin_altinda():
    r = _layout([
        (20, [(40, "ABC GIDA A.S.")]),
        (60, [(700, "Fatura No"), (950, "Fatura Tarihi")]),
        (90, [(700, "ABC2026000000777"), (950, "1 Kasım 2026")]),
    ])
    assert r.document_no.value == "ABC2026000000777"
    assert r.document_date.value == "2026-11-01"


def test_belge_no_bossa_baska_alan_alinmaz():
    # numara okunamamis: yanindaki tarih, ETTN ya da Ozellestirme No belge no diye ONERILMEZ
    r = _layout([
        (20, [(40, "ABC GIDA A.S.")]),
        (60, [(700, "Ozellestirme No:"), (950, "TR1.2")]),
        (90, [(700, "Fatura No:"), (950, "Tarih: 01.11.2026")]),
        (120, [(40, "ETTN: 3f2a9c1e-7b4d-4e8a-9c1f-2d3e4f5a6b7c")]),
    ])
    assert r.document_no.value is None
    assert r.document_date.value == "2026-11-01"


def test_gib_numarasi_etiketsiz_ve_ocr_harf_karisikligi_duzeltilir():
    r = _layout([(20, [(40, "ABC GIDA A.S.")]), (60, [(700, "Belge: AKY2O26OOOOOO145")])])
    assert r.document_no.value == "AKY2026000000145"


def test_matbu_fatura_seri_sira_no():
    r = _layout([(20, [(40, "ABC GIDA A.S.")]), (60, [(700, "Seri: A   Sira No: 123456")])])
    assert r.document_no.value == "A-123456"


def test_vade_tarihi_belge_tarihi_sayilmaz():
    r = _layout([
        (20, [(40, "ABC GIDA A.S.")]),
        (60, [(40, "Vade Tarihi: 30.11.2026")]),
        (90, [(40, "Fatura Tarihi: 03 KAS 2026")]),
    ])
    assert r.document_date.value == "2026-11-03"


@pytest.mark.parametrize(
    "text,expected",
    [("05/11/26", (2026, 11, 5)), ("2026-11-05", (2026, 11, 5)), ("05,11.2026", (2026, 11, 5)),
     ("5 Kas. 2026", (2026, 11, 5)), ("05 Nov 2026", (2026, 11, 5)), ("11/25/2026", (2026, 11, 25))],
)
def test_tarih_yazim_cesitleri(text, expected):
    d, _src = ocr.parse_turkish_date(text)
    assert (d.year, d.month, d.day) == expected


def test_tutar_tarih_sanilmaz():
    assert ocr.parse_turkish_date("Tutar 1.543,50 TL") is None


def test_alici_blogu_tedarikci_sanilmaz():
    r = _layout([
        (20, [(600, "e-FATURA")]),
        (60, [(40, "Kemalpasa Cad. No:12 Bornova / IZMIR")]),
        (100, [(40, "SAYIN")]),
        (130, [(40, "Avatar Gida Sanayi ve Ticaret A.S.")]),
    ])
    assert r.supplier_name.value is None


def test_satici_etiketi_alici_unvani_degil():
    r = _layout([
        (20, [(40, "Alici Unvani: Avatar Gida A.S.")]),
        (60, [(40, "Satici: Yayla Et Ltd. Sti.")]),
    ])
    assert r.supplier_name.value == "Yayla Et Ltd. Sti."


def test_iki_satira_bolunmus_unvan_birlestirilir():
    r = _layout([
        (20, [(40, "AKYAZI ET VE ET URUNLERI GIDA"), (900, "Senaryo: TEMELFATURA")]),
        (44, [(40, "SANAYI VE TICARET LIMITED SIRKETI")]),
        (70, [(40, "Tel: 0232 555 12 34")]),
    ])
    assert r.supplier_name.value == "AKYAZI ET VE ET URUNLERI GIDA SANAYI VE TICARET LIMITED SIRKETI"


@pytest.mark.parametrize(
    "raw,fmt,expected",
    [("12.50", "tr", Decimal("12.50")), ("1.234.567", "tr", Decimal("1234567")), ("1 543,50", "tr", Decimal("1543.50")),
     ("1.543,5O", "tr", Decimal("1543.50")), ("-12,50", "tr", Decimal("-12.50")), ("12,50 TL", "tr", Decimal("12.50")),
     ("€1.018,00", "tr", Decimal("1018.00")), ("1,5", "us", Decimal("1.5"))],
)
def test_parse_amount_ocr_ve_bicim_cesitleri(raw, fmt, expected):
    assert ocr.parse_amount(raw, fmt) == expected


# e-Fatura tablosu: iki satirli basliklar, ayri birim sutunu, KDV/iskonto sutunlari
_TABLE = [
    (20, [(40, "Yayla Hayvancilik Ltd. Sti.")]),
    (60, [(700, "Fatura No:"), (950, "YAY2026000000012")]),
    (300, [(40, "Sira"), (140, "Mal Hizmet"), (560, "Miktar"), (680, "Birim"), (780, "Birim"), (930, "Iskonto"),
           (1080, "KDV"), (1200, "Mal Hizmet")]),
    (322, [(40, "No"), (780, "Fiyat"), (930, "Orani"), (1080, "Orani"), (1200, "Tutari")]),
    (360, [(40, "1"), (140, "Kuzu But Vakum 1 kg'lik"), (600, "12"), (690, "KG"), (790, "14,50"), (950, "%0"),
           (1090, "%1"), (1230, "174,00")]),
    (384, [(140, "Ozel Kesim")]),
    (420, [(40, "2"), (140, "Dana Kiyma"), (600, "20"), (690, "KG"), (790, "11,25"), (950, "%10"), (1090, "%1"),
           (1230, "202,50")]),
    (460, [(40, "3"), (140, "Koyun Eti SKT: 10.05.2027"), (600, "15"), (690, "KG"), (790, "1480"), (950, "%0"),
           (1090, "%1"), (1230, "222,00")]),
    (520, [(900, "Mal Hizmet Topiam Tutari"), (1230, "598,50")]),
    (550, [(900, "Hesaplanan KDV(%1)"), (1230, "5,99")]),
    (580, [(900, "Odenecek Tutar"), (1230, "604,49")]),
]


def test_tablo_sutunlari_iskonto_alt_satira_tasan_ad_ve_virgul_kaybi():
    r = _layout(_TABLE)
    assert len(r.lines) == 3  # "Topiam" (OCR bozuk "Toplam") satiri kalem sayilmaz
    kuzu, dana, koyun = r.lines
    # adin icindeki "1 kg" miktar sanilmaz; alt satira tasan ad eklenir
    assert kuzu.product_name.value == "Kuzu But Vakum 1 kg'lik Ozel Kesim"
    assert (kuzu.quantity.value, kuzu.unit.value, kuzu.unit_price.value, kuzu.line_total.value) == \
        ("12", "kg", "14.50", "174.00")
    assert kuzu.warnings == []
    # %10 iskonto: net birim fiyat onerilir, kullanici uyarilir
    assert (dana.unit_price.value, dana.line_total.value) == ("10.125", "202.50")
    assert any(w.startswith("ISKONTO_UYGULANDI") for w in dana.warnings)
    # "14,80"in virgulu okunmamis ("1480"): aritmetikle duzeltilir ama kullaniciya soylenir
    assert (koyun.unit_price.value, koyun.expiry_date.value) == ("14.80", "2027-05-10")
    assert any(w.startswith("ONDALIK_DUZELTILDI") for w in koyun.warnings)
    assert koyun.product_name.value == "Koyun Eti"
    # satir toplami Mal Hizmet Toplami'na esit; KDV'li odenecek tutar yuzunden yanlis alarm yok
    assert not any(w.startswith("TOPLAM_UYUSMAZLIGI") for w in r.warnings)


def test_belge_toplami_tutmazsa_uyarilir():
    # ne Mal Hizmet Toplami ne de Odenecek - KDV satir toplamini (598,50) tutuyor: eksik/yanlis satir isareti
    changed = {520: (520, [(900, "Mal Hizmet Toplam Tutari"), (1230, "650,00")]),
               580: (580, [(900, "Odenecek Tutar"), (1230, "655,99")])}
    r = _layout([changed.get(row[0], row) for row in _TABLE])
    assert any(w.startswith("TOPLAM_UYUSMAZLIGI") for w in r.warnings)


def test_ad_sayilarin_ustundeki_satirda():
    r = _layout([
        (20, [(40, "Yayla Et Ltd. Sti.")]),
        (300, [(140, "Urun Adi"), (560, "Miktar"), (780, "Birim Fiyat"), (1200, "Tutar")]),
        (340, [(140, "Bütün Kuzu Karkas")]),
        (364, [(600, "25 kg"), (790, "11,50"), (1230, "287,50")]),
    ])
    assert len(r.lines) == 1
    assert r.lines[0].product_name.value == "Bütün Kuzu Karkas"
    assert (r.lines[0].quantity.value, r.lines[0].line_total.value) == ("25", "287.50")


def test_basliksiz_satir_aritmetikle_ayristirilir():
    parsed = ocr.parse_line_row("Narin Sucuk 1 kg'lik paket      85 adet      5,50      467,50")
    assert parsed is not None
    assert parsed["name"] == "Narin Sucuk 1 kg'lik paket"
    assert (parsed["qty_raw"], parsed["unit"], parsed["price_raw"], parsed["total_raw"]) == ("85", "adet", "5,50", "467,50")


@pytest.mark.parametrize("cell,lot", [("LOT:KZ-2026-001", "KZ-2026-001"), ("Parti No: 2026/45", "2026/45"),
                                      ("(Lot: DK-2026-118)", "DK-2026-118")])
def test_lot_yazim_cesitleri(cell, lot):
    parsed = ocr.parse_line_row(f"Dana Eti {cell}      10 kg      9,70      97,00")
    assert parsed is not None and parsed["lot_raw"] == lot
    assert parsed["name"] == "Dana Eti"


# ---------------------------------------------------------------- uctan uca: e-Fatura benzeri belgeler (Tesseract)


def _efatura_checks(r):
    """Tum farklari birlikte raporlar (CI'da farkli Tesseract surumu/fontu tek turda gorunsun)."""
    errs = []

    def check(name, got, ok):
        if not ok:
            errs.append(f"{name}: {got!r}")

    sup = ocr._key(r.supplier_name.value or "")
    check("tedarikci", r.supplier_name.value, "AKYAZI" in sup and "SIRKETI" in sup)
    check("belge no", r.document_no.value, r.document_no.value == "AKY2026000000145")
    check("tarih", r.document_date.value, r.document_date.value == "2026-11-01")
    check("para birimi", r.currency.value, r.currency.value == "TRY")
    check("toplam uyarisi", r.warnings, not any(w.startswith("TOPLAM_UYUSMAZLIGI") for w in r.warnings))
    check("satir sayisi", [l.product_name.value for l in r.lines], len(r.lines) == 3)
    if len(r.lines) == 3:
        kuzu, dana, sucuk = r.lines
        name = ocr._key(kuzu.product_name.value or "")
        # OCR kelime araligi/yazimi surume gore degisir ("KuzuBut"); kural hatasi ise ada sizan birim
        # ("Paket KG Ozel") ya da bozuk okunmus yuzde ("Paket O Ozel") olur
        check("kuzu adi", kuzu.product_name.value, "KUZU" in name and "KESIM" in name
              and not any(tok in ("KG", "ADET") or len(tok) == 1 and tok.isalpha() for tok in name.split()))
        check("kuzu sayilar", (kuzu.quantity.value, kuzu.unit.value, kuzu.unit_price.value, kuzu.line_total.value),
              (kuzu.quantity.value, kuzu.unit.value, kuzu.unit_price.value, kuzu.line_total.value)
              == ("12", "kg", "14.50", "174.00"))
        check("dana", (dana.unit_price.value, dana.line_total.value, dana.lot_no.value),
              (dana.unit_price.value, dana.line_total.value, dana.lot_no.value) == ("10.125", "202.50", "DK-2026-118"))
        check("sucuk", (sucuk.quantity.value, sucuk.unit.value, sucuk.expiry_date.value),
              (sucuk.quantity.value, sucuk.unit.value, sucuk.expiry_date.value) == ("4", "adet", "2027-05-10"))
    assert errs == []


@pytest.mark.parametrize("kwargs", [
    {},  # degerler etiketin saginda, birim miktarla ayni hucrede
    {"info_layout": "left", "unit_column": True, "doc_date": "01.11.2026"},  # deger SOLDA, ayri birim sutunu
    {"info_layout": "below"},  # deger etiketin ALTINDA
], ids=["sag", "sol-birim-sutunu", "alt"])
def test_efatura_duzenleri(kwargs):
    _efatura_checks(ocr.extract_invoice(_png_bytes(build_efatura(**kwargs)), "image/png", "efatura.png"))


def test_efatura_telefon_fotografi():
    # egik, golgeli, bulanik, JPEG -- on isleme (golge, egiklik, cizgi silme) olmadan okunamiyordu
    buf = BytesIO()
    build_efatura_photo().save(buf, format="JPEG", quality=90)
    _efatura_checks(ocr.extract_invoice(buf.getvalue(), "image/jpeg", "foto.jpg"))
