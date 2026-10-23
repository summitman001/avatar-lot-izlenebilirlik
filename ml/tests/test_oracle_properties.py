import json
from decimal import Decimal

import pytest
from hypothesis import given, settings
from hypothesis import strategies as st

from oracle import cli, cutting_allocation, production_cost
from oracle.cutting_allocation import Part, allocate_cutting
from oracle.decimal_utils import D, money, unit4
from oracle.rounding import distribute

kg_st = st.decimals(min_value="0.001", max_value="60", places=3)
price_st = st.decimals(min_value="0", max_value="40", places=2)


@given(
    total=st.decimals(min_value="0", max_value="100000", places=2),
    weights=st.lists(st.decimals(min_value="0.001", max_value="500", places=3), min_size=1, max_size=15),
)
def test_distribute_toplam_birebir(total, weights):
    out = distribute(total, weights, 2)
    assert sum(out) == total
    assert all(x.as_tuple().exponent == -2 for x in out)


@st.composite
def part_sets(draw):
    n_main = draw(st.integers(min_value=1, max_value=8))
    parts = [Part(f"P{i}", draw(kg_st), draw(price_st)) for i in range(n_main)]
    for j in range(draw(st.integers(min_value=0, max_value=2))):
        parts.append(Part(f"Y{j}", draw(kg_st), draw(price_st), is_by_product=True))
    for j in range(draw(st.integers(min_value=0, max_value=2))):
        parts.append(Part(f"F{j}", draw(kg_st), Decimal("0"), is_waste=True))
    return parts


@settings(max_examples=300, deadline=None)
@given(
    parts=part_sets(),
    method=st.sampled_from(["quantity", "value"]),
    weight=st.decimals(min_value="1", max_value="200", places=3),
    unit_cost=st.decimals(min_value="1", max_value="30", places=2),
)
def test_dagitim_toplami_maliyete_esit(parts, method, weight, unit_cost):
    total = money(weight * unit_cost)
    by_product = sum((money(p.kg * p.reference_price) for p in parts if p.is_by_product), Decimal(0))
    value_weights = [p.kg * p.reference_price for p in parts if not p.is_waste and not p.is_by_product]
    if method == "value" and sum(value_weights) == 0:
        return  # tum ana parcalar fiyatsiz: hesaplanamaz, ValueError beklenir
    if by_product > total:
        with pytest.raises(ValueError):
            allocate_cutting(weight, parts, method, unit_cost_per_kg=unit_cost)
        return
    out = allocate_cutting(weight, parts, method, unit_cost_per_kg=unit_cost)
    assert sum(D(p["allocated_cost"]) for p in out["parts"]) == total
    assert D(out["total_allocated"]) == total
    for p in out["parts"]:
        if p["role"] == "waste":
            assert p["allocated_cost"] == "0.00"


def test_fire_maliyet_tasimaz_ve_yuku_ana_parcalara_biner():
    parts = [Part("A", Decimal("10"), Decimal("5")), Part("Fire", Decimal("5"), Decimal("0"), is_waste=True)]
    out = allocate_cutting(Decimal("15"), parts, "quantity", unit_cost_per_kg=Decimal("10"))
    assert [p["allocated_cost"] for p in out["parts"]] == ["150.00", "0.00"]


lot_st = st.builds(
    production_cost.Lot,
    name=st.just("x"),
    category=st.sampled_from(["meat", "spice", "casing", "other"]),
    quantity=st.decimals(min_value="0", max_value="500", places=3),
    unit_cost=st.decimals(min_value="0", max_value="60", places=4),
)


@settings(max_examples=300, deadline=None)
@given(
    lots=st.lists(lot_st, min_size=1, max_size=8),
    delivered=st.decimals(min_value="1", max_value="500", places=0),
    price=st.decimals(min_value="0", max_value="20", places=2),
    invoiced=st.one_of(st.none(), st.decimals(min_value="0", max_value="500", places=0)),
)
def test_satir_toplamlari_total_cost_a_birebir_esit(lots, delivered, price, invoiced):
    r = production_cost.calculate_production_cost(lots, delivered, price, invoiced_qty=invoiced)
    assert sum(a for _, a in r.lot_lines) == r.material_cost
    assert r.material_cost + r.service_cost == r.total_cost
    # her satir tek basina 2 ondalik ROUND_HALF_UP
    for lot, (_, amount) in zip(lots, r.lot_lines):
        assert amount == money(lot.quantity * lot.unit_cost)
    # ekp_display 4 ondalikli degerden degil, dogrudan total/adet'ten yuvarlanir
    assert r.ekp_display == money(r.total_cost / delivered)


def test_float_reddedilir():
    with pytest.raises(TypeError):
        D(1.5)
    with pytest.raises(TypeError):
        D(True)


def test_yuvarlama_kurali_half_up():
    assert money(Decimal("0.005")) == Decimal("0.01")
    assert money(Decimal("2.675")) == Decimal("2.68")  # float'ta 2.67 cikardi
    assert unit4(Decimal("0.00005")) == Decimal("0.0001")


def test_ekp_display_ile_degerleme_yanlis_unit_ile_dogru():
    inp = cli.production_cases()[0]["input"]
    e = production_cost.calculate(inp)
    wrong = money(D(e["ekp_display"]) * D(inp["delivered_qty"]))
    right = money(D(e["ekp_unit"]) * D(inp["delivered_qty"]))
    assert wrong == Decimal("1543.60") != D(e["total_cost"])
    assert right == D(e["total_cost"])


def test_faturalanan_verilmezse_teslim_alinana_esit():
    inp = {**cli.production_cases()[0]["input"], "invoiced_qty": None}
    e = production_cost.calculate(inp)
    assert e["invoiced_qty"] == "85" and e["warnings"] == []


def test_gecersiz_girdiler():
    inp = {**cli.production_cases()[0]["input"], "delivered_qty": "0"}
    with pytest.raises(ValueError):
        production_cost.calculate(inp)
    c = cli.cutting_cases()[0]["input"]
    with pytest.raises(ValueError):
        cutting_allocation.allocate({**c, "method": "weight"})
    with pytest.raises(ValueError):
        cutting_allocation.allocate({**c, "cost_basis": "declared", "declared_total_cost": None})


def test_golden_dosyalari_guncel(tmp_path):
    """Repodaki docs/golden/* oracle'in uretecegi icerikle birebir ayni olmali."""
    cli.main(["--out", str(tmp_path)])
    for name in ("production_cases.json", "cutting_cases.json", "cutting_case.json"):
        fresh = json.loads((tmp_path / name).read_text(encoding="utf-8"))
        committed = json.loads((cli.DEFAULT_OUT / name).read_text(encoding="utf-8"))
        assert fresh == committed, f"{name} eski, python -m oracle.cli calistirin"


def test_golden_format_duz_ve_decimaller_string():
    for name in ("production_cases.json", "cutting_cases.json"):
        for case in json.loads((cli.DEFAULT_OUT / name).read_text(encoding="utf-8")):
            assert set(case) == {"case_id", "description", "input", "expected"}
            _no_numbers(case)


def _no_numbers(node):
    if isinstance(node, dict):
        for v in node.values():
            _no_numbers(v)
    elif isinstance(node, list):
        for v in node:
            _no_numbers(v)
    else:
        assert not isinstance(node, (int, float)) or isinstance(node, bool), f"sayisal JSON degeri: {node!r}"


def test_recete_oranlari_toplami_bir():
    recipe = json.loads((cli.DEFAULT_OUT / "cutting_case.json").read_text(encoding="utf-8"))
    ratios = [D(p["expected_ratio"]) for p in recipe["expected"]["parts"]]
    assert sum(ratios) == Decimal("1")
    assert {p["part"] for p in recipe["expected"]["parts"]} >= {"But", "Kol", "Sırt", "Kaburga", "Gerdan", "Kıyma", "Fire"}
