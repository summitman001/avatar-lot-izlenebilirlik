"""Golden dosyalarini yeniden uretir: python -m oracle.cli [--out DIR]

Uretilen dosyalar (docs/golden/):
- production_cases.json : Gorev 1 senaryolari
- cutting_cases.json    : Gorev 2 senaryolari
- cutting_case.json     : recete varsayilanlari (backend seed okur)
Format: [{"case_id","description","input":{...},"expected":{...}}]
"""
from __future__ import annotations

import argparse
import copy
import json
from pathlib import Path
from typing import Any

from . import cutting_allocation, production_cost
from .decimal_utils import D, dstr, ratio4

DEFAULT_OUT = Path(__file__).resolve().parents[2] / "docs" / "golden"
VKP = "24.90"  # varsayim: bkz. docs/assumptions.md
DEMO_NOTE = (
    "Demo için 287.50 seçildi: birim fiyat (11.50 €/kg) ve ağırlık (25 kg) birbiriyle tutarlı, "
    "hatalı olan tek alan beyan edilen toplam (351.00). 351.00 alternatifi alternative_basis altında raporlanır."
)
DECLARED_NOTE = (
    "Alternatif senaryo: alış belgesinde beyan edilen 351.00 esas alındı; "
    "birim fiyat ve ağırlıkla uyuşmazlık bilinerek kabul edildi."
)

# ---------------------------------------------------------------- Gorev 1

_DOC_LOTS = [
    {"name": "Dana eti", "category": "meat", "quantity": "100", "unit_cost": "9.70"},
    {"name": "Baharat", "category": "spice", "quantity": "2", "unit_cost": "24.00"},
    {"name": "Bağırsak", "category": "casing", "quantity": "5", "unit_cost": "11.60"},
]


def production_cases() -> list[dict[str, Any]]:
    base = {
        "product": "Narin Sucuk",
        "unit_weight_kg": "1.000",
        "lots": _DOC_LOTS,
        "delivered_qty": "85",
        "service_unit_price": "5.50",
        "invoiced_qty": None,
        "sale_price": VKP,
    }
    c1 = copy.deepcopy(base)
    c2 = {**copy.deepcopy(base), "delivered_qty": "83"}
    c3 = {**copy.deepcopy(base), "delivered_qty": "83", "invoiced_qty": "85"}
    c4 = copy.deepcopy(base)
    c5 = {**copy.deepcopy(base), "sale_price": None}
    # 500 g urun: ayni 85 kg cikti 170 adet; hizmet adet basi 2.75 (= 5.50 / 2), VKP 12.45 (= 24.90 / 2)
    c6 = {
        **copy.deepcopy(base),
        "product": "Narin Sucuk 500 g",
        "unit_weight_kg": "0.500",
        "delivered_qty": "170",
        "service_unit_price": "2.75",
        "sale_price": "12.45",
    }
    descriptions = [
        ("UR-001", "Dokuman senaryosu: 85 adet 1 kg Narin Sucuk teslim, 85 faturali, VKP 24.90", c1),
        ("UR-002", "83 teslim, hizmet teslim edilen adet uzerinden faturalanmis", c2),
        ("UR-003", "83 teslim ama uretici 85 adet faturalamis (INVOICED_QTY_MISMATCH)", c3),
        ("UR-004", "Yuvarlama kaniti: ekp_display x adet toplam maliyete esit degil", c4),
        ("UR-005", "Verim bilgisi: 107 kg girdi, 85 adet x 1 kg = 85 kg cikti (satis fiyati yok)", c5),
        ("UR-006", "500 g urun: 170 adet x 0.500 kg = 85 kg cikti, ayni toplam maliyet, adet basi EKP yarisi", c6),
    ]
    out = []
    for case_id, desc, inp in descriptions:
        expected = production_cost.calculate(inp)
        if case_id == "UR-004":
            result = production_cost.calculate_production_cost(**production_cost.parse_input(inp))
            expected["rounding_proof"] = production_cost.rounding_proof(result)
        out.append({"case_id": case_id, "description": desc, "input": inp, "expected": expected})
    return out


# ---------------------------------------------------------------- Gorev 2

# (ad, kg, referans satis fiyati EUR/kg)
_DOC_PARTS = [
    ("But", "7.5", "16.00"),
    ("Kol", "4.5", "15.00"),
    ("Sırt", "2.5", "18.00"),
    ("Kaburga", "3.0", "12.00"),
    ("Gerdan", "2.0", "10.00"),
    ("Kıyma", "2.5", "11.00"),
]


def _part(name: str, kg: str, price: str, waste: bool = False, by_product: bool = False) -> dict[str, Any]:
    return {"name": name, "kg": kg, "reference_price": price, "is_waste": waste, "is_by_product": by_product}


def _doc_parts(with_waste: bool) -> list[dict[str, Any]]:
    parts = [_part(*p) for p in _DOC_PARTS]
    if with_waste:
        parts.append(_part("Fire", "3.0", "0.00", waste=True))
    return parts


def _cutting_input(
    parts: list[dict[str, Any]],
    method: str,
    weight: str = "25",
    unit_cost: str = "11.50",
    declared: str | None = "351.00",
    basis: str | None = "computed",
    note: str | None = None,
) -> dict[str, Any]:
    return {
        "input_weight_kg": weight,
        "unit_cost_per_kg": unit_cost,
        "declared_total_cost": declared,
        "cost_basis": basis,
        "resolution_note": note,
        "method": method,
        "parts": parts,
    }


def cutting_cases() -> list[dict[str, Any]]:
    specs: list[tuple[str, str, dict[str, Any]]] = []
    for method, tag in (("quantity", "QTY"), ("value", "VAL")):
        specs.append(
            (
                f"KZ-2026-001-{tag}",
                f"Dokuman senaryosu, fire kaydedilmemis: 3.0 kg fark, WEIGHT_IMBALANCE, closable=false ({method})",
                _cutting_input(_doc_parts(False), method, note=DEMO_NOTE),
            )
        )
    for method, tag in (("quantity", "QTY"), ("value", "VAL")):
        specs.append(
            (
                f"KZ-2026-002-{tag}",
                f"Ayni senaryo, 3.0 kg fire kaydedilmis, denge tamam, maliyet 287.50 ({method})",
                _cutting_input(_doc_parts(True), method, note=DEMO_NOTE),
            )
        )
    for method, tag in (("quantity", "QTY"), ("value", "VAL")):
        specs.append(
            (
                f"KZ-2026-003-{tag}",
                f"Alis belgesindeki 351.00 maliyet esas alinmis, denge tamam ({method})",
                _cutting_input(_doc_parts(True), method, basis="declared", note=DECLARED_NOTE),
            )
        )
    by_product_parts = [
        _part("But", "7.0", "16.00"),
        _part("Kol", "4.5", "15.00"),
        _part("Sırt", "2.5", "18.00"),
        _part("Kaburga", "3.0", "12.00"),
        _part("Gerdan", "2.0", "10.00"),
        _part("Kıyma", "2.0", "11.00"),
        _part("Ciğer", "1.0", "6.00", by_product=True),
        _part("Fire", "3.0", "0.00", waste=True),
    ]
    for method, tag in (("quantity", "QTY"), ("value", "VAL")):
        specs.append(
            (
                f"KZ-2026-004-{tag}",
                f"Yan urunlu: 1.0 kg ciger (6 EUR/kg) maliyetten dusulur, kalan ana parcalara dagitilir ({method}). "
                "Ciger eklenince parca toplami 22 kg kalsin diye but 7.5 -> 7.0 kg ve kiyma 2.5 -> 2.0 kg indirildi",
                _cutting_input(by_product_parts, method, declared=None),
            )
        )
    tie_parts = [_part("A", "3.0", "10.00"), _part("B", "3.0", "10.00"), _part("C", "3.0", "10.00"),
                 _part("Fire", "1.0", "0.00", waste=True)]
    specs.append(
        (
            "KZ-2026-005-QTY",
            "Kurus farki: 100.00 / 3 esit parca = 33.33 x 3 = 99.99, 0.01 fark ilk (esit) satira eklenir",
            _cutting_input(tie_parts, "quantity", weight="10", unit_cost="10.00", declared=None),
        )
    )
    boundary_parts = [_part("A", "5.00", "10.00"), _part("B", "4.95", "10.00")]
    specs.append(
        (
            "KZ-2026-006-QTY",
            "Tolerans siniri: fark tam 0.05 kg, uyari yok, closable=true",
            _cutting_input(boundary_parts, "quantity", weight="10", unit_cost="10.00", declared=None),
        )
    )
    specs.append(
        (
            "KZ-2026-007-QTY",
            "Uyusmazlik cozumlenmemis: cost_basis ve resolution_note yok, MISMATCH_RESOLUTION_MISSING, closable=false",
            _cutting_input(_doc_parts(True), "quantity", basis=None, note=None),
        )
    )
    return [
        {"case_id": cid, "description": desc, "input": inp, "expected": cutting_allocation.allocate(inp)}
        for cid, desc, inp in specs
    ]


def cutting_recipe() -> dict[str, Any]:
    """Recete varsayilanlari: dokuman senaryosu orani (25 kg karkas) uzerinden."""
    carcass = D("25")
    rows = [(n, D(kg), p, False) for n, kg, p in _DOC_PARTS] + [("Fire", D("3.0"), "0.00", True)]
    parts = [
        {
            "part": name,
            "expected_ratio": dstr(ratio4(kg / carcass)),
            "reference_sale_price": price,
            "is_waste": waste,
            "is_by_product": False,
        }
        for name, kg, price, waste in rows
    ]
    total = sum((D(p["expected_ratio"]) for p in parts), D("0"))
    assert total == D("1"), "recete oranlari toplami 1 olmali"
    return {
        "case_id": "RECIPE-LAMB-DEFAULT",
        "description": "Kuzu parcalama recete varsayilanlari (dokuman senaryosundan, 25 kg karkas)",
        "input": {"animal": "lamb"},
        "expected": {"parts": parts, "ratio_sum": dstr(total)},
    }


# ---------------------------------------------------------------- cikti


def _write(path: Path, data: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8", newline="\n")


def build_all() -> dict[str, Any]:
    return {
        "production_cases.json": production_cases(),
        "cutting_cases.json": cutting_cases(),
        "cutting_case.json": cutting_recipe(),
    }


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(description="Golden dosyalarini uretir")
    parser.add_argument("--out", type=Path, default=DEFAULT_OUT)
    args = parser.parse_args(argv)
    for name, data in build_all().items():
        _write(args.out / name, data)
        print(f"yazildi: {args.out / name}")


if __name__ == "__main__":
    main()
