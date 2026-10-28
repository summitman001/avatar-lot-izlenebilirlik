from app import datasets, model_store


def test_egitilmis_model_dosyalari_yuklenebiliyor():
    """ml/models/*.joblib (commitlenmis) yuklenebilmeli ve tahmin uretebilmeli."""
    prod_df = datasets.load_production()
    cut_df = datasets.load_cutting()
    for task, name, df in [
        ("production", "production_ridge", prod_df),
        ("production", "production_gbm", prod_df),
        ("cutting", "cutting_ridge_multi", cut_df),
        ("cutting", "cutting_gbm_multi", cut_df),
    ]:
        model = model_store.load(task, name)
        assert model is not None
        preds = model.predict(df.iloc[:2])
        assert len(preds) == 2


def test_model_store_olmayan_model_none_doner():
    assert model_store.load("production", "yok_boyle_model") is None
