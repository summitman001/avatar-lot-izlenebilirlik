import hashlib
from decimal import Decimal

import pandas as pd

from data import generate_cutting, generate_production
from oracle import production_cost
from oracle.decimal_utils import D


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


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
    assert len(df) == 60
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
        res = production_cost.calculate_production_cost(lots, D(r.delivered_qty), D(r.service_unit_price))
        assert str(res.ekp_unit) == r.ekp_unit and str(res.total_cost) == r.total_cost


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
    # agirlik arttikca but orani duser
    assert pd.Series(ratio("but")).corr(df.carcass_kg.astype(float)) < 0
