"""Model arayuzleri. Yeni modeller (Ridge, gradient boosting) bu sinifları uygular.

Girdi/cikti Decimal'dir; float kullanan modeller sinirda Decimal(str(x)) ile cevirir.
"""
from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from decimal import Decimal
from typing import Any

import pandas as pd

PRODUCTION = "production"
CUTTING = "cutting"


@dataclass(frozen=True)
class ModelInfo:
    task: str
    name: str
    version: str
    baseline: bool


@dataclass(frozen=True)
class ProductionPrediction:
    delivered_qty: Decimal  # kg, 2 ondalik
    yield_ratio: Decimal  # kg/kg et, 4 ondalik
    notes: dict[str, Any] = field(default_factory=dict)


@dataclass(frozen=True)
class CuttingPrediction:
    parts_kg: dict[str, Decimal]  # satilabilir parcalar, 3 ondalik
    fire_kg: Decimal
    notes: dict[str, Any] = field(default_factory=dict)


class ProductionModel(ABC):
    info: ModelInfo

    @abstractmethod
    def fit(self, df: pd.DataFrame) -> "ProductionModel":
        """df: production_orders.csv sutunlari (string)."""

    @abstractmethod
    def predict(self, df: pd.DataFrame) -> list[ProductionPrediction]:
        """df: en az et_kg; opsiyonel supplier_id, producer_id, month, ... """


class CuttingModel(ABC):
    info: ModelInfo

    @abstractmethod
    def fit(self, df: pd.DataFrame) -> "CuttingModel":
        """df: lamb_cutting.csv sutunlari (string)."""

    @abstractmethod
    def predict(self, df: pd.DataFrame) -> list[CuttingPrediction]:
        """df: en az carcass_kg; opsiyonel breed, sex."""
