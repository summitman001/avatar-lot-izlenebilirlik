"""Veri yukleme ve bolme: servis ve egitim kodu ayni fonksiyonlari kullanir."""
from __future__ import annotations

import os
from pathlib import Path

import numpy as np
import pandas as pd

ML_ROOT = Path(__file__).resolve().parents[1]
DATA_DIR = Path(os.environ.get("ML_DATA_DIR", ML_ROOT / "data" / "out"))
PRODUCTION_CSV = "production_orders.csv"
CUTTING_CSV = "cutting_records.csv"

TEST_FRACTION = 0.2
CUTTING_SPLIT_SEED = 42
CUTTING_PARTS = ["but", "kol", "sirt", "kaburga", "gerdan", "kiyma", "fire"]


def ensure_data(data_dir: Path | None = None) -> Path:
    """CSV'ler yoksa sabit seed ile uretir (sentetik veri, bkz. docs/ml-data.md)."""
    data_dir = data_dir or DATA_DIR
    if not (data_dir / PRODUCTION_CSV).exists():
        from data import generate_production

        generate_production.main(["--out", str(data_dir)])
    if not (data_dir / CUTTING_CSV).exists():
        from data import generate_cutting

        generate_cutting.main(["--out", str(data_dir)])
    return data_dir


def load_production(data_dir: Path | None = None) -> pd.DataFrame:
    # Tum sutunlar string: Decimal'e cevirme modelin isidir (float'a dusme riski yok)
    df = pd.read_csv(ensure_data(data_dir) / PRODUCTION_CSV, dtype=str)
    return df.sort_values(["order_date", "order_id"], kind="stable").reset_index(drop=True)


def load_cutting(data_dir: Path | None = None) -> pd.DataFrame:
    return pd.read_csv(ensure_data(data_dir) / CUTTING_CSV, dtype=str)


def split_time_ordered(df: pd.DataFrame, test_fraction: float = TEST_FRACTION) -> tuple[pd.DataFrame, pd.DataFrame]:
    """Zaman siralı bolme: en eski %80 egitim, en yeni %20 test."""
    ordered = df.sort_values(["order_date", "order_id"], kind="stable").reset_index(drop=True)
    n_test = max(1, round(len(ordered) * test_fraction))
    cut = len(ordered) - n_test
    return ordered.iloc[:cut].reset_index(drop=True), ordered.iloc[cut:].reset_index(drop=True)


def split_random(
    df: pd.DataFrame, test_fraction: float = TEST_FRACTION, seed: int = CUTTING_SPLIT_SEED
) -> tuple[pd.DataFrame, pd.DataFrame]:
    """Rastgele %80/20 bolme (sabit seed)."""
    rng = np.random.default_rng(seed)
    idx = rng.permutation(len(df))
    n_test = max(1, round(len(df) * test_fraction))
    test_idx, train_idx = np.sort(idx[:n_test]), np.sort(idx[n_test:])
    return df.iloc[train_idx].reset_index(drop=True), df.iloc[test_idx].reset_index(drop=True)
