"""Degerlendirme: python -m train.evaluate [--all] [--out DIR]

DURUSTLUK KURALI: tek bir train/test bolmesi KULLANILMAZ (gurultulu/rastgele sonuc verir,
bkz. docs/ml-report.md "Yontem"). Butun modeller ayni protokolden gecer:

- production: rolling-origin CV (zaman sirali, genisleyen pencere). Ilk 40 kayitla basla,
  her katmanda sonraki 10 kaydi test et, egitim setini genisleterek ilerle (8 katman).
- cutting: 5-fold CV (shuffle, sabit seed).

Her model icin: MAE, MAPE, baseline'a gore iyilesme yuzdesi, katmanlar arasi standart
sapma, baseline'a karsi paired t-test p degeri. Production icin ayrica EKP hata metrigi
(hibrit formul: ML'in verim tahmini + KESIN malzeme maliyeti). Cutting icin ayrica
normalizasyon oncesi/sonrasi MAE (yalnizca raw tahmin ureten modellerde).

Cikti: reports/model_comparison.json (ozet), reports/cv_folds.json (katman detayi),
reports/learning_curve.json (ayni protokolle, artan egitim boyutuyla).
"""
from __future__ import annotations

import argparse
import json
from decimal import ROUND_HALF_UP, Decimal
from pathlib import Path
from typing import Any

import numpy as np
import pandas as pd

from app import datasets, registry
from app.models.base import CUTTING, PRODUCTION
from oracle.decimal_utils import dstr, money, unit4

from . import cv as cvmod

REPORT_DIR = datasets.ML_ROOT / "reports"
COMPARISON_PATH = REPORT_DIR / "model_comparison.json"
CURVE_PATH = REPORT_DIR / "learning_curve.json"
FOLDS_PATH = REPORT_DIR / "cv_folds.json"

CURVE_SIZES = {PRODUCTION: [5, 10, 20, 40], CUTTING: [10, 25, 50, 100, 160]}
CURVE_SEED = 7
ROLLING_INITIAL_TRAIN = 40
ROLLING_STEP = 10
CUTTING_N_SPLITS = 5
CUTTING_CV_SEED = 42
_Q4 = Decimal("0.0001")

_PROTOCOL_DESC = {
    PRODUCTION: f"rolling_origin (initial_train={ROLLING_INITIAL_TRAIN}, step={ROLLING_STEP})",
    CUTTING: f"kfold (n_splits={CUTTING_N_SPLITS}, seed={CUTTING_CV_SEED}, shuffle=True)",
}


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


def _ekp_errors(test: pd.DataFrame, preds: list) -> dict[str, str]:
    """Hibrit EKP formulu ile tahmin edilen EKP'nin gercek EKP'ye hatasi (bkz. docs/assumptions.md madde 11).

    predicted_ekp_unit = (material_cost [KESIN] + round2(predicted_qty * service_unit_price)) / predicted_qty
    """
    abs_err: list[Decimal] = []
    pct_err: list[Decimal] = []
    for i, p in enumerate(preds):
        predicted_qty = p.delivered_qty if p.delivered_qty > 0 else Decimal("0.01")
        material_cost = Decimal(test["material_cost"].iloc[i])
        service_price = Decimal(test["service_unit_price"].iloc[i])
        predicted_service_cost = money(predicted_qty * service_price)
        predicted_ekp = unit4((material_cost + predicted_service_cost) / predicted_qty)
        actual_ekp = Decimal(test["ekp_unit"].iloc[i])
        err = abs(actual_ekp - predicted_ekp)
        abs_err.append(err)
        if actual_ekp != 0:
            pct_err.append(err / actual_ekp * 100)
    mae = sum(abs_err, Decimal(0)) / len(abs_err)
    mape = sum(pct_err, Decimal(0)) / len(pct_err) if pct_err else Decimal(0)
    return {"mae_eur": dstr(mae.quantize(_Q4, ROUND_HALF_UP)), "mape_pct": dstr(mape.quantize(_Q4, ROUND_HALF_UP))}


def evaluate_production(model, train: pd.DataFrame, test: pd.DataFrame) -> dict[str, Any]:
    model.fit(train)
    preds = model.predict(test)
    y_true = [Decimal(x) for x in test["delivered_qty"]]
    m = regression_metrics(y_true, [p.delivered_qty for p in preds])
    # verim kalintilari: VERIM_DUSUK anomali kuralinin sigma esigi icin (bkz. app/anomaly.py)
    yield_residuals = [
        float(Decimal(test["yield_ratio"].iloc[i]) - p.yield_ratio) for i, p in enumerate(preds)
    ]
    return {
        "target": "delivered_qty (adet)",
        "mae_kg": dstr(m["mae"]),
        "mape_pct": dstr(m["mape_pct"]),
        "ekp": _ekp_errors(test, preds),
        "yield_residuals": yield_residuals,
    }


def evaluate_cutting(model, train: pd.DataFrame, test: pd.DataFrame) -> dict[str, Any]:
    model.fit(train)
    preds = model.predict(test)
    true_all: list[Decimal] = []
    pred_all: list[Decimal] = []
    per_part: dict[str, dict[str, str]] = {}
    for part in datasets.CUTTING_PARTS:
        t = [Decimal(x) for x in test[f"{part}_kg"]]
        p = [pr.fire_kg if part == "fire" else pr.parts_kg[part] for pr in preds]
        per_part[part] = _fmt(regression_metrics(t, p))
        true_all += t
        pred_all += p
    overall = regression_metrics(true_all, pred_all)
    # fire orani kalintilari: FIRE_YUKSEK anomali kuralinin sigma esigi icin (bkz. app/anomaly.py)
    fire_residuals = [
        float(Decimal(test["fire_kg"].iloc[i]) / Decimal(test["carcass_kg"].iloc[i]) - pr.notes["expected_fire_ratio"])
        for i, pr in enumerate(preds)
    ]
    result = {
        "target": "parca kg (but, kol, sirt, kaburga, gerdan, kiyma, fire)",
        "overall": {"mae_kg": dstr(overall["mae"]), "mape_pct": dstr(overall["mape_pct"])},
        "per_part": per_part,
        "fire_residuals": fire_residuals,
    }
    if hasattr(model, "predict_raw"):
        raw_preds = model.predict_raw(test)
        raw_true: list[Decimal] = []
        raw_pred: list[Decimal] = []
        for part in datasets.CUTTING_PARTS:
            raw_true += [Decimal(x) for x in test[f"{part}_kg"]]
            raw_pred += [rp[part] for rp in raw_preds]
        raw_overall = regression_metrics(raw_true, raw_pred)
        result["normalization_effect"] = {
            "raw_mae_kg": dstr(raw_overall["mae"]),
            "raw_mape_pct": dstr(raw_overall["mape_pct"]),
            "normalized_mae_kg": dstr(overall["mae"]),
            "normalized_mape_pct": dstr(overall["mape_pct"]),
        }
    return result


_EVALUATORS = {PRODUCTION: evaluate_production, CUTTING: evaluate_cutting}


def _overall_mae(task: str, metrics: dict[str, Any]) -> float:
    return float(metrics["mae_kg"]) if task == PRODUCTION else float(metrics["overall"]["mae_kg"])


def _overall_mape(task: str, metrics: dict[str, Any]) -> float:
    return float(metrics["mape_pct"]) if task == PRODUCTION else float(metrics["overall"]["mape_pct"])


def _std(values: list[float]) -> float:
    return float(np.std(values, ddof=1)) if len(values) > 1 else 0.0


def production_folds(data_dir: Path | None = None) -> list[cvmod.Fold]:
    return cvmod.rolling_origin_folds(datasets.load_production(data_dir), ROLLING_INITIAL_TRAIN, ROLLING_STEP)


def cutting_folds(data_dir: Path | None = None) -> list[cvmod.Fold]:
    return cvmod.kfold_folds(datasets.load_cutting(data_dir), CUTTING_N_SPLITS, CUTTING_CV_SEED)


_FOLD_BUILDERS = {PRODUCTION: production_folds, CUTTING: cutting_folds}


def run_cv(baseline_only: bool = False, data_dir: Path | None = None) -> tuple[dict[str, Any], dict[str, Any]]:
    """Tum kayitli modelleri CV protokolunden gecirir. (model_comparison, cv_folds) dondurur."""
    comparison: dict[str, Any] = {}
    folds_detail: dict[str, Any] = {}
    for task in (PRODUCTION, CUTTING):
        folds = _FOLD_BUILDERS[task](data_dir)
        specs = registry.specs(task, baseline_only=baseline_only)
        per_model_folds: dict[str, list[dict[str, Any]]] = {}
        for spec in specs:
            per_fold = []
            for fold in folds:
                metrics = _EVALUATORS[task](spec.factory(), fold.train, fold.test)
                per_fold.append({"fold": fold.index, "n_train": fold.n_train, "n_test": fold.n_test, **metrics})
            per_model_folds[spec.info.name] = per_fold

        baseline_name = next(s.info.name for s in specs if s.info.baseline)
        baseline_mae = [_overall_mae(task, fm) for fm in per_model_folds[baseline_name]]
        baseline_mean = float(np.mean(baseline_mae))

        models_summary: dict[str, Any] = {}
        for spec in specs:
            per_fold = per_model_folds[spec.info.name]
            mae_series = [_overall_mae(task, fm) for fm in per_fold]
            mape_series = [_overall_mape(task, fm) for fm in per_fold]
            mean_mae, mean_mape = float(np.mean(mae_series)), float(np.mean(mape_series))
            entry: dict[str, Any] = {
                "version": spec.info.version,
                "baseline": spec.info.baseline,
                "n_folds": len(per_fold),
                "mae_mean": round(mean_mae, 4),
                "mae_std": round(_std(mae_series), 4),
                "mape_pct_mean": round(mean_mape, 4),
                "mape_pct_std": round(_std(mape_series), 4),
            }
            if spec.info.baseline:
                entry["improvement_vs_baseline_pct"] = None
                entry["significance_vs_baseline"] = None
            else:
                entry["improvement_vs_baseline_pct"] = (
                    round((baseline_mean - mean_mae) / baseline_mean * 100, 2) if baseline_mean else None
                )
                entry["significance_vs_baseline"] = cvmod.paired_significance(mae_series, baseline_mae)
            if task == PRODUCTION:
                ekp_mae = [float(fm["ekp"]["mae_eur"]) for fm in per_fold]
                ekp_mape = [float(fm["ekp"]["mape_pct"]) for fm in per_fold]
                entry["ekp_mae_eur_mean"] = round(float(np.mean(ekp_mae)), 4)
                entry["ekp_mae_eur_std"] = round(_std(ekp_mae), 4)
                entry["ekp_mape_pct_mean"] = round(float(np.mean(ekp_mape)), 4)
            if task == CUTTING and "normalization_effect" in per_fold[0]:
                raw_mae = [float(fm["normalization_effect"]["raw_mae_kg"]) for fm in per_fold]
                entry["normalization_raw_mae_kg_mean"] = round(float(np.mean(raw_mae)), 4)
                entry["normalization_normalized_mae_kg_mean"] = round(mean_mae, 4)
            residual_key = "yield_residuals" if task == PRODUCTION else "fire_residuals"
            pooled_residuals = [r for fm in per_fold for r in fm.get(residual_key, [])]
            residual_field = "residual_std_yield_ratio" if task == PRODUCTION else "residual_std_fire_ratio"
            entry[residual_field] = round(_std(pooled_residuals), 6) if len(pooled_residuals) > 1 else 0.0
            models_summary[spec.info.name] = entry

        comparison[task] = {"protocol": _PROTOCOL_DESC[task], "baseline_model": baseline_name, "models": models_summary}
        folds_detail[task] = {"protocol": _PROTOCOL_DESC[task], "folds": per_model_folds}
    return comparison, folds_detail


def _write_json(out: Path, report: dict[str, Any]) -> None:
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8", newline="\n")


def run(out: Path = COMPARISON_PATH, folds_out: Path = FOLDS_PATH, baseline_only: bool = False, data_dir: Path | None = None):
    comparison, folds_detail = run_cv(baseline_only, data_dir)
    _write_json(out, comparison)
    _write_json(folds_out, folds_detail)
    return comparison, folds_detail


def training_subset(train: pd.DataFrame, n: int, seed: int = CURVE_SEED) -> pd.DataFrame:
    """Cutting icin ic ice altkume (sabit seed ile karistirilmis egitim verisinin ilk n kaydi)."""
    order = np.random.default_rng(seed).permutation(len(train))
    return train.iloc[np.sort(order[:n])].reset_index(drop=True)


def _curve_point(task: str, spec, n: int, data_dir: Path | None) -> dict[str, Any] | None:
    if task == PRODUCTION:
        df = datasets.load_production(data_dir)
        if n + ROLLING_STEP > len(df):
            return None
        folds = cvmod.fixed_window_folds(df, n, ROLLING_STEP)
        per_fold = [_EVALUATORS[task](spec.factory(), f.train, f.test) for f in folds]
    else:
        df = datasets.load_cutting(data_dir)
        kfolds = cvmod.kfold_folds(df, CUTTING_N_SPLITS, CUTTING_CV_SEED)
        if n > min(f.n_train for f in kfolds):
            return None
        per_fold = [_EVALUATORS[task](spec.factory(), training_subset(f.train, n), f.test) for f in kfolds]
    mae_series = [_overall_mae(task, m) for m in per_fold]
    mape_series = [_overall_mape(task, m) for m in per_fold]
    return {
        "n_train": n,
        "n_folds": len(per_fold),
        "mae": dstr(Decimal(str(round(float(np.mean(mae_series)), 4)))),
        "mae_std": dstr(Decimal(str(round(_std(mae_series), 4)))),
        "mape_pct": dstr(Decimal(str(round(float(np.mean(mape_series)), 4)))),
        "mape_pct_std": dstr(Decimal(str(round(_std(mape_series), 4)))),
    }


def learning_curve_task(task: str, sizes: list[int] | None, baseline_only: bool, data_dir: Path | None) -> dict[str, Any]:
    sizes = sizes or CURVE_SIZES[task]
    models = {}
    for spec in registry.specs(task, baseline_only=baseline_only):
        points = [p for n in sizes if (p := _curve_point(task, spec, n, data_dir)) is not None]
        models[spec.info.name] = {"version": spec.info.version, "baseline": spec.info.baseline, "points": points}
    unit = "kg (delivered_qty)" if task == PRODUCTION else "kg (tum parcalarin ortalamasi)"
    return {"protocol": _PROTOCOL_DESC[task], "mae_unit": unit, "models": models}


def run_learning_curve(
    out: Path = CURVE_PATH, baseline_only: bool = False, data_dir: Path | None = None
) -> dict[str, Any]:
    report = {task: learning_curve_task(task, None, baseline_only, data_dir) for task in (PRODUCTION, CUTTING)}
    _write_json(out, report)
    return report


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--baseline-only", action="store_true", help="yalnizca baseline modelleri degerlendir (hizli, gelistirme icin)"
    )
    parser.add_argument("--out", type=Path, default=None, help="cikti klasoru (varsayilan: reports/)")
    args = parser.parse_args(argv)
    out_dir = args.out or REPORT_DIR
    comparison_out = out_dir / "model_comparison.json"
    folds_out = out_dir / "cv_folds.json"
    curve_out = out_dir / "learning_curve.json"
    comparison, folds_detail = run(comparison_out, folds_out, baseline_only=args.baseline_only)
    print(json.dumps(comparison, indent=2, ensure_ascii=False))
    print(f"yazildi: {comparison_out}")
    print(f"yazildi: {folds_out}")
    run_learning_curve(curve_out, baseline_only=args.baseline_only)
    print(f"yazildi: {curve_out}")


if __name__ == "__main__":
    main()
