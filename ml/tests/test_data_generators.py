import hashlib
from pathlib import Path
from decimal import Decimal

import numpy as np
import pandas as pd

from data import generate_cutting, generate_production
from oracle import production_cost
from oracle.decimal_utils import D


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _norm(path):
    return path.read_bytes().replace(b"\r\n", b"\n")


def test_commitlenen_ornek_csvler_uretecle_ayni(tmp_path):
    """data/out/ altindaki ornek kayitlar sabit seed ile uretilenle birebir ayni olmali."""
    out = Path(generate_production.DEFAULT_OUT)
    assert _norm(generate_production.main(["--out", str(tmp_path)])) == _norm(out / "production_orders.csv")
    assert _norm(generate_cutting.main(["--out", str(tmp_path)])) == _norm(out / "cutting_records.csv")


def test_production_deterministik(tmp_path):
    a = generate_production.main(["--out", str(tmp_path / "a")])
    b = generate_production.main(["--out", str(tmp_path / "b")])
    assert sha(a) == sha(b)


def test_cutting_deterministik(tmp_path):
    a = generate_cutting.main(["--out", str(tmp_path / "a")])
    b = generate_cutting.main(["--out", str(tmp_path / "b")])
    assert sha(a) == sha(b)


def test_production_ozellikleri(tmp_path):
    df = pd.read_csv(generate_production.main(["--out", str(tmp_path)]), dtype=str)
    assert len(df) == 120 and "meat_purchase_date" in df.columns
    assert df.order_date.min() >= "2025-01-01" and df.order_date.max() <= "2026-10-31"
    assert df.order_date.is_monotonic_increasing
    assert df.et_kg.map(Decimal).between(60, 150).all()
    assert set(df.supplier_id) == {"TED-A", "TED-B", "TED-C"} and set(df.producer_id) == {"URT-1", "URT-2"}
    assert df.order_id.is_unique


def test_production_ekp_oracle_ile_tutarli(tmp_path):
    df = pd.read_csv(generate_production.main(["--out", str(tmp_path)]), dtype=str)
    for r in df.itertuples():
        lots = [
            production_cost.Lot("e", "meat", D(r.et_kg), D(r.meat_unit_price)),
            production_cost.Lot("b", "spice", D(r.spice_kg), D(r.spice_unit_price)),
            production_cost.Lot("c", "casing", D(r.casing_kg), D(r.casing_unit_price)),
        ]
        res = production_cost.calculate_production_cost(
            lots, D(r.delivered_qty), D(r.service_unit_price), unit_weight_kg=D(r.unit_weight_kg)
        )
        assert str(res.ekp_unit) == r.ekp_unit and str(res.total_cost) == r.total_cost
        # satir yuvarlamasi: malzeme = yuvarlanmis satirlarin toplami
        assert sum(a for _, a in res.lot_lines) == D(r.material_cost)


def test_production_etkileri(tmp_path):
    df = pd.read_csv(generate_production.main(["--out", str(tmp_path)]))
    df["y"] = df.delivered_qty * df.unit_weight_kg / df.et_kg
    assert df.groupby("is_summer").y.mean().diff().iloc[-1] < 0  # yaz verimi dusuk
    assert df.y.corr(df.lot_age_days) < 0  # et lotu eskidikce verim duser
    assert df.lot_age_days.between(1, 10).all()
    assert (pd.to_datetime(df.order_date) - pd.to_datetime(df.meat_purchase_date)).dt.days.eq(df.lot_age_days).all()
    by_sup = df.groupby("supplier_id").y.mean()
    assert by_sup["TED-A"] > by_sup["TED-B"] > by_sup["TED-C"]
    # tedarikci x uretici etkilesimi: TED-A + URT-2 (-0.015) ayni tedarikcinin URT-1'inden dusuk
    a = df[df.supplier_id == "TED-A"].groupby("producer_id").y.mean()
    assert a["URT-2"] < a["URT-1"]
    assert (df["product"] == "Narin Sucuk").all() and (df.unit_weight_kg == 1.0).all()


def test_cutting_ozellikleri(tmp_path):
    df = pd.read_csv(generate_cutting.main(["--out", str(tmp_path)]), dtype=str)
    assert len(df) == 200
    assert set(df.breed) == {"Morkaraman", "Akkaraman", "Merinos"}
    assert df.carcass_kg.map(Decimal).between(14, 28).all()
    for r in df.to_dict("records"):
        total = sum(Decimal(r[f"{p}_kg"]) for p in generate_cutting.PARTS)
        assert total == Decimal(r["carcass_kg"])  # oranlar toplami 1: parcalar + fire = karkas, birebir
    ratio = lambda p: (df[f"{p}_kg"].map(Decimal) / df.carcass_kg.map(Decimal)).astype(float)
    assert 0.29 <= ratio("but").mean() <= 0.33
    assert 0.15 <= ratio("kol").mean() <= 0.19
    assert 0.08 <= ratio("gerdan").mean() <= 0.09
    assert 0.10 <= ratio("fire").mean() <= 0.12
    # agirlik arttikca but ve kol orani duser, kaburga ve kiyma artar
    w = df.carcass_kg.astype(float)
    assert ratio("but").corr(w) < 0 and ratio("kol").corr(w) < 0
    assert ratio("kaburga").corr(w) > 0 and ratio("kiyma").corr(w) > 0


def test_cutting_cinsiyet_ve_irk_agirlik_etkilesimi(tmp_path):
    df = pd.read_csv(generate_cutting.main(["--out", str(tmp_path)]))
    df["but_r"] = df.but_kg / df.carcass_kg
    df["kaburga_r"] = df.kaburga_kg / df.carcass_kg
    assert df.groupby("sex").but_r.mean()["M"] > df.groupby("sex").but_r.mean()["F"]
    assert df.groupby("sex").kaburga_r.mean()["F"] > df.groupby("sex").kaburga_r.mean()["M"]
    slope = {b: np.polyfit(g.carcass_kg, g.but_r, 1)[0] for b, g in df.groupby("breed")}
    assert slope["Merinos"] < slope["Morkaraman"] < 0  # Merinos'ta but orani agirlikla daha hizli duser
