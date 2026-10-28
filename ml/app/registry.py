"""Model kayit defteri: yeni modeller `register` ile eklenir, evaluate ve servis otomatik gorur."""
from __future__ import annotations

import os
from dataclasses import dataclass
from typing import Callable

from .models.base import CUTTING, PRODUCTION, ModelInfo


@dataclass(frozen=True)
class ModelSpec:
    info: ModelInfo
    factory: Callable[[], object]


_REGISTRY: dict[str, dict[str, ModelSpec]] = {PRODUCTION: {}, CUTTING: {}}

DEFAULT_ACTIVE = {
    # CV karsilastirmasina gore (bkz. reports/model_comparison.json, docs/ml-report.md):
    # production: hicbir ML modeli baseline'i gecemedi (ridge/gbm istatistiksel olarak
    # anlamli degil, p>0.2) -> baseline aktif kalir, DURUSTLUK KURALI geregi.
    PRODUCTION: "production_baseline_supplier_mean",
    # cutting: cutting_ridge_multi baseline'i anlamli sekilde gecti (p=0.0096, %16.4
    # iyilesme) ve gbm_multi'den daha basit/yorumlanabilir -> aktif secildi.
    CUTTING: "cutting_ridge_multi",
}


def register(cls):
    """Sinif dekoratoru: `cls.info` ile kaydeder."""
    info: ModelInfo = cls.info
    _REGISTRY[info.task][info.name] = ModelSpec(info=info, factory=cls)
    return cls


def load_builtin_models() -> None:
    from .models import baseline, cutting_ml, production_ml  # noqa: F401  (yan etki: modeller register olur)


def specs(task: str, baseline_only: bool = False) -> list[ModelSpec]:
    load_builtin_models()
    out = list(_REGISTRY[task].values())
    return [s for s in out if s.info.baseline] if baseline_only else out


def get(task: str, name: str) -> ModelSpec:
    load_builtin_models()
    try:
        return _REGISTRY[task][name]
    except KeyError:
        raise KeyError(f"model kayitli degil: {task}/{name}") from None


def active_name(task: str) -> str:
    """Yarin: ACTIVE_PRODUCTION_MODEL / ACTIVE_CUTTING_MODEL ortam degiskeniyle model degistirilir."""
    return os.environ.get(f"ACTIVE_{task.upper()}_MODEL", DEFAULT_ACTIVE[task])
