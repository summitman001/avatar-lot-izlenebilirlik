"""Egitilmis model dosyalarinin (joblib) diskte saklanmasi.

Baseline modeller egitilmez (CSV'den anlik hesaplanir, maliyetsiz); yalnizca
ogrenen modeller (Ridge/GBM) burada saklanir. Boylece Docker imaji/servis
baslangicinda yeniden egitim GEREKMEZ -- `train.train_models` ile ONCEDEN
uretilip commitlenen dosyalar yuklenir. Dosya yoksa (ornegin yeni bir model
eklendiginde egitim scripti henuz calistirilmadiysa) servis geriye donuk
uyumluluk icin modeli o anda egitir (bkz. app/service.py).
"""
from __future__ import annotations

from pathlib import Path
from typing import Any

import joblib

from . import datasets

MODELS_DIR = datasets.ML_ROOT / "models"


def path_for(task: str, name: str) -> Path:
    return MODELS_DIR / f"{task}__{name}.joblib"


def save(task: str, name: str, model: Any) -> Path:
    MODELS_DIR.mkdir(parents=True, exist_ok=True)
    path = path_for(task, name)
    joblib.dump(model, path)
    return path


def load(task: str, name: str) -> Any | None:
    path = path_for(task, name)
    if not path.exists():
        return None
    return joblib.load(path)
