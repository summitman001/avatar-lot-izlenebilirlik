import json
from decimal import Decimal

import pytest

from app import datasets
from train import evaluate


def test_metrikler_elle():
    # |100-110| = 10, |200-180| = 20 -> MAE 15 ; MAPE (10/100 + 20/200)/2 = %10
    m = evaluate.regression_metrics([Decimal("100"), Decimal("200")], [Decimal("110"), Decimal("180")])
    assert m == {"mae": Decimal("15.0000"), "mape_pct": Decimal("10.0000")}


def test_metrik_uzunluk_hatasi():
    with pytest.raises(ValueError):
        evaluate.regression_metrics([Decimal(1)], [])


def test_zaman_sirali_bolme_sizinti_yok():
    train, test = datasets.split_time_ordered(datasets.load_production())
    assert (len(train), len(test)) == (48, 12)
    assert train.order_date.max() <= test.order_date.min()


def test_rastgele_bolme_deterministik_ve_ayrik():
    df = datasets.load_cutting()
    a_tr, a_te = datasets.split_random(df)
    b_tr, b_te = datasets.split_random(df)
    assert (len(a_tr), len(a_te)) == (160, 40)
    assert a_te.lamb_id.tolist() == b_te.lamb_id.tolist()
    assert not set(a_tr.lamb_id) & set(a_te.lamb_id)


def test_ogrenme_egrisi_boyutlari_ve_alt_kume():
    prod_train, prod_test = datasets.split_time_ordered(datasets.load_production())
    cut_train, cut_test = datasets.split_random(datasets.load_cutting())
    for n in (5, 40):
        sub = evaluate.training_subset("production", prod_train, n)
        assert len(sub) == n and sub.order_date.min() >= prod_train.order_date.iloc[-n]  # en yeni n kayit
    small = set(evaluate.training_subset("cutting", cut_train, 25).lamb_id)
    big = set(evaluate.training_subset("cutting", cut_train, 50).lamb_id)
    assert small < big  # ic ice altkumeler


def test_ogrenme_egrisi_raporu(tmp_path):
    report = evaluate.run_learning_curve(tmp_path / "lc.json")
    sizes = {t: [p["n_train"] for p in next(iter(v["models"].values()))["points"]] for t, v in report.items()}
    assert sizes == {"production": [5, 10, 20, 40], "cutting": [10, 25, 50, 100, 160]}
    for task in report.values():
        for model in task["models"].values():
            assert all(isinstance(p["mae"], str) and isinstance(p["mape_pct"], str) for p in model["points"])


def test_ogrenme_egrisi_dosyasi_guncel(tmp_path):
    fresh = evaluate.run_learning_curve(tmp_path / "lc.json")
    assert json.loads(evaluate.CURVE_PATH.read_text(encoding="utf-8")) == fresh


def test_rapor_yazilir(tmp_path):
    out = tmp_path / "baseline_metrics.json"
    report = evaluate.run(out)
    assert json.loads(out.read_text(encoding="utf-8")) == report
    p = report["production"]["models"]["production_baseline_supplier_mean"]["metrics"]
    c = report["cutting"]["models"]["cutting_baseline_breed_mean"]["metrics"]["overall"]
    assert all(isinstance(v, str) for v in (p["mae_kg"], p["mape_pct"], c["mae_kg"], c["mape_pct"]))


def test_baseline_rapor_dosyasi_guncel(tmp_path):
    """reports/baseline_metrics.json mevcut kod ve veriyle birebir ayni uretilmeli."""
    out = tmp_path / "m.json"
    fresh = evaluate.run(out)
    assert json.loads(evaluate.REPORT_PATH.read_text(encoding="utf-8")) == fresh
