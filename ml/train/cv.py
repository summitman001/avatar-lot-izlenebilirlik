"""Degerlendirme protokolu: rolling-origin CV (production) ve k-fold CV (cutting).

Tek bir train/test bolmesi KULLANILMAZ (gurultulu/gurultuye asiri hassas sonuc verir).
Production: zaman sirali, genisleyen pencere (varsayilan: ilk 40 kayitla basla, her
katmanda sonraki 10 kaydi test et). Cutting: 5-fold CV (shuffle, sabit seed).

Ayni fonksiyonlar ogrenme egrisinde de kullanilir (initial_train/n_splits degisir),
boylece ogrenme egrisi de ayni protokolden gecer.
"""
from __future__ import annotations

from dataclasses import dataclass

import numpy as np
import pandas as pd
from scipy import stats


@dataclass(frozen=True)
class Fold:
    index: int
    train: pd.DataFrame
    test: pd.DataFrame
    n_train: int
    n_test: int


def rolling_origin_folds(df: pd.DataFrame, initial_train: int, step: int = 10) -> list[Fold]:
    """Zaman sirali genisleyen pencere: train=[0:initial_train+step*k], test=sonraki `step` kayit.

    `df` zaten zaman sirasina gore siralanmis olmalidir (order_date, order_id).
    """
    folds = []
    n = len(df)
    k = 0
    while True:
        train_end = initial_train + step * k
        test_end = train_end + step
        if test_end > n:
            break
        folds.append(
            Fold(
                index=k,
                train=df.iloc[:train_end].reset_index(drop=True),
                test=df.iloc[train_end:test_end].reset_index(drop=True),
                n_train=train_end,
                n_test=step,
            )
        )
        k += 1
    if not folds:
        raise ValueError(f"rolling-origin CV icin yetersiz veri: n={n}, initial_train={initial_train}, step={step}")
    return folds


def fixed_window_folds(df: pd.DataFrame, window: int, step: int = 10) -> list[Fold]:
    """Sabit boyutlu kayan pencere: train=[step*k : step*k+window], test=sonraki `step` kayit.

    Ogrenme egrisinde kullanilir: egitim boyutu SABIT tutulup (farkli `window` degerleri
    icin) birden fazla zaman dilimi uzerinden ortalanir -- boylece "egitim boyutunun
    etkisi" ile "hangi donemin test edildigi" birbirine karismaz (genisleyen pencereden
    farki budur).
    """
    folds = []
    n = len(df)
    k = 0
    while True:
        train_start = step * k
        train_end = train_start + window
        test_end = train_end + step
        if test_end > n:
            break
        folds.append(
            Fold(
                index=k,
                train=df.iloc[train_start:train_end].reset_index(drop=True),
                test=df.iloc[train_end:test_end].reset_index(drop=True),
                n_train=window,
                n_test=step,
            )
        )
        k += 1
    if not folds:
        raise ValueError(f"sabit pencereli CV icin yetersiz veri: n={n}, window={window}, step={step}")
    return folds


def kfold_folds(df: pd.DataFrame, n_splits: int = 5, seed: int = 42) -> list[Fold]:
    """Rastgele k-fold (shuffle, sabit seed). Siniflandirma degil regresyon oldugu icin basit KFold."""
    rng = np.random.default_rng(seed)
    order = rng.permutation(len(df))
    fold_sizes = np.full(n_splits, len(df) // n_splits, dtype=int)
    fold_sizes[: len(df) % n_splits] += 1
    folds = []
    start = 0
    for k, size in enumerate(fold_sizes):
        test_idx = np.sort(order[start : start + size])
        train_idx = np.sort(np.concatenate([order[:start], order[start + size :]]))
        folds.append(
            Fold(
                index=k,
                train=df.iloc[train_idx].reset_index(drop=True),
                test=df.iloc[test_idx].reset_index(drop=True),
                n_train=len(train_idx),
                n_test=len(test_idx),
            )
        )
        start += size
    return folds


def paired_significance(model_errors: list[float], baseline_errors: list[float]) -> dict[str, object]:
    """Katman-bazli (fold-level) ortalama mutlak hatalari eslestirip paired t-test uygular.

    Fark anlamlıysa (p<0.05) raporlanir; degilse de aciklama ile birlikte p degeri donulur.
    Katman sayisi < 3 ise test anlamli olamaz (yetersiz serbestlik derecesi); None doner.
    """
    if len(model_errors) != len(baseline_errors):
        raise ValueError("model ve baseline katman sayisi ayni olmali")
    if len(model_errors) < 3:
        return {"test": "paired_t_test", "p_value": None, "note": "katman sayisi < 3, test anlamli degil"}
    diffs = np.array(model_errors) - np.array(baseline_errors)
    if np.allclose(diffs, 0.0):
        return {"test": "paired_t_test", "p_value": 1.0, "note": "model ve baseline katman hatalari ozdes"}
    try:
        t_stat, p_value = stats.ttest_rel(model_errors, baseline_errors)
        return {"test": "paired_t_test", "statistic": float(t_stat), "p_value": float(p_value)}
    except Exception as exc:  # pragma: no cover - savunma
        return {"test": "paired_t_test", "p_value": None, "note": f"hesaplanamadi: {exc}"}
