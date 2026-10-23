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

# ---------------------------------------------------------------- Gorev 1

_DOC_LOTS = [
    {"name": "Dana eti", "category": "meat", "quantity": "100", "unit_cost": "9.70"},
    {"name": "Baharat", "category": "spice", "quantity": "2", "unit_cost": "24.00"},
    {"name": "Bağırsak", "category": "casing", "quantity": "5", "unit_cost": "11.60"},
]


def production_cases() -> list[dict[str, Any]]:
    base = {
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
    descriptions = [
        ("UR-001", "Dokuman senaryosu: 85 teslim, 85 faturali, VKP 24.90", c1),
        ("UR-002", "83 teslim, hizmet teslim edilen adet uzerinden faturalanmis", c2),
        ("UR-003", "83 teslim ama uretici 85 adet faturalamis (INVOICED_QTY_MISMATCH)", c3),
        ("UR-004", "Yuvarlama kaniti: ekp_display x adet toplam maliyete esit degil", c4),
        ("UR-005", "Verim bilgisi: 107 kg girdi, 85 kg cikti (satis fiyati yok)", c5),
    ]
    out = []
    for case_id, desc, inp in descriptions:
        expected = production_cost.calculate(inp)
        if case_id == "UR-004":
            result = _result_for(inp)
            expected["rounding_proof"] = production_cost.rounding_proof(result)
        out.append({"case_id": case_id, "description": desc, "input": inp, "expected": expected})
    return out


def _result_for(inp: dict[str, Any]) -> production_cost.ProductionResult:
    lots = [
        production_cost.Lot(r["name"], r["category"], D(r["quantity"]), D(r["unit_cost"])) for r in inp["lots"]
    ]
    return production_cost.calculate_production_cost(
        lots,
        D(inp["delivered_qty"]),
        D(inp["service_unit_price"]),
        D(inp["invoiced_qty"]) if inp.get("invoiced_qty") is not None else None,
        D(inp["sale_price"]) if inp.get("sale_price") is not None else None,
    )


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
    basis: str = "computed",
) -> dict[str, Any]:
    return {
        "input_weight_kg": weight,
        "unit_cost_per_kg": unit_cost,
        "declared_total_cost": declared,
        "cost_basis": basis,
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
                _cutting_input(_doc_parts(False), method),
            )
        )
    for method, tag in (("quantity", "QTY"), ("value", "VAL")):
        specs.append(
            (
                f"KZ-2026-002-{tag}",
                f"Ayni senaryo, 3.0 kg fire kaydedilmis, denge tamam, maliyet 287.50 ({method})",
                _cutting_input(_doc_parts(True), method),
            )
        )
    for method, tag in (("quantity", "QTY"), ("value", "VAL")):
        specs.append(
            (
                f"KZ-2026-003-{tag}",
                f"Alis belgesindeki 351.00 maliyet esas alinmis, denge tamam ({method})",
                _cutting_input(_doc_parts(True), method, basis="declared"),
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
                f"Yan urunlu: 1.0 kg ciger (6 EUR/kg) maliyetten dusulur, kalan ana parcalara dagitilir ({method})",
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
