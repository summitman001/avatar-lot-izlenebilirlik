"""Demo ornek fatura/irsaliye gorselleri uretir: python -m invoicegen.cli [--out DIR]

Uc ornek (varsayilan docs/samples/):
- invoice_clean.png       : temiz, duzenli (yuksek guven beklenir)
- invoice_mismatch.png    : KZ-2026-001 senaryosu -- 25 kg x 11,50 €/kg = 287,50 € ama belge
                            351,00 € yaziyor (TUTAR_UYUSMAZLIGI OCR uzerinden de yakalanmali)
- invoice_low_quality.png : egri taranmis + gurultulu, LOT/SKT yok (dusuk guven, bazi alanlar
                            bos beklenir -- sistemin durust davrandiginin kaniti)

Hayali firma adlari kullanilir; gercek bir firmanin faturasi DEGILDIR.
"""
from __future__ import annotations

import argparse
from pathlib import Path
from typing import TYPE_CHECKING

from .render import EfInvoice, EfLine, Line, efatura_pdf, photo_degrade, render_efatura, render_invoice

if TYPE_CHECKING:
    from PIL import Image

DEFAULT_OUT = Path(__file__).resolve().parents[2] / "docs" / "samples"


def build_clean() -> "Image.Image":
    return render_invoice(
        supplier="Atasoy Et ve Şarküteri Ürünleri San. Tic. A.Ş.",
        doc_no="HY-F-0002",
        doc_date="2 Kasım 2026",
        lines=[
            Line("Dana Eti", "100,000", "kg", "9,70", "970,00", lot="ET-2026-010", skt="15.03.2027"),
            Line("Sucuk Baharatı", "2,000", "kg", "24,00", "48,00", lot="BHR-2026-003"),
        ],
        grand_total="1.018,00",
    )


def build_mismatch() -> "Image.Image":
    return render_invoice(
        supplier="Yayla Hayvancılık Gıda San. Tic. Ltd. Şti.",
        doc_no="HY-F-0001",
        doc_date="02.03.2026",
        lines=[
            Line("Bütün Kuzu", "25,000", "kg", "11,50", "351,00", lot="KZ-2026-001", skt="09.03.2026"),
        ],
        grand_total="351,00",
    )


def build_low_quality() -> "Image.Image":
    return render_invoice(
        supplier="Narin Gıda Üretim ve Paz. Ltd. Şti.",
        doc_no="NRN-2026-077",
        doc_date="05/11/2026",
        lines=[
            Line("Dana Kıyma", "40,000", "kg", "12,25", "490,00"),
            Line("Koyun Eti", "15,000", "kg", "14,80", "222,00"),
        ],
        grand_total="712,00",
        degraded=True,
    )


def efatura_invoice(**overrides) -> EfInvoice:
    """GIB e-Fatura benzeri ornek: iki satirlik unvan, alt satira tasan urun adi (adinda "1 kg"
    gecer), %10 iskontolu satir, ad icinde LOT/SKT, iki farkli KDV orani ve KDV'li toplam kutusu.

    Beklenen: Mal Hizmet Toplam = 174,00 + 202,50 + 96,00 = 472,50; KDV %1 = 3,77, %10 = 9,60;
    Odenecek = 485,87. Iskontolu satirin net birim fiyati 202,50 / 20 = 10,125.
    """
    spec = dict(
        seller=["AKYAZI ET VE ET ÜRÜNLERİ GIDA", "SANAYİ VE TİCARET LİMİTED ŞİRKETİ",
                "Organize Sanayi Bölgesi 3. Cad. No:14", "Kemalpaşa / İZMİR",
                "Tel: 0232 555 12 34  Fax: 0232 555 12 35", "Vergi Dairesi: KEMALPAŞA  VKN: 0123456789",
                "Mersis No: 0012345678900015"],
        seller_name_lines=2,
        doc_no="AKY2026000000145",
        doc_date="01-11-2026",
        lines=[
            EfLine("Kuzu But Kemikli Vakumlu Paket Özel Kesim 1 kg'lık", "12", "KG", "14,50", "174,00",
                   vat_amount="1,74"),
            EfLine("Dana Kıyma (LOT: DK-2026-118)", "20", "KG", "11,25", "202,50", disc_pct="%10",
                   disc_amount="22,50", vat_amount="2,03"),
            EfLine("Sucuk Baharatı SKT: 10.05.2027", "4", "ADET", "24,00", "96,00", vat_pct="%10",
                   vat_amount="9,60"),
        ],
        totals=[("Mal Hizmet Toplam Tutarı", "472,50"), ("Toplam İskonto", "22,50"),
                ("Hesaplanan KDV(%1)", "3,77"), ("Hesaplanan KDV(%10)", "9,60"),
                ("Vergiler Dahil Toplam Tutar", "485,87"), ("Ödenecek Tutar", "485,87")],
    )
    spec.update(overrides)
    return EfInvoice(**spec)


def build_efatura(**overrides) -> "Image.Image":
    return render_efatura(efatura_invoice(**overrides))


def build_efatura_photo() -> "Image.Image":
    return photo_degrade(build_efatura())


def build_efatura_pdf(**overrides) -> bytes:
    """Ayni faturanin metin katmanli (dijital) PDF'i: portaldan indirilen e-Fatura gibi."""
    return efatura_pdf(efatura_invoice(**overrides))


BUILDERS = {
    "invoice_clean.png": build_clean,
    "invoice_mismatch.png": build_mismatch,
    "invoice_low_quality.png": build_low_quality,
}


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(description="Demo ornek fatura gorselleri uretir")
    parser.add_argument("--out", type=Path, default=DEFAULT_OUT)
    args = parser.parse_args(argv)
    args.out.mkdir(parents=True, exist_ok=True)
    for name, builder in BUILDERS.items():
        path = args.out / name
        builder().save(path)
        print(f"yazildi: {path}")


if __name__ == "__main__":
    main()
