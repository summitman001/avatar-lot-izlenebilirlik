"""Ortak degerlendirme: python -m train.evaluate [--all] [--out PATH]

- production: zaman siralı bolme (en eski %80 egitim, en yeni %20 test)
- cutting: rastgele %80/20 (sabit seed)
Metrikler: MAE ve MAPE. Kayitli her model ayni fonksiyonla degerlendirilir; yarin
Ridge ve gradient boosting `app.registry.register` ile eklenince `--all` hepsini karsilastirir.
Metrikler JSON'da string (Decimal).
"""
from __future__ import annotations

import argparse
import json
from decimal import ROUND_HALF_UP, Decimal
from pathlib import Path
from typing import Any

import pandas as pd

from app import datasets, registry
from app.models.base import CUTTING, PRODUCTION
from oracle.decimal_utils import dstr

REPORT_PATH = datasets.ML_ROOT / "reports" / "baseline_metrics.json"
_Q4 = Decimal("0.0001")


def regression_metrics(y_true: list[Decimal], y_pred: list[Decimal]) -> dict[str, Decimal]:
    """MAE (hedef biriminde) ve MAPE (%). y_true == 0 olan satirlar MAPE'ye dahil edilmez."""
    if len(y_true) != len(y_pred) or not y_true:
        raise ValueError("y_true ve y_pred ayni uzunlukta ve bos olmamali")
    abs_err = [abs(t - p) for t, p in zip(y_true, y_pred)]
    mae = sum(abs_err, Decimal(0)) / len(abs_err)
    pct = [e / abs(t) * 100 for e, t in zip(abs_err, y_true) if t != 0]
    mape = sum(pct, Decimal(0)) / len(pct) if pct else Decimal(0)
    return {"mae": mae.quantize(_Q4, ROUND_HALF_UP), "mape_pct": mape.quantize(_Q4, ROUND_HALF_UP)}


def _fmt(metrics: dict[str, Decimal]) -> dict[str, str]:
    return {k: dstr(v) for k, v in metrics.items()}


def evaluate_production(model, train: pd.DataFrame, test: pd.DataFrame) -> dict[str, Any]:
    model.fit(train)
    preds = model.predict(test)
    y_true = [Decimal(x) for x in test["delivered_qty"]]
    m = regression_metrics(y_true, [p.delivered_qty for p in preds])
    return {"target": "delivered_qty (kg)", "mae_kg": dstr(m["mae"]), "mape_pct": dstr(m["mape_pct"])}


def evaluate_cutting(model, train: pd.DataFrame, test: pd.DataFrame) -> dict[str, Any]:
    model.fit(train)
    preds = model.predict(test)
    true_all: list[Decimal] = []
    pred_all: list[Decimal] = []
    per_part: dict[str, dict[str, str]] = {}
    for part in datasets.CUTTING_PARTS:
        t = [Decimal(x) for x in test[f"{part}_kg"]]
        p = [pr.fire_kg if part == "fire" else pr.parts_kg[part] for pr in preds]
        per_part[part] = _fmt({"mae_kg": regression_metrics(t, p)["mae"], "mape_pct": regression_metrics(t, p)["mape_pct"]})
        true_all += t
        pred_all += p
    overall = regression_metrics(true_all, pred_all)
    return {
        "target": "parca kg (but, kol, sirt, kaburga, gerdan, kiyma, fire)",
        "overall": {"mae_kg": dstr(overall["mae"]), "mape_pct": dstr(overall["mape_pct"])},
        "per_part": per_part,
    }


def build_splits(data_dir: Path | None = None) -> dict[str, tuple[pd.DataFrame, pd.DataFrame]]:
    return {
        PRODUCTION: datasets.split_time_ordered(datasets.load_production(data_dir)),
        CUTTING: datasets.split_random(datasets.load_cutting(data_dir)),
    }


_EVALUATORS = {PRODUCTION: evaluate_production, CUTTING: evaluate_cutting}
_SPLIT_NAMES = {
    PRODUCTION: "time_ordered_80_20 (en yeni %20 test)",
    CUTTING: f"random_80_20 (seed={datasets.CUTTING_SPLIT_SEED})",
}


def evaluate_task(task: str, train: pd.DataFrame, test: pd.DataFrame, baseline_only: bool = True) -> dict[str, Any]:
    models = {}
    for spec in registry.specs(task, baseline_only=baseline_only):
        models[spec.info.name] = {
            "version": spec.info.version,
            "baseline": spec.info.baseline,
            "metrics": _EVALUATORS[task](spec.factory(), train, test),
        }
    return {"split": _SPLIT_NAMES[task], "n_train": len(train), "n_test": len(test), "models": models}


def run(out: Path = REPORT_PATH, baseline_only: bool = True, data_dir: Path | None = None) -> dict[str, Any]:
    splits = build_splits(data_dir)
    report = {task: evaluate_task(task, tr, te, baseline_only) for task, (tr, te) in splits.items()}
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8", newline="\n")
    return report


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--all", action="store_true", help="baseline disindaki kayitli modelleri de degerlendir")
    parser.add_argument("--out", type=Path, default=None)
    args = parser.parse_args(argv)
    out = args.out or (REPORT_PATH if not args.all else REPORT_PATH.with_name("model_comparison.json"))
    report = run(out, baseline_only=not args.all)
    print(json.dumps(report, indent=2, ensure_ascii=False))
    print(f"yazildi: {out}")


if __name__ == "__main__":
    main()
