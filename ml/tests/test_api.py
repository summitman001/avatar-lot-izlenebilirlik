from decimal import Decimal

import pytest
from fastapi.testclient import TestClient

from app.main import app


@pytest.fixture(scope="module")
def client():
    with TestClient(app) as c:
        yield c


def test_health(client):
    r = client.get("/health")
    assert r.status_code == 200 and r.json() == {"status": "ok", "models_loaded": True}


def test_models(client):
    r = client.get("/models")
    assert r.status_code == 200
    models = {m["model_name"]: m for m in r.json()["models"]}
    assert set(models) == {"production_baseline_supplier_mean", "cutting_baseline_breed_mean"}
    for m in models.values():
        assert m["baseline"] is True and m["active"] is True and m["model_version"] and m["trained_at"]
        assert m["metrics"]["holdout"]


def test_predict_production(client):
    r = client.post("/predict/production", json={"et_kg": "100", "supplier_id": "TED-A"})
    assert r.status_code == 200
    body = r.json()
    assert body["baseline"] is True and body["model_name"] and body["model_version"]
    assert body["supplier_used"] == "TED-A" and body["fallback"] is False
    assert all(isinstance(body[k], str) for k in ("predicted_yield_ratio", "predicted_delivered_qty", "et_kg"))
    diff = Decimal(body["predicted_delivered_qty"]) - Decimal("100") * Decimal(body["predicted_yield_ratio"])
    assert abs(diff) < Decimal("0.01")  # verim 4 ondalik gosterim, adet 2 ondalik


def test_predict_production_500g_urun(client):
    kg1 = client.post("/predict/production", json={"et_kg": "100", "supplier_id": "TED-B"}).json()
    g500 = client.post("/predict/production", json={"et_kg": "100", "supplier_id": "TED-B", "unit_weight_kg": "0.500"}).json()
    # ayni cikti kg, adet iki kat
    assert Decimal(g500["predicted_output_kg"]) == Decimal(kg1["predicted_output_kg"])
    assert len(kg1["predicted_output_kg"].split(".")[1]) == 3  # kg 3 ondalik
    assert Decimal(g500["predicted_delivered_qty"]) == 2 * Decimal(kg1["predicted_delivered_qty"])


def test_predict_production_tedarikci_yoksa_genel_ortalama(client):
    unknown = client.post("/predict/production", json={"et_kg": "100", "supplier_id": "YOK"}).json()
    none = client.post("/predict/production", json={"et_kg": "100"}).json()
    assert unknown["supplier_used"] == none["supplier_used"] == "ALL"
    assert unknown["fallback"] is True
    assert unknown["predicted_delivered_qty"] == none["predicted_delivered_qty"]


def test_predict_cutting(client):
    r = client.post("/predict/cutting", json={"carcass_kg": "22", "breed": "Akkaraman", "sex": "F"})
    assert r.status_code == 200
    body = r.json()
    assert body["baseline"] is True and body["breed_used"] == "Akkaraman"
    sellable = sum(Decimal(p["kg"]) for p in body["parts"])
    assert sellable == Decimal(body["sellable_kg"])
    assert sellable + Decimal(body["expected_fire_kg"]) == Decimal("22")  # karkas = parcalar + fire, birebir
    fire_ratio = Decimal(body["expected_fire_ratio"])
    assert abs(sellable - Decimal("22") * (1 - fire_ratio)) <= Decimal("0.01")
    assert {p["part"] for p in body["parts"]} == {"but", "kol", "sirt", "kaburga", "gerdan", "kiyma"}


def test_predict_cutting_irk_yoksa_genel(client):
    body = client.post("/predict/cutting", json={"carcass_kg": "20"}).json()
    assert body["breed_used"] == "ALL" and body["fallback"] is True


@pytest.mark.parametrize("path,payload", [("/predict/production", {"et_kg": "0"}), ("/predict/cutting", {"carcass_kg": "-1"})])
def test_gecersiz_girdi_422(client, path, payload):
    assert client.post(path, json=payload).status_code == 422
