"""Uretim verimi icin ogrenen modeller: Ridge ve HistGradientBoosting.

Ikisi de `yield_ratio`i (cikti kg / et kg) tahmin eder, delivered_qty = et_kg x yield / unit_weight_kg
seklinde donusturulur (baseline ile aynı donusum, karsilastirilabilirlik icin).
"""
from __future__ import annotations

from decimal import ROUND_HALF_UP, Decimal

import pandas as pd
from sklearn.ensemble import HistGradientBoostingRegressor
from sklearn.linear_model import Ridge
from sklearn.model_selection import GridSearchCV
from sklearn.pipeline import Pipeline
from sklearn.preprocessing import StandardScaler

from oracle.decimal_utils import ratio4

from ..registry import register
from .baseline import GLOBAL_KEY, _opt
from .base import PRODUCTION, ModelInfo, ProductionModel, ProductionPrediction
from .features import PRODUCTION_SUPPLIERS, ProductionFeatureBuilder

_Q2 = Decimal("0.01")


def _notes(df: pd.DataFrame, i: int) -> dict[str, object]:
    supplier = _opt(df, i, "supplier_id")
    known = supplier in PRODUCTION_SUPPLIERS
    return {"supplier_used": supplier if known else GLOBAL_KEY, "fallback": not known}


class _LearnedYieldModel(ProductionModel):
    """Ridge/GBM icin ortak fit/predict iskeleti; alt sinif yalnizca estimator kurar."""

    def __init__(self) -> None:
        self.features = ProductionFeatureBuilder()
        self.estimator = None

    def _build_estimator(self, n_train: int):
        raise NotImplementedError

    def fit(self, df: pd.DataFrame) -> "_LearnedYieldModel":
        self.features.learn_defaults(df)
        X = self.features.transform(df)
        y = pd.to_numeric(df["yield_ratio"], errors="coerce").to_numpy(dtype=float)
        self.estimator = self._build_estimator(len(df))
        self.estimator.fit(X, y)
        return self

    def predict(self, df: pd.DataFrame) -> list[ProductionPrediction]:
        if self.estimator is None:
            raise RuntimeError("model egitilmedi")
        X = self.features.transform(df)
        preds = self.estimator.predict(X)
        out = []
        for i, yhat in enumerate(preds):
            et = Decimal(str(df["et_kg"].iloc[i]))
            unit_weight = Decimal(_opt(df, i, "unit_weight_kg") or "1")
            y = Decimal(str(float(yhat)))
            delivered = (et * y / unit_weight).quantize(_Q2, rounding=ROUND_HALF_UP)
            out.append(ProductionPrediction(delivered_qty=delivered, yield_ratio=ratio4(y), notes=_notes(df, i)))
        return out


@register
class RidgeYield(_LearnedYieldModel):
    info = ModelInfo(PRODUCTION, "production_ridge", "1.0.0", False)

    def _build_estimator(self, n_train: int):
        pipe = Pipeline([("scaler", StandardScaler()), ("ridge", Ridge())])
        # ic CV (dis CV'ye sizmaz): her disardaki katmanda yalniz o katmanin egitim verisiyle secilir
        inner_cv = min(5, max(2, n_train // 8))
        grid = GridSearchCV(
            pipe,
            param_grid={"ridge__alpha": [0.01, 0.1, 1.0, 10.0, 50.0, 100.0]},
            cv=inner_cv,
            scoring="neg_mean_absolute_error",
        )
        return grid


@register
class GBMYield(_LearnedYieldModel):
    info = ModelInfo(PRODUCTION, "production_gbm", "1.0.0", False)

    def _build_estimator(self, n_train: int):
        # az veri icin muhafazakar parametreler; cok kucuk katmanlarda early stopping
        # kendi ic validasyon bolmesi icin yetersiz veri bulur, bu yuzden kapatilir.
        early_stopping = n_train >= 20
        return HistGradientBoostingRegressor(
            max_depth=3,
            learning_rate=0.05,
            max_iter=200,
            min_samples_leaf=max(2, min(15, n_train // 4)),
            early_stopping=early_stopping,
            validation_fraction=0.2,
            n_iter_no_change=10,
            random_state=0,
        )
