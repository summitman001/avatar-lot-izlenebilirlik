import json
from decimal import Decimal

import pytest

from app import datasets
from app.models.base import CUTTING, PRODUCTION
from train import cv as cvmod
from train import evaluate


def test_metrikler_elle():
    # |100-110| = 10, |200-180| = 20 -> MAE 15 ; MAPE (10/100 + 20/200)/2 = %10
    m = evaluate.regression_metrics([Decimal("100"), Decimal("200")], [Decimal("110"), Decimal("180")])
    assert m == {"mae": Decimal("15.0000"), "mape_pct": Decimal("10.0000")}


def test_metrik_uzunluk_hatasi():
    with pytest.raises(ValueError):
        evaluate.regression_metrics([Decimal(1)], [])


def test_rolling_origin_katmanlari_sizinti_yok():
    """Production: ilk 40 kayitla basla, her katman 10 test, genisleyen pencere -> 8 katman."""
    df = datasets.load_production()
    folds = cvmod.rolling_origin_folds(df, 40, 10)
    assert len(folds) == 8
    assert [f.n_train for f in folds] == [40, 50, 60, 70, 80, 90, 100, 110]
    assert all(f.n_test == 10 for f in folds)
    for f in folds:
        assert f.train.order_date.max() <= f.test.order_date.min()  # zaman sizintisi yok
    # pencereler genisliyor: bir sonraki katmanin egitimi oncekinin tamamini icerir
    assert set(folds[0].train.order_id) < set(folds[1].train.order_id)


def test_kfold_deterministik_ve_ayrik():
    df = datasets.load_cutting()
    a = cvmod.kfold_folds(df, 5, 42)
    b = cvmod.kfold_folds(df, 5, 42)
    assert len(a) == 5
    assert sum(f.n_test for f in a) == len(df)
    assert [f.test.lamb_id.tolist() for f in a] == [f.test.lamb_id.tolist() for f in b]  # deterministik
    for f in a:
        assert not set(f.train.lamb_id) & set(f.test.lamb_id)  # ayrik


def test_paired_significance_ozdes_seriler():
    r = cvmod.paired_significance([1.0, 2.0, 3.0], [1.0, 2.0, 3.0])
    assert r["p_value"] == 1.0


def test_paired_significance_yetersiz_katman():
    r = cvmod.paired_significance([1.0, 2.0], [1.0, 1.0])
    assert r["p_value"] is None


def test_evaluate_production_ekp_metrigi_var():
    df = datasets.load_production()
    folds = cvmod.rolling_origin_folds(df, 40, 10)
    from app.models.baseline import SupplierMeanYield

    m = evaluate.evaluate_production(SupplierMeanYield(), folds[0].train, folds[0].test)
    assert "ekp" in m and Decimal(m["ekp"]["mae_eur"]) >= 0
    assert len(m["yield_residuals"]) == len(folds[0].test)


def test_evaluate_cutting_normalizasyon_etkisi_ridge_multi_icin_var():
    df = datasets.load_cutting()
    folds = cvmod.kfold_folds(df, 5, 42)
    from app.models.cutting_ml import RidgeMultiCutting

    m = evaluate.evaluate_cutting(RidgeMultiCutting(), folds[0].train, folds[0].test)
    assert "normalization_effect" in m
    assert len(m["fire_residuals"]) == len(folds[0].test)


def test_cv_protokolu_deterministik():
    """Aynı seed/veri -> aynı metrikler (CV protokolu deterministik olmali)."""
    a, _ = evaluate.run_cv(baseline_only=True)
    b, _ = evaluate.run_cv(baseline_only=True)
    assert a == b


def test_ogrenme_egrisi_boyutlari():
    import tempfile
    from pathlib import Path

    with tempfile.TemporaryDirectory() as tmp:
        report = evaluate.run_learning_curve(Path(tmp) / "lc.json", baseline_only=True)
    sizes = {t: [p["n_train"] for p in next(iter(v["models"].values()))["points"]] for t, v in report.items()}
    assert sizes == {PRODUCTION: [5, 10, 20, 40], CUTTING: [10, 25, 50, 100, 160]}
    for task in report.values():
        for model in task["models"].values():
            assert all(isinstance(p["mae"], str) and isinstance(p["mape_pct"], str) for p in model["points"])


def test_model_comparison_dosyasi_guncel():
    """reports/model_comparison.json ve cv_folds.json mevcut kod/veriyle birebir ayni uretilmeli."""
    comparison, folds_detail = evaluate.run_cv(baseline_only=False)
    assert json.loads(evaluate.COMPARISON_PATH.read_text(encoding="utf-8")) == comparison
    assert json.loads(evaluate.FOLDS_PATH.read_text(encoding="utf-8")) == folds_detail


def test_learning_curve_dosyasi_guncel():
    fresh = evaluate.run_learning_curve(evaluate.CURVE_PATH.parent / "_tmp_lc.json", baseline_only=False)
    assert json.loads(evaluate.CURVE_PATH.read_text(encoding="utf-8")) == fresh
    (evaluate.CURVE_PATH.parent / "_tmp_lc.json").unlink(missing_ok=True)


def test_model_comparison_durustluk_production_baseline_gecemedi():
    """DURUSTLUK KURALI: production icin ML baseline'i gecmiyor, bu acikca raporlanir."""
    comparison = json.loads(evaluate.COMPARISON_PATH.read_text(encoding="utf-8"))
    for name in ("production_ridge", "production_gbm"):
        entry = comparison[PRODUCTION]["models"][name]
        assert entry["improvement_vs_baseline_pct"] is not None and entry["improvement_vs_baseline_pct"] < 0


def test_model_comparison_cutting_ridge_multi_anlamli_iyilesme():
    comparison = json.loads(evaluate.COMPARISON_PATH.read_text(encoding="utf-8"))
    entry = comparison[CUTTING]["models"]["cutting_ridge_multi"]
    assert entry["improvement_vs_baseline_pct"] > 0
    assert entry["significance_vs_baseline"]["p_value"] < 0.05
