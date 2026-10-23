"""Servis durumu: acilista veriyi hazirlar, tum kayitli modelleri degerlendirir ve tum veriyle egitir."""
from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import pandas as pd

from train import evaluate

from . import datasets, registry
from .models.base import CUTTING, PRODUCTION, ModelInfo


@dataclass
class LoadedModel:
    info: ModelInfo
    model: Any
    trained_at: str
    n_train: int
    metrics: dict[str, Any]
    active: bool


class ModelService:
    def __init__(self) -> None:
        self.models: dict[str, dict[str, LoadedModel]] = {PRODUCTION: {}, CUTTING: {}}
        self.ready = False

    def bootstrap(self, data_dir: Path | None = None) -> "ModelService":
        splits = evaluate.build_splits(data_dir)
        full = {PRODUCTION: datasets.load_production(data_dir), CUTTING: datasets.load_cutting(data_dir)}
        active = {task: registry.active_name(task) for task in (PRODUCTION, CUTTING)}
        for task, (train, test) in splits.items():
            report = evaluate.evaluate_task(task, train, test, baseline_only=False)["models"]
            for spec in registry.specs(task):
                model = spec.factory().fit(full[task])  # sunulan model tum veriyle egitilir
                self.models[task][spec.info.name] = LoadedModel(
                    info=spec.info,
                    model=model,
                    trained_at=datetime.now(timezone.utc).isoformat(timespec="seconds"),
                    n_train=len(full[task]),
                    metrics={"holdout": report[spec.info.name]["metrics"], "holdout_n_train": len(train), "holdout_n_test": len(test)},
                    active=spec.info.name == active[task],
                )
        for task, name in active.items():
            if name not in self.models[task]:
                raise RuntimeError(f"aktif model kayitli degil: {task}/{name}")
        self.ready = True
        return self

    def active(self, task: str) -> LoadedModel:
        return next(m for m in self.models[task].values() if m.active)

    def describe(self) -> list[dict[str, Any]]:
        return [
            {
                "task": task,
                "model_name": m.info.name,
                "model_version": m.info.version,
                "baseline": m.info.baseline,
                "active": m.active,
                "trained_at": m.trained_at,
                "n_train": m.n_train,
                "metrics": m.metrics,
            }
            for task in (PRODUCTION, CUTTING)
            for m in self.models[task].values()
        ]


def frame(**cols: Any) -> pd.DataFrame:
    """Tek satirlik, string sutunlu girdi (None olanlar atlanir)."""
    return pd.DataFrame([{k: str(v) for k, v in cols.items() if v is not None}], dtype=str)
