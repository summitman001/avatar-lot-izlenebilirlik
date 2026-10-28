"""Parcalama verimi icin ogrenen modeller: parca basina ayri Ridge / HistGradientBoosting.

Her parca (but, kol, sirt, kaburga, gerdan, kiyma, fire) icin ayri bir regressor orani
(parca_kg / karkas_kg) tahmin eder. Ham tahminler (`predict_raw`) karkasa toplanmak
ZORUNDA DEGILDIR; resmi `predict()` ciktisi bunlari `oracle.rounding.distribute` ile
karkasa (fiziksel kisit) normalize eder -- tipki baseline'in irk ortalamalarini
normalize etmesi gibi. Normalizasyonun metriklere etkisi `train/evaluate.py` icinde
raw vs normalized MAE olarak raporlanir.
"""
from __future__ import annotations

from decimal import Decimal

import pandas as pd
from sklearn.ensemble import HistGradientBoostingRegressor
from sklearn.linear_model import Ridge
from sklearn.pipeline import Pipeline
from sklearn.preprocessing import StandardScaler

from oracle.decimal_utils import qty3, ratio4
from oracle.rounding import distribute

from ..registry import register
from .baseline import GLOBAL_KEY, SALEABLE_PARTS, _opt
from .base import CUTTING, CuttingModel, CuttingPrediction, ModelInfo
from .features import CUTTING_BREEDS, CuttingFeatureBuilder

ALL_PARTS = SALEABLE_PARTS + ["fire"]
_MIN_WEIGHT = Decimal("0.0001")


class _LearnedCuttingModel(CuttingModel):
    """Ridge/GBM icin ortak fit/predict iskeleti; alt sinif yalnizca estimator kurar."""

    def __init__(self) -> None:
        self.features = CuttingFeatureBuilder()
        self.estimators: dict[str, object] = {}

    def _build_estimator(self, n_train: int):
        raise NotImplementedError

    def fit(self, df: pd.DataFrame) -> "_LearnedCuttingModel":
        X = self.features.transform(df)
        carcass = pd.to_numeric(df["carcass_kg"], errors="coerce").to_numpy(dtype=float)
        for part in ALL_PARTS:
            y = pd.to_numeric(df[f"{part}_kg"], errors="coerce").to_numpy(dtype=float) / carcass
            est = self._build_estimator(len(df))
            est.fit(X, y)
            self.estimators[part] = est
        return self

    def _raw_ratio_predictions(self, df: pd.DataFrame) -> dict[str, list[float]]:
        if not self.estimators:
            raise RuntimeError("model egitilmedi")
        X = self.features.transform(df)
        return {part: list(self.estimators[part].predict(X)) for part in ALL_PARTS}

    def predict_raw(self, df: pd.DataFrame) -> list[dict[str, Decimal]]:
        """Normalize edilmemis (karkasa toplanmasi garanti olmayan) parca kg tahminleri."""
        ratios = self._raw_ratio_predictions(df)
        carcass = pd.to_numeric(df["carcass_kg"], errors="coerce").to_numpy(dtype=float)
        out = []
        for i in range(len(df)):
            c = Decimal(str(carcass[i]))
            out.append({part: qty3(c * Decimal(str(max(ratios[part][i], 0.0)))) for part in ALL_PARTS})
        return out

    def predict(self, df: pd.DataFrame) -> list[CuttingPrediction]:
        ratios = self._raw_ratio_predictions(df)
        out = []
        for i in range(len(df)):
            carcass = Decimal(str(df["carcass_kg"].iloc[i]))
            weights = {part: max(Decimal(str(ratios[part][i])), _MIN_WEIGHT) for part in ALL_PARTS}
            fire_ratio = min(max(Decimal(str(ratios["fire"][i])), Decimal("0")), Decimal("0.9"))
            saleable_total = qty3(carcass * (1 - fire_ratio))
            saleable_weights = [weights[p] for p in SALEABLE_PARTS]
            kgs = distribute(saleable_total, saleable_weights, 3)
            breed = _opt(df, i, "breed")
            known = breed in CUTTING_BREEDS
            out.append(
                CuttingPrediction(
                    parts_kg=dict(zip(SALEABLE_PARTS, kgs)),
                    fire_kg=qty3(carcass) - saleable_total,
                    notes={
                        "breed_used": breed if known else GLOBAL_KEY,
                        "fallback": not known,
                        "expected_fire_ratio": ratio4(fire_ratio),
                    },
                )
            )
        return out


@register
class RidgeMultiCutting(_LearnedCuttingModel):
    info = ModelInfo(CUTTING, "cutting_ridge_multi", "1.0.0", False)

    def _build_estimator(self, n_train: int):
        return Pipeline([("scaler", StandardScaler()), ("ridge", Ridge(alpha=1.0))])


@register
class GBMMultiCutting(_LearnedCuttingModel):
    info = ModelInfo(CUTTING, "cutting_gbm_multi", "1.0.0", False)

    def _build_estimator(self, n_train: int):
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
