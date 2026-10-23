"""Gorev 2: kuzu parcalama ve maliyet dagitimi.

Kurallar:
- agirlik dengesi: balance_diff_kg = giris_kg - sum(tum parcalar, fire ve yan urun dahil)
  |diff| > 0.05 kg ise WEIGHT_IMBALANCE uyarisi ve closable=false
- fire (is_waste): maliyet tasimaz (0.00), maliyeti satilabilir parcalara yuklenir
- yan urun (is_by_product): kendi satis degeri kadar maliyet tasir (kg * referans fiyat, 2 ondalik),
  bu tutar toplam maliyetten dusulur; kalan havuz ana parcalara dagitilir (net realizable value)
- quantity yontemi: ana parcalara kg oraninda, value yontemi: kg * referans fiyat oraninda
- dagitim yuvarlamasi: oracle.rounding.distribute (toplam kaynaga birebir esit)
"""
from __future__ import annotations

from dataclasses import dataclass
from decimal import Decimal
from typing import Any

from .decimal_utils import D, dstr, money, pct2, qty3, unit4
from .rounding import distribute

WEIGHT_TOLERANCE_KG = Decimal("0.05")
WEIGHT_IMBALANCE = "WEIGHT_IMBALANCE"
PURCHASE_TOTAL_MISMATCH = "PURCHASE_TOTAL_MISMATCH"
METHODS = ("quantity", "value")
COST_BASES = ("computed", "declared")


@dataclass(frozen=True)
class Part:
    name: str
    kg: Decimal
    reference_price: Decimal  # EUR/kg
    is_waste: bool = False
    is_by_product: bool = False

    @property
    def role(self) -> str:
        if self.is_waste:
            return "waste"
        if self.is_by_product:
            return "by_product"
        return "main"


def allocate_cutting(
    input_weight_kg: Decimal,
    parts: list[Part],
    method: str,
    unit_cost_per_kg: Decimal | None = None,
    declared_total_cost: Decimal | None = None,
    cost_basis: str = "computed",
) -> dict[str, Any]:
    if method not in METHODS:
        raise ValueError(f"gecersiz yontem: {method}")
    if cost_basis not in COST_BASES:
        raise ValueError(f"gecersiz maliyet esasi: {cost_basis}")
    if input_weight_kg <= 0:
        raise ValueError("giris agirligi sifirdan buyuk olmali")
    if not parts:
        raise ValueError("en az bir parca gerekli")
    for p in parts:
        if p.is_waste and p.is_by_product:
            raise ValueError(f"parca hem fire hem yan urun olamaz: {p.name}")
        if p.kg < 0 or p.reference_price < 0:
            raise ValueError(f"negatif deger: {p.name}")

    computed_total = money(input_weight_kg * unit_cost_per_kg) if unit_cost_per_kg is not None else None
    if cost_basis == "computed":
        if computed_total is None:
            raise ValueError("computed esasi icin unit_cost_per_kg gerekli")
        total_cost = computed_total
    else:
        if declared_total_cost is None:
            raise ValueError("declared esasi icin declared_total_cost gerekli")
        total_cost = money(declared_total_cost)

    warnings: list[str] = []
    if computed_total is not None and declared_total_cost is not None and computed_total != money(declared_total_cost):
        warnings.append(PURCHASE_TOTAL_MISMATCH)

    parts_weight = sum((p.kg for p in parts), Decimal(0))
    balance_diff = input_weight_kg - parts_weight
    closable = abs(balance_diff) <= WEIGHT_TOLERANCE_KG
    if not closable:
        warnings.append(WEIGHT_IMBALANCE)

    # Yan urun: kendi satis degeri kadar maliyet tasir
    by_product_cost = {i: money(p.kg * p.reference_price) for i, p in enumerate(parts) if p.role == "by_product"}
    pool = total_cost - sum(by_product_cost.values(), Decimal(0))
    if pool < 0:
        raise ValueError("yan urun degeri toplam maliyeti asiyor")

    main_idx = [i for i, p in enumerate(parts) if p.role == "main"]
    if not main_idx:
        raise ValueError("en az bir ana parca gerekli")
    if method == "quantity":
        weights = [parts[i].kg for i in main_idx]
    else:
        weights = [parts[i].kg * parts[i].reference_price for i in main_idx]
    main_alloc = dict(zip(main_idx, distribute(pool, weights, 2)))

    out_parts = []
    total_allocated = Decimal(0)
    for i, p in enumerate(parts):
        if p.role == "main":
            cost = main_alloc[i]
        elif p.role == "by_product":
            cost = by_product_cost[i]
        else:
            cost = Decimal("0.00")
        total_allocated += cost
        out_parts.append(
            {
                "name": p.name,
                "role": p.role,
                "kg": dstr(qty3(p.kg)),
                "yield_pct": dstr(pct2(p.kg / input_weight_kg * 100)),
                "allocated_cost": dstr(cost),
                "unit_cost": dstr(unit4(cost / p.kg)) if p.kg > 0 else "0.0000",
            }
        )
    assert total_allocated == total_cost, "dagitim toplami maliyete esit olmali"

    sellable_kg = sum((p.kg for p in parts if p.role == "main"), Decimal(0))
    return {
        "method": method,
        "cost_basis": cost_basis,
        "input_weight_kg": dstr(qty3(input_weight_kg)),
        "computed_total_cost": None if computed_total is None else dstr(computed_total),
        "declared_total_cost": None if declared_total_cost is None else dstr(money(declared_total_cost)),
        "total_input_cost": dstr(total_cost),
        "parts_weight_kg": dstr(qty3(parts_weight)),
        "balance_diff_kg": dstr(qty3(balance_diff)),
        "closable": closable,
        "sellable_kg": dstr(qty3(sellable_kg)),
        "by_product_value": dstr(sum(by_product_cost.values(), Decimal(0)).quantize(Decimal("0.01"))),
        "allocation_pool": dstr(pool),
        "parts": out_parts,
        "total_allocated": dstr(total_allocated),
        "warnings": warnings,
    }


def allocate(inp: dict[str, Any]) -> dict[str, Any]:
    """Golden `input` sozlugunden `expected` sozlugunu uretir."""
    parts = [
        Part(
            name=raw["name"],
            kg=D(raw["kg"]),
            reference_price=D(raw["reference_price"]),
            is_waste=bool(raw.get("is_waste", False)),
            is_by_product=bool(raw.get("is_by_product", False)),
        )
        for raw in inp["parts"]
    ]
    return allocate_cutting(
        input_weight_kg=D(inp["input_weight_kg"]),
        parts=parts,
        method=inp["method"],
        unit_cost_per_kg=D(inp["unit_cost_per_kg"]) if inp.get("unit_cost_per_kg") is not None else None,
        declared_total_cost=D(inp["declared_total_cost"]) if inp.get("declared_total_cost") is not None else None,
        cost_basis=inp.get("cost_basis", "computed"),
    )
