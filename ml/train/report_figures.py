"""Rapor grafikleri: python -m train.report_figures

docs/screenshots/ml/ altina 4 PNG uretir: ogrenme egrisi, tahmin-vs-gercek saçılım,
parca bazinda hata, kalinti histogrami. `docs/ml-report.md` bunlari gomuyor.
"""
from __future__ import annotations

import json
from pathlib import Path

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np

from app import datasets, registry
from app.models.base import CUTTING, PRODUCTION
from train import cv as cvmod
from train import evaluate

OUT_DIR = Path(__file__).resolve().parents[2] / "docs" / "screenshots" / "ml"

# dataviz skill: sabit kategorik sira (ilk 3 slot, tum ciftlerde dogrulanmis), text/surface tokenlari
BLUE, ORANGE, AQUA = "#2a78d6", "#eb6834", "#1baf7a"
TEXT_PRIMARY, TEXT_SECONDARY = "#0b0b0b", "#52514e"
SURFACE = "#fcfcfb"
GRID = "#e3e2dc"

plt.rcParams.update(
    {
        "figure.facecolor": SURFACE,
        "axes.facecolor": SURFACE,
        "savefig.facecolor": SURFACE,
        "text.color": TEXT_PRIMARY,
        "axes.labelcolor": TEXT_PRIMARY,
        "axes.edgecolor": GRID,
        "xtick.color": TEXT_SECONDARY,
        "ytick.color": TEXT_SECONDARY,
        "axes.grid": True,
        "grid.color": GRID,
        "grid.linewidth": 0.7,
        "axes.spines.top": False,
        "axes.spines.right": False,
        "font.size": 10,
    }
)

MODEL_COLORS = {
    "production_baseline_supplier_mean": BLUE,
    "production_ridge": ORANGE,
    "production_gbm": AQUA,
    "cutting_baseline_breed_mean": BLUE,
    "cutting_ridge_multi": ORANGE,
    "cutting_gbm_multi": AQUA,
}
MODEL_LABELS = {
    "production_baseline_supplier_mean": "baseline",
    "production_ridge": "ridge",
    "production_gbm": "gbm",
    "cutting_baseline_breed_mean": "baseline",
    "cutting_ridge_multi": "ridge_multi",
    "cutting_gbm_multi": "gbm_multi",
}


def fig_learning_curve(curve: dict) -> Path:
    fig, axes = plt.subplots(1, 2, figsize=(11, 4.2))
    for ax, task, title in [(axes[0], PRODUCTION, "Uretim (delivered_qty)"), (axes[1], CUTTING, "Parcalama (parca kg)")]:
        for name, m in curve[task]["models"].items():
            pts = m["points"]
            x = [p["n_train"] for p in pts]
            y = np.array([float(p["mae"]) for p in pts])
            std = np.array([float(p["mae_std"]) for p in pts])
            color = MODEL_COLORS[name]
            ax.plot(x, y, marker="o", markersize=5, linewidth=2, color=color, label=MODEL_LABELS[name])
            ax.fill_between(x, y - std, y + std, color=color, alpha=0.12, linewidth=0)
        ax.set_title(title, color=TEXT_PRIMARY, fontsize=11)
        ax.set_xlabel("egitim kaydi sayisi (n_train)")
        ax.set_ylabel("MAE (CV katmanlari ortalamasi)")
        ax.legend(frameon=False, fontsize=9)
    fig.suptitle("Ogrenme egrisi (rolling-origin / k-fold CV protokolu)", color=TEXT_PRIMARY, fontsize=12)
    fig.tight_layout()
    path = OUT_DIR / "learning_curve.png"
    fig.savefig(path, dpi=150)
    plt.close(fig)
    return path


def fig_pred_vs_actual() -> Path:
    fig, axes = plt.subplots(1, 2, figsize=(11, 4.6))

    prod_df = datasets.load_production()
    fold = cvmod.rolling_origin_folds(prod_df, 40, 10)[-1]
    active_name = registry.active_name(PRODUCTION)
    model = registry.get(PRODUCTION, active_name).factory().fit(fold.train)
    preds = model.predict(fold.test)
    actual = [float(v) for v in fold.test["yield_ratio"]]
    predicted = [float(p.yield_ratio) for p in preds]
    ax = axes[0]
    ax.scatter(actual, predicted, s=36, color=BLUE, alpha=0.85, edgecolor="white", linewidth=0.5)
    lo, hi = min(actual + predicted), max(actual + predicted)
    ax.plot([lo, hi], [lo, hi], color=TEXT_SECONDARY, linewidth=1.2, linestyle="--")
    ax.set_title(f"Uretim verimi ({MODEL_LABELS[active_name]}, son CV katmani)", fontsize=11)
    ax.set_xlabel("gercek yield_ratio")
    ax.set_ylabel("tahmin edilen yield_ratio")

    cut_df = datasets.load_cutting()
    fold_c = cvmod.kfold_folds(cut_df, 5, 42)[0]
    active_c = registry.active_name(CUTTING)
    model_c = registry.get(CUTTING, active_c).factory().fit(fold_c.train)
    preds_c = model_c.predict(fold_c.test)
    actual_c, predicted_c = [], []
    for part in ["but", "kol", "sirt", "kaburga", "gerdan", "kiyma"]:
        actual_c += [float(v) for v in fold_c.test[f"{part}_kg"]]
        predicted_c += [float(p.parts_kg[part]) for p in preds_c]
    ax2 = axes[1]
    ax2.scatter(actual_c, predicted_c, s=30, color=ORANGE, alpha=0.75, edgecolor="white", linewidth=0.5)
    lo2, hi2 = min(actual_c + predicted_c), max(actual_c + predicted_c)
    ax2.plot([lo2, hi2], [lo2, hi2], color=TEXT_SECONDARY, linewidth=1.2, linestyle="--")
    ax2.set_title(f"Parcalama ({MODEL_LABELS[active_c]}, ilk CV katmani, tum parcalar)", fontsize=11)
    ax2.set_xlabel("gercek parca kg")
    ax2.set_ylabel("tahmin edilen parca kg")

    fig.suptitle("Tahmin vs gercek (kesikli cizgi = birebir dogru)", color=TEXT_PRIMARY, fontsize=12)
    fig.tight_layout()
    path = OUT_DIR / "pred_vs_actual.png"
    fig.savefig(path, dpi=150)
    plt.close(fig)
    return path


def fig_per_part_error(folds_detail: dict) -> Path:
    baseline_name = next(s.info.name for s in registry.specs(CUTTING) if s.info.baseline)
    active_name = registry.active_name(CUTTING)
    parts = ["but", "kol", "sirt", "kaburga", "gerdan", "kiyma", "fire"]

    def mean_per_part(model_name: str) -> list[float]:
        folds = folds_detail[CUTTING]["folds"][model_name]
        out = []
        for part in parts:
            vals = [float(f["per_part"][part]["mae"]) for f in folds]
            out.append(float(np.mean(vals)))
        return out

    base_vals = mean_per_part(baseline_name)
    active_vals = mean_per_part(active_name)

    fig, ax = plt.subplots(figsize=(8, 4.2))
    x = np.arange(len(parts))
    width = 0.36
    ax.bar(x - width / 2, base_vals, width, color=BLUE, label=MODEL_LABELS[baseline_name])
    ax.bar(x + width / 2, active_vals, width, color=ORANGE, label=MODEL_LABELS[active_name])
    ax.set_xticks(x)
    ax.set_xticklabels(parts)
    ax.set_ylabel("MAE (kg, CV katmanlari ortalamasi)")
    ax.set_title("Parca bazinda hata: baseline vs aktif model", fontsize=11)
    ax.legend(frameon=False, fontsize=9)
    fig.tight_layout()
    path = OUT_DIR / "per_part_error.png"
    fig.savefig(path, dpi=150)
    plt.close(fig)
    return path


def fig_residual_histogram(folds_detail: dict) -> Path:
    fig, axes = plt.subplots(1, 2, figsize=(11, 4.2))

    name_p = registry.active_name(PRODUCTION)
    pooled_p = [r for f in folds_detail[PRODUCTION]["folds"][name_p] for r in f["yield_residuals"]]
    axes[0].hist(pooled_p, bins=18, color=BLUE, edgecolor="white", linewidth=0.6)
    axes[0].axvline(0, color=TEXT_SECONDARY, linewidth=1.2, linestyle="--")
    axes[0].set_title(f"Uretim verimi kalintilari ({MODEL_LABELS[name_p]})", fontsize=11)
    axes[0].set_xlabel("gercek - tahmin (yield_ratio)")
    axes[0].set_ylabel("adet")

    name_c = registry.active_name(CUTTING)
    pooled_c = [r for f in folds_detail[CUTTING]["folds"][name_c] for r in f["fire_residuals"]]
    axes[1].hist(pooled_c, bins=18, color=ORANGE, edgecolor="white", linewidth=0.6)
    axes[1].axvline(0, color=TEXT_SECONDARY, linewidth=1.2, linestyle="--")
    axes[1].set_title(f"Fire orani kalintilari ({MODEL_LABELS[name_c]})", fontsize=11)
    axes[1].set_xlabel("gercek - tahmin (fire orani)")
    axes[1].set_ylabel("adet")

    fig.suptitle("Kalinti dagilimi (CV test katmanlari, havuzlanmis)", color=TEXT_PRIMARY, fontsize=12)
    fig.tight_layout()
    path = OUT_DIR / "residual_hist.png"
    fig.savefig(path, dpi=150)
    plt.close(fig)
    return path


def main() -> None:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    comparison, folds_detail = evaluate.run_cv(baseline_only=False)
    curve = {task: evaluate.learning_curve_task(task, None, False, None) for task in (PRODUCTION, CUTTING)}
    for fn in (
        lambda: fig_learning_curve(curve),
        lambda: fig_pred_vs_actual(),
        lambda: fig_per_part_error(folds_detail),
        lambda: fig_residual_histogram(folds_detail),
    ):
        path = fn()
        print(f"yazildi: {path}")


if __name__ == "__main__":
    main()
