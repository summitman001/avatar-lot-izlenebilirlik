"""Gorev 2: kuzu parcalama ve maliyet dagitimi.

Kurallar:
- agirlik dengesi: balance_diff_kg = giris_kg - sum(tum parcalar, fire ve yan urun dahil)
  |diff| > 0.05 kg ise WEIGHT_IMBALANCE uyarisi ve closable=false
- alis toplami uyusmazligi (hesaplanan != beyan edilen): PURCHASE_TOTAL_MISMATCH. Bu durumda
  cost_basis ve resolution_note ZORUNLUDUR; verilmemisse MISMATCH_RESOLUTION_MISSING ve closable=false
  (dagitim gecici olarak hesaplanan tutar uzerinden gosterilir). Diger esas da `alternative_basis`
  altinda hesaplanip raporlanir.
- fire (is_waste): maliyet tasimaz (0.00), maliyeti satilabilir parcalara yuklenir
- yan urun (is_by_product): kendi satis degeri kadar maliyet tasir (kg * referans fiyat, 2 ondalik),
  bu tutar toplam maliyetten dusulur; kalan havuz ana parcalara dagitilir (net realizable value)
- quantity yontemi: ana parcalara kg oranina, value yontemi: kg * referans fiyat oranina
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
MISMATCH_RESOLUTION_MISSING = "MISMATCH_RESOLUTION_MISSING"
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


def _distribute_costs(total_cost: Decimal, parts: list[Part], method: str) -> tuple[list[Decimal], Decimal, Decimal]:
    """(parca maliyetleri, ana parca havuzu, yan urun degeri toplami)"""
    # Yan urun: kendi satis degeri kadar maliyet tasir
    by_product_cost = {i: money(p.kg * p.reference_price) for i, p in enumerate(parts) if p.role == "by_product"}
    by_product_total = sum(by_product_cost.values(), Decimal(0))
    pool = total_cost - by_product_total
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
    costs = []
    for i, p in enumerate(parts):
        if p.role == "main":
            costs.append(main_alloc[i])
        elif p.role == "by_product":
            costs.append(by_product_cost[i])
        else:
            costs.append(Decimal("0.00"))
    return costs, pool, by_product_total


def allocate_cutting(
    input_weight_kg: Decimal,
    parts: list[Part],
    method: str,
    unit_cost_per_kg: Decimal | None = None,
    declared_total_cost: Decimal | None = None,
    cost_basis: str | None = None,
    resolution_note: str | None = None,
) -> dict[str, Any]:
    if method not in METHODS:
        raise ValueError(f"gecersiz yontem: {method}")
    if cost_basis is not None and cost_basis not in COST_BASES:
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
    declared_total = money(declared_total_cost) if declared_total_cost is not None else None
    totals = {"computed": computed_total, "declared": declared_total}
    if computed_total is None and declared_total is None:
        raise ValueError("unit_cost_per_kg veya declared_total_cost gerekli")

    mismatch = computed_total is not None and declared_total is not None and computed_total != declared_total
    # Esas verilmemisse gecici olarak hesaplanan tutar (yoksa beyan) kullanilir
    basis_used = cost_basis or ("computed" if computed_total is not None else "declared")
    if totals[basis_used] is None:
        raise ValueError(f"{basis_used} esasi icin gerekli girdi eksik")
    total_cost = totals[basis_used]
    note = (resolution_note or "").strip() or None
    unresolved = mismatch and (cost_basis is None or note is None)

    warnings: list[str] = []
    if mismatch:
        warnings.append(PURCHASE_TOTAL_MISMATCH)
    if unresolved:
        warnings.append(MISMATCH_RESOLUTION_MISSING)

    parts_weight = sum((p.kg for p in parts), Decimal(0))
    balance_diff = input_weight_kg - parts_weight
    balanced = abs(balance_diff) <= WEIGHT_TOLERANCE_KG
    if not balanced:
        warnings.append(WEIGHT_IMBALANCE)
    closable = balanced and not unresolved

    costs, pool, by_product_total = _distribute_costs(total_cost, parts, method)
    total_allocated = sum(costs, Decimal(0))
    assert total_allocated == total_cost, "dagitim toplami maliyete esit olmali"

    out_parts = [
        {
            "name": p.name,
            "role": p.role,
            "kg": dstr(qty3(p.kg)),
            "yield_pct": dstr(pct2(p.kg / input_weight_kg * 100)),
            "allocated_cost": dstr(cost),
            "unit_cost": dstr(unit4(cost / p.kg)) if p.kg > 0 else "0.0000",
        }
        for p, cost in zip(parts, costs)
    ]

    alternative = None
    if mismatch:
        other = "declared" if basis_used == "computed" else "computed"
        alt_costs, _, _ = _distribute_costs(totals[other], parts, method)
        alternative = {
            "cost_basis": other,
            "total_input_cost": dstr(totals[other]),
            "parts": [{"name": p.name, "allocated_cost": dstr(c)} for p, c in zip(parts, alt_costs)],
            "total_allocated": dstr(sum(alt_costs, Decimal(0))),
        }

    sellable_kg = sum((p.kg for p in parts if p.role == "main"), Decimal(0))
    return {
        "method": method,
        "cost_basis": cost_basis,
        "cost_basis_used": basis_used,
        "resolution_note": note,
        "input_weight_kg": dstr(qty3(input_weight_kg)),
        "computed_total_cost": None if computed_total is None else dstr(computed_total),
        "declared_total_cost": None if declared_total is None else dstr(declared_total),
        "total_input_cost": dstr(total_cost),
        "parts_weight_kg": dstr(qty3(parts_weight)),
        "balance_diff_kg": dstr(qty3(balance_diff)),
        "closable": closable,
        "sellable_kg": dstr(qty3(sellable_kg)),
        "by_product_value": dstr(by_product_total.quantize(Decimal("0.01"))),
        "allocation_pool": dstr(pool),
        "parts": out_parts,
        "total_allocated": dstr(total_allocated),
        "alternative_basis": alternative,
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
        cost_basis=inp.get("cost_basis"),
        resolution_note=inp.get("resolution_note"),
    )
