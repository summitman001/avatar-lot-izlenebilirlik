from decimal import Decimal

import pandas as pd

from app import datasets
from app.models.cutting_ml import ALL_PARTS, GBMMultiCutting, RidgeMultiCutting
from app.models.production_ml import GBMYield, RidgeYield


def _production_df() -> pd.DataFrame:
    return datasets.load_production()


def _cutting_df() -> pd.DataFrame:
    return datasets.load_cutting()


def test_ridge_yield_fit_predict():
    df = _production_df()
    model = RidgeYield().fit(df)
    preds = model.predict(df.iloc[:5])
    assert len(preds) == 5
    for p in preds:
        assert Decimal("0.5") < p.yield_ratio < Decimal("1.2")
        assert p.delivered_qty > 0
        assert "supplier_used" in p.notes and "fallback" in p.notes


def test_gbm_yield_fit_predict():
    df = _production_df()
    model = GBMYield().fit(df)
    preds = model.predict(df.iloc[:5])
    assert len(preds) == 5
    assert all(p.delivered_qty > 0 for p in preds)


def test_ridge_yield_eksik_ozelliklerle_calisir():
    """Backend sozlesmesi sadece et_kg/supplier_id/producer_id/unit_weight_kg gonderir."""
    df = _production_df()
    model = RidgeYield().fit(df)
    minimal = pd.DataFrame([{"et_kg": "100", "supplier_id": "TED-A", "unit_weight_kg": "1.000"}])
    preds = model.predict(minimal)
    assert len(preds) == 1 and preds[0].delivered_qty > 0


def test_ridge_multi_cutting_normalize_toplami_karkasa_esit():
    df = _cutting_df()
    model = RidgeMultiCutting().fit(df)
    preds = model.predict(df.iloc[:10])
    for i, p in enumerate(preds):
        carcass = Decimal(df["carcass_kg"].iloc[i])
        total = sum(p.parts_kg.values(), Decimal(0)) + p.fire_kg
        assert total == carcass  # distribute() ile birebir esit (fiziksel kisit)


def test_gbm_multi_cutting_normalize_toplami_karkasa_esit():
    df = _cutting_df()
    model = GBMMultiCutting().fit(df)
    preds = model.predict(df.iloc[:10])
    for i, p in enumerate(preds):
        carcass = Decimal(df["carcass_kg"].iloc[i])
        total = sum(p.parts_kg.values(), Decimal(0)) + p.fire_kg
        assert total == carcass


def test_ridge_multi_cutting_predict_raw_normalize_etmez():
    df = _cutting_df()
    model = RidgeMultiCutting().fit(df)
    raw = model.predict_raw(df.iloc[:5])
    assert len(raw) == 5
    for r in raw:
        assert set(r) == set(ALL_PARTS)
        assert all(v >= 0 for v in r.values())


def test_kucuk_egitim_verisiyle_de_calisir():
    """Ogrenme egrisinin en kucuk noktalari (n=5 production) icin modeller patlamamali."""
    prod_df = _production_df()
    RidgeYield().fit(prod_df.iloc[:5]).predict(prod_df.iloc[5:6])
    GBMYield().fit(prod_df.iloc[:5]).predict(prod_df.iloc[5:6])
    cut_df = _cutting_df()
    RidgeMultiCutting().fit(cut_df.iloc[:10]).predict(cut_df.iloc[10:11])
    GBMMultiCutting().fit(cut_df.iloc[:10]).predict(cut_df.iloc[10:11])
