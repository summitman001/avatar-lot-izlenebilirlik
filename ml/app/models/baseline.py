"""Baseline modeller (ogrenme yok, gecmis ortalamalar).

production: tedarikci bazinda gecmis ortalama verim x et_kg (tedarikci yoksa genel ortalama)
cutting:    irk bazinda ortalama parca oranlari x karkas kg; satilabilir toplam = karkas x (1 - beklenen fire)
"""
from __future__ import annotations

from decimal import ROUND_HALF_UP, Decimal

import pandas as pd

from oracle.decimal_utils import qty3, ratio4
from oracle.rounding import distribute

from ..registry import register
from .base import CUTTING, PRODUCTION, CuttingModel, CuttingPrediction, ModelInfo, ProductionModel, ProductionPrediction

SALEABLE_PARTS = ["but", "kol", "sirt", "kaburga", "gerdan", "kiyma"]
GLOBAL_KEY = "ALL"
_Q2 = Decimal("0.01")


def _mean(values: list[Decimal]) -> Decimal:
    return sum(values, Decimal(0)) / Decimal(len(values))


def _opt(df: pd.DataFrame, i: int, col: str) -> str | None:
    if col not in df.columns:
        return None
    v = df[col].iloc[i]
    return None if pd.isna(v) or v == "" else str(v)


@register
class SupplierMeanYield(ProductionModel):
    info = ModelInfo(PRODUCTION, "production_baseline_supplier_mean", "1.0.0", True)

    def __init__(self) -> None:
        self.by_supplier: dict[str, Decimal] = {}
        self.global_mean: Decimal | None = None

    def fit(self, df: pd.DataFrame) -> "SupplierMeanYield":
        yields = [Decimal(d) / Decimal(e) for d, e in zip(df["delivered_qty"], df["et_kg"])]
        self.global_mean = _mean(yields)
        groups: dict[str, list[Decimal]] = {}
        for s, y in zip(df["supplier_id"], yields):
            groups.setdefault(s, []).append(y)
        self.by_supplier = {s: _mean(v) for s, v in groups.items()}
        return self

    def predict(self, df: pd.DataFrame) -> list[ProductionPrediction]:
        if self.global_mean is None:
            raise RuntimeError("model egitilmedi")
        out = []
        for i in range(len(df)):
            et = Decimal(str(df["et_kg"].iloc[i]))
            supplier = _opt(df, i, "supplier_id")
            known = supplier in self.by_supplier
            y = self.by_supplier[supplier] if known else self.global_mean
            out.append(
                ProductionPrediction(
                    delivered_qty=(et * y).quantize(_Q2, rounding=ROUND_HALF_UP),
                    yield_ratio=ratio4(y),
                    notes={"supplier_used": supplier if known else GLOBAL_KEY, "fallback": not known},
                )
            )
        return out


@register
class BreedMeanRatios(CuttingModel):
    info = ModelInfo(CUTTING, "cutting_baseline_breed_mean", "1.0.0", True)

    def __init__(self) -> None:
        self.by_breed: dict[str, dict[str, Decimal]] = {}
        self.global_ratios: dict[str, Decimal] | None = None

    @staticmethod
    def _ratios(df: pd.DataFrame) -> dict[str, Decimal]:
        carcass = [Decimal(c) for c in df["carcass_kg"]]
        return {
            p: _mean([Decimal(k) / c for k, c in zip(df[f"{p}_kg"], carcass)]) for p in SALEABLE_PARTS + ["fire"]
        }

    def fit(self, df: pd.DataFrame) -> "BreedMeanRatios":
        self.global_ratios = self._ratios(df)
        self.by_breed = {b: self._ratios(g) for b, g in df.groupby("breed")}
        return self

    def predict(self, df: pd.DataFrame) -> list[CuttingPrediction]:
        if self.global_ratios is None:
            raise RuntimeError("model egitilmedi")
        out = []
        for i in range(len(df)):
            carcass = Decimal(str(df["carcass_kg"].iloc[i]))
            breed = _opt(df, i, "breed")
            known = breed in self.by_breed
            r = self.by_breed[breed] if known else self.global_ratios
            fire_ratio = r["fire"]
            saleable_sum = sum((r[p] for p in SALEABLE_PARTS), Decimal(0))
            # satilabilir parcalar toplami = karkas x (1 - beklenen fire), 3 ondalik, kaynaga birebir
            saleable_total = qty3(carcass * (1 - fire_ratio))
            kgs = distribute(saleable_total, [r[p] / saleable_sum for p in SALEABLE_PARTS], 3)
            out.append(
                CuttingPrediction(
                    parts_kg=dict(zip(SALEABLE_PARTS, kgs)),
                    fire_kg=qty3(carcass) - saleable_total,
                    notes={"breed_used": breed if known else GLOBAL_KEY, "fallback": not known,
                           "expected_fire_ratio": ratio4(fire_ratio)},
                )
            )
        return out
