"""Ridge/GBM modelleri icin ortak ozellik muhendisligi.

Girdi df'leri string sutunlu (CSV veya API `frame()` ciktisi). Eksik sutunlar (API
istegi minimal oldugunda, ornegin lot_age_days/spice_kg/casing_kg verilmediginde)
egitim ortalamalariyla doldurulur (`fit`te ogrenilir) -- boylece backend sozlesmesi
(yalnizca et_kg/supplier_id/producer_id/unit_weight_kg/order_date gonderir) degismeden
calisir; eksik ozellikler modelin ogrendigi ortalamaya duser (bkz. docs/ml-report.md
SINIRLILIKLAR).
"""
from __future__ import annotations

import numpy as np
import pandas as pd

PRODUCTION_SUPPLIERS = ["TED-A", "TED-B", "TED-C"]
PRODUCTION_PRODUCERS = ["URT-1", "URT-2"]
CUTTING_BREEDS = ["Morkaraman", "Akkaraman", "Merinos"]
CUTTING_SEXES = ["M", "F"]
CUTTING_REF_CARCASS = 21.0


def _col_float(df: pd.DataFrame, name: str, default: float) -> np.ndarray:
    if name not in df.columns:
        return np.full(len(df), default)
    s = pd.to_numeric(df[name], errors="coerce")
    return s.fillna(default).to_numpy(dtype=float)


def _one_hot(df: pd.DataFrame, col: str, categories: list[str]) -> np.ndarray:
    if col not in df.columns:
        return np.zeros((len(df), len(categories)))
    values = df[col].astype(object).where(df[col].notna(), None)
    return np.array([[1.0 if v == c else 0.0 for c in categories] for v in values])


class ProductionFeatureBuilder:
    """et_kg, tedarikci/dis uretici one-hot, ay (sin/cos), lot yasi, baharat/bagirsak orani."""

    FEATURE_NAMES = (
        ["et_kg", "month_sin", "month_cos", "lot_age_days", "spice_ratio", "casing_ratio"]
        + [f"supplier_{s}" for s in PRODUCTION_SUPPLIERS]
        + [f"producer_{p}" for p in PRODUCTION_PRODUCERS]
    )

    def __init__(self) -> None:
        self.mean_month: float = 6.0
        self.mean_lot_age: float = 5.0
        self.mean_spice_ratio: float = 0.02
        self.mean_casing_ratio: float = 0.05

    def learn_defaults(self, df: pd.DataFrame) -> None:
        if "month" in df.columns:
            self.mean_month = float(pd.to_numeric(df["month"], errors="coerce").mean())
        if "lot_age_days" in df.columns:
            self.mean_lot_age = float(pd.to_numeric(df["lot_age_days"], errors="coerce").mean())
        if {"spice_kg", "et_kg"} <= set(df.columns):
            self.mean_spice_ratio = float(
                (pd.to_numeric(df["spice_kg"], errors="coerce") / pd.to_numeric(df["et_kg"], errors="coerce")).mean()
            )
        if {"casing_kg", "et_kg"} <= set(df.columns):
            self.mean_casing_ratio = float(
                (pd.to_numeric(df["casing_kg"], errors="coerce") / pd.to_numeric(df["et_kg"], errors="coerce")).mean()
            )

    def transform(self, df: pd.DataFrame) -> np.ndarray:
        et = _col_float(df, "et_kg", 100.0)
        month = _col_float(df, "month", self.mean_month)
        age = _col_float(df, "lot_age_days", self.mean_lot_age)
        if "spice_kg" in df.columns:
            spice_ratio = _col_float(df, "spice_kg", 0.0) / np.where(et == 0, 1, et)
        else:
            spice_ratio = np.full(len(df), self.mean_spice_ratio)
        if "casing_kg" in df.columns:
            casing_ratio = _col_float(df, "casing_kg", 0.0) / np.where(et == 0, 1, et)
        else:
            casing_ratio = np.full(len(df), self.mean_casing_ratio)
        month_sin = np.sin(2 * np.pi * month / 12)
        month_cos = np.cos(2 * np.pi * month / 12)
        supplier_oh = _one_hot(df, "supplier_id", PRODUCTION_SUPPLIERS)
        producer_oh = _one_hot(df, "producer_id", PRODUCTION_PRODUCERS)
        return np.column_stack(
            [et, month_sin, month_cos, age, spice_ratio, casing_ratio, supplier_oh, producer_oh]
        )


class CuttingFeatureBuilder:
    """karkas kg, irk/cinsiyet one-hot, irk x agirlik etkilesimi."""

    FEATURE_NAMES = (
        ["carcass_kg"]
        + [f"breed_{b}" for b in CUTTING_BREEDS]
        + [f"sex_{s}" for s in CUTTING_SEXES]
        + [f"interaction_{b}" for b in CUTTING_BREEDS]
    )

    def transform(self, df: pd.DataFrame) -> np.ndarray:
        carcass = _col_float(df, "carcass_kg", 21.0)
        breed_oh = _one_hot(df, "breed", CUTTING_BREEDS)
        sex_oh = _one_hot(df, "sex", CUTTING_SEXES)
        interaction = breed_oh * (carcass - CUTTING_REF_CARCASS)[:, None]
        return np.column_stack([carcass, breed_oh, sex_oh, interaction])
