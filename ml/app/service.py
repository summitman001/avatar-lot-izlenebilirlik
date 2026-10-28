"""Servis durumu: acilista veriyi hazirlar, modelleri yukler (veya egitir) ve /models icin
onceden hesaplanmis CV metriklerini (reports/model_comparison.json) okur.

Baseline modeller her acilista TUM veriyle anlik egitilir (maliyetsiz). Ogrenen modeller
(Ridge/GBM) ONCEDEN egitilip diske kaydedilmis olmalidir (`python -m train.train_models`,
bkz. docs/HANDOFF.md); dosya yoksa (gelistirme sirasinda veya yeni eklenen bir model icin)
geriye donuk uyumluluk olarak o anda tum veriyle egitilir.
"""
from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import pandas as pd

from . import datasets, model_store, registry
from .models.base import CUTTING, PRODUCTION, ModelInfo

COMPARISON_PATH = datasets.ML_ROOT / "reports" / "model_comparison.json"


@dataclass
class LoadedModel:
    info: ModelInfo
    model: Any
    trained_at: str
    n_train: int
    from_disk: bool
    metrics: dict[str, Any]
    active: bool


def _load_comparison() -> dict[str, Any]:
    if not COMPARISON_PATH.exists():
        return {}
    return json.loads(COMPARISON_PATH.read_text(encoding="utf-8"))


class ModelService:
    def __init__(self) -> None:
        self.models: dict[str, dict[str, LoadedModel]] = {PRODUCTION: {}, CUTTING: {}}
        self.ready = False

    def bootstrap(self, data_dir: Path | None = None) -> "ModelService":
        full = {PRODUCTION: datasets.load_production(data_dir), CUTTING: datasets.load_cutting(data_dir)}
        active = {task: registry.active_name(task) for task in (PRODUCTION, CUTTING)}
        comparison = _load_comparison()
        now = datetime.now(timezone.utc).isoformat(timespec="seconds")
        for task in (PRODUCTION, CUTTING):
            task_metrics = comparison.get(task, {}).get("models", {})
            for spec in registry.specs(task):
                from_disk = False
                if spec.info.baseline:
                    model = spec.factory().fit(full[task])
                else:
                    loaded = model_store.load(task, spec.info.name)
                    if loaded is not None:
                        model, from_disk = loaded, True
                    else:
                        model = spec.factory().fit(full[task])
                self.models[task][spec.info.name] = LoadedModel(
                    info=spec.info,
                    model=model,
                    trained_at=now,
                    n_train=len(full[task]),
                    from_disk=from_disk,
                    metrics={"cv": task_metrics.get(spec.info.name)},
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
                "loaded_from_disk": m.from_disk,
                "metrics": m.metrics,
            }
            for task in (PRODUCTION, CUTTING)
            for m in self.models[task].values()
        ]


def frame(**cols: Any) -> pd.DataFrame:
    """Tek satirlik, string sutunlu girdi (None olanlar atlanir)."""
    return pd.DataFrame([{k: str(v) for k, v in cols.items() if v is not None}], dtype=str)
