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
    assert set(models) == {
        "production_baseline_supplier_mean",
        "production_ridge",
        "production_gbm",
        "cutting_baseline_breed_mean",
        "cutting_ridge_multi",
        "cutting_gbm_multi",
    }
    for m in models.values():
        assert m["model_version"] and m["trained_at"]
        assert m["metrics"]["cv"] is not None
    # her gorev icin tam olarak bir aktif model olmali
    assert models["production_baseline_supplier_mean"]["active"] is True
    assert models["production_ridge"]["active"] is False
    assert models["production_gbm"]["active"] is False
    assert models["cutting_ridge_multi"]["active"] is True
    assert models["cutting_baseline_breed_mean"]["active"] is False
    assert models["cutting_gbm_multi"]["active"] is False


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
    # ayni cikti kg (adet 2 ondalik ayri yuvarlandigi icin kg'da en fazla 0.01 fark olabilir), adet ~iki kat
    assert abs(Decimal(g500["predicted_output_kg"]) - Decimal(kg1["predicted_output_kg"])) <= Decimal("0.01")
    assert len(kg1["predicted_output_kg"].split(".")[1]) == 3  # kg 3 ondalik
    assert abs(Decimal(g500["predicted_delivered_qty"]) - 2 * Decimal(kg1["predicted_delivered_qty"])) <= Decimal("0.02")


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
    # aktif model cutting_ridge_multi (CV karsilastirmasinda baseline'i anlamli sekilde gecti)
    assert body["model_name"] == "cutting_ridge_multi" and body["baseline"] is False
    assert body["breed_used"] == "Akkaraman"
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


def test_detect_anomaly_bos_istek_200_bos_liste(client):
    r = client.post("/detect/anomaly", json={"reference_id": "UE-999"})
    assert r.status_code == 200
    assert r.json() == {"reference_id": "UE-999", "findings": []}


def test_detect_anomaly_kutle_dengesi_tetiklenir(client):
    r = client.post("/detect/anomaly", json={"input_weight_kg": "20", "output_weights_kg": ["10", "15"]})
    assert r.status_code == 200
    findings = r.json()["findings"]
    assert len(findings) == 1 and findings[0]["code"] == "KUTLE_DENGESI" and findings[0]["severity"] == "critical"


def test_detect_anomaly_kutle_dengesi_tetiklenmez(client):
    r = client.post("/detect/anomaly", json={"input_weight_kg": "20", "output_weights_kg": ["10", "9"]})
    assert r.json()["findings"] == []


def test_detect_anomaly_maliyet_artisi_tetiklenir(client):
    r = client.post("/detect/anomaly", json={"current_unit_cost": "15", "recent_unit_costs": ["10", "10", "10"]})
    findings = r.json()["findings"]
    assert len(findings) == 1 and findings[0]["code"] == "MALIYET_ARTISI"


def test_detect_anomaly_maliyet_artisi_tetiklenmez(client):
    r = client.post("/detect/anomaly", json={"current_unit_cost": "10.2", "recent_unit_costs": ["10", "10", "10"]})
    assert r.json()["findings"] == []


def test_detect_anomaly_ekp_sapma_tetiklenir(client):
    r = client.post("/detect/anomaly", json={"predicted_ekp_unit": "10", "actual_ekp_unit": "11.2"})
    findings = r.json()["findings"]
    assert len(findings) == 1 and findings[0]["code"] == "EKP_SAPMA"


def test_detect_anomaly_ekp_sapma_tetiklenmez(client):
    r = client.post("/detect/anomaly", json={"predicted_ekp_unit": "10", "actual_ekp_unit": "10.3"})
    assert r.json()["findings"] == []


def test_detect_anomaly_verim_dusuk_tetiklenir(client):
    r = client.post("/detect/anomaly", json={"actual_yield_ratio": "0.60", "et_kg": "100", "supplier_id": "TED-A"})
    findings = r.json()["findings"]
    assert len(findings) == 1 and findings[0]["code"] == "VERIM_DUSUK"


def test_detect_anomaly_verim_dusuk_tetiklenmez(client):
    pred = client.post("/predict/production", json={"et_kg": "100", "supplier_id": "TED-A"}).json()
    r = client.post(
        "/detect/anomaly",
        json={"actual_yield_ratio": pred["predicted_yield_ratio"], "et_kg": "100", "supplier_id": "TED-A"},
    )
    assert r.json()["findings"] == []


def test_detect_anomaly_fire_yuksek_tetiklenir(client):
    r = client.post(
        "/detect/anomaly", json={"actual_fire_kg": "8", "carcass_kg": "22", "breed": "Akkaraman", "sex": "F"}
    )
    findings = r.json()["findings"]
    assert len(findings) == 1 and findings[0]["code"] == "FIRE_YUKSEK"


def test_detect_anomaly_fire_yuksek_tetiklenmez(client):
    pred = client.post("/predict/cutting", json={"carcass_kg": "22", "breed": "Akkaraman", "sex": "F"}).json()
    r = client.post(
        "/detect/anomaly",
        json={"actual_fire_kg": pred["expected_fire_kg"], "carcass_kg": "22", "breed": "Akkaraman", "sex": "F"},
    )
    assert r.json()["findings"] == []
