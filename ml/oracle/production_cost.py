"""Gorev 1: dis hizmetle uretim maliyeti (EKP).

Formuller (tum ara degerler tam hassasiyet, sadece cikti yuvarlanir):
    material_cost = sum(sevk_miktari * birim_maliyet)
    service_cost  = faturalanan_adet * hizmet_birim_fiyati
    total_cost    = material_cost + service_cost
    ekp_unit      = total_cost / teslim_alinan_adet        (4 ondalik)
    ekp_display   = total_cost / teslim_alinan_adet        (2 ondalik, tam degerden)
Degerleme her zaman ekp_unit (4 ondalik) veya total_cost uzerinden yapilir;
ekp_display ile carpmak yanlistir (bkz. rounding_proof).
"""
from __future__ import annotations

from dataclasses import dataclass
from decimal import Decimal
from typing import Any

from .decimal_utils import D, dstr, money, pct2, qty3, ratio4, unit4

CATEGORIES = ("meat", "spice", "casing", "other")
INVOICED_QTY_MISMATCH = "INVOICED_QTY_MISMATCH"


@dataclass(frozen=True)
class Lot:
    name: str
    category: str
    quantity: Decimal  # kg
    unit_cost: Decimal  # EUR/kg


@dataclass(frozen=True)
class ProductionResult:
    material_cost: Decimal
    service_cost: Decimal
    total_cost: Decimal
    ekp_unit: Decimal
    ekp_display: Decimal
    delivered_qty: Decimal
    invoiced_qty: Decimal
    input_kg_total: Decimal
    yield_input_pct: Decimal | None
    yield_meat_ratio: Decimal | None
    lot_lines: list[tuple[str, Decimal]]
    sale_price: Decimal | None
    revenue: Decimal | None
    gross_profit_unit: Decimal | None
    gross_profit_total: Decimal | None
    margin_pct: Decimal | None
    warnings: list[str]

    def to_json(self) -> dict[str, Any]:
        def s(x: Decimal | None) -> str | None:
            return None if x is None else dstr(x)

        return {
            "lot_lines": [{"name": n, "amount": dstr(a)} for n, a in self.lot_lines],
            "material_cost": s(self.material_cost),
            "service_cost": s(self.service_cost),
            "total_cost": s(self.total_cost),
            "delivered_qty": s(self.delivered_qty),
            "invoiced_qty": s(self.invoiced_qty),
            "ekp_unit": s(self.ekp_unit),
            "ekp_display": s(self.ekp_display),
            "input_kg_total": s(self.input_kg_total),
            "yield_input_pct": s(self.yield_input_pct),
            "yield_meat_ratio": s(self.yield_meat_ratio),
            "sale_price": s(self.sale_price),
            "revenue": s(self.revenue),
            "gross_profit_unit": s(self.gross_profit_unit),
            "gross_profit_total": s(self.gross_profit_total),
            "margin_pct": s(self.margin_pct),
            "warnings": list(self.warnings),
        }


def calculate_production_cost(
    lots: list[Lot],
    delivered_qty: Decimal,
    service_unit_price: Decimal,
    invoiced_qty: Decimal | None = None,
    sale_price: Decimal | None = None,
) -> ProductionResult:
    if delivered_qty <= 0:
        raise ValueError("teslim alinan adet sifirdan buyuk olmali")
    if service_unit_price < 0:
        raise ValueError("hizmet birim fiyati negatif olamaz")
    for lot in lots:
        if lot.category not in CATEGORIES:
            raise ValueError(f"gecersiz kategori: {lot.category}")
        if lot.quantity < 0 or lot.unit_cost < 0:
            raise ValueError("miktar ve birim maliyet negatif olamaz")
    if invoiced_qty is None:
        invoiced_qty = delivered_qty
    if invoiced_qty < 0:
        raise ValueError("faturalanan adet negatif olamaz")

    # Tam hassasiyetli ara degerler (yuvarlama yok)
    lot_exact = [(lot.name, lot.quantity * lot.unit_cost) for lot in lots]
    material_exact = sum((a for _, a in lot_exact), Decimal(0))
    service_exact = invoiced_qty * service_unit_price
    total_exact = material_exact + service_exact
    ekp_exact = total_exact / delivered_qty

    warnings: list[str] = []
    if invoiced_qty != delivered_qty:
        warnings.append(INVOICED_QTY_MISMATCH)

    input_kg = sum((lot.quantity for lot in lots), Decimal(0))
    meat_kg = sum((lot.quantity for lot in lots if lot.category == "meat"), Decimal(0))
    yield_input = pct2(delivered_qty / input_kg * 100) if input_kg > 0 else None
    yield_meat = ratio4(delivered_qty / meat_kg) if meat_kg > 0 else None

    revenue = profit_unit = profit_total = margin = None
    if sale_price is not None:
        if sale_price < 0:
            raise ValueError("satis fiyati negatif olamaz")
        revenue_exact = sale_price * delivered_qty
        revenue = money(revenue_exact)
        profit_unit = unit4(sale_price - ekp_exact)
        profit_total = money(revenue_exact - total_exact)
        margin = pct2((revenue_exact - total_exact) / revenue_exact * 100) if revenue_exact > 0 else None

    return ProductionResult(
        material_cost=money(material_exact),
        service_cost=money(service_exact),
        total_cost=money(total_exact),
        ekp_unit=unit4(ekp_exact),
        ekp_display=money(ekp_exact),
        delivered_qty=delivered_qty,
        invoiced_qty=invoiced_qty,
        input_kg_total=qty3(input_kg),
        yield_input_pct=yield_input,
        yield_meat_ratio=yield_meat,
        lot_lines=[(n, money(a)) for n, a in lot_exact],
        sale_price=sale_price,
        revenue=revenue,
        gross_profit_unit=profit_unit,
        gross_profit_total=profit_total,
        margin_pct=margin,
        warnings=warnings,
    )


def rounding_proof(result: ProductionResult) -> dict[str, str]:
    """ekp_display * adet ile total_cost'un neden esit olmadigini gosterir."""
    naive = money(result.ekp_display * result.delivered_qty)
    return {
        "ekp_display_times_qty": dstr(naive),
        "total_cost": dstr(result.total_cost),
        "difference": dstr(naive - result.total_cost),
    }


def calculate(inp: dict[str, Any]) -> dict[str, Any]:
    """Golden `input` sozlugunden `expected` sozlugunu uretir."""
    lots = [
        Lot(
            name=raw["name"],
            category=raw["category"],
            quantity=D(raw["quantity"]),
            unit_cost=D(raw["unit_cost"]),
        )
        for raw in inp["lots"]
    ]
    result = calculate_production_cost(
        lots=lots,
        delivered_qty=D(inp["delivered_qty"]),
        service_unit_price=D(inp["service_unit_price"]),
        invoiced_qty=D(inp["invoiced_qty"]) if inp.get("invoiced_qty") is not None else None,
        sale_price=D(inp["sale_price"]) if inp.get("sale_price") is not None else None,
    )
    return result.to_json()
