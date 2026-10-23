"""Elle hesaplanmis degerler: oracle'dan bagimsiz, sabit sayilarla dogrulama.

Her sabitin yaninda elle yapilan aritmetik yorum olarak yazilidir. Ayrica
fractions.Fraction ile yazilmis ikinci, bagimsiz bir uygulama ile capraz kontrol yapilir.
"""
from decimal import ROUND_HALF_UP, Decimal
from fractions import Fraction

import pytest

from oracle import cli, cutting_allocation


def prod(case_id):
    return next(c for c in cli.production_cases() if c["case_id"] == case_id)["expected"]


def cut(case_id):
    return next(c for c in cli.cutting_cases() if c["case_id"] == case_id)["expected"]


def costs(e):
    return [p["allocated_cost"] for p in e["parts"]]


# ---------------- Gorev 1
def test_ur001_elle():
    e = prod("UR-001")
    # et 100 x 9.70 = 970.00 ; baharat 2 x 24.00 = 48.00 ; bagirsak 5 x 11.60 = 58.00 -> malzeme 1076.00
    assert e["material_cost"] == "1076.00"
    # hizmet 85 x 5.50 = 467.50 ; toplam 1076.00 + 467.50 = 1543.50
    assert e["service_cost"] == "467.50" and e["total_cost"] == "1543.50"
    # 1543.50 / 85 = 18.158823... -> 18.1588 ; 2 ondalik 18.16
    assert e["ekp_unit"] == "18.1588" and e["ekp_display"] == "18.16"
    # VKP 24.90: birim kar 24.90 - 18.158823 = 6.741176 -> 6.7412
    assert e["gross_profit_unit"] == "6.7412"
    # ciro 85 x 24.90 = 2116.50 ; kar 2116.50 - 1543.50 = 573.00 ; marj 573/2116.5 = 0.27073 -> 27.07
    assert e["revenue"] == "2116.50" and e["gross_profit_total"] == "573.00" and e["margin_pct"] == "27.07"
    assert e["warnings"] == []


def test_ur002_elle():
    e = prod("UR-002")
    # hizmet 83 x 5.50 = 456.50 ; toplam 1076.00 + 456.50 = 1532.50 ; 1532.50 / 83 = 18.463855 -> 18.4639
    assert (e["service_cost"], e["total_cost"], e["ekp_unit"], e["ekp_display"]) == ("456.50", "1532.50", "18.4639", "18.46")
    # ciro 83 x 24.90 = 2066.70 ; kar 2066.70 - 1532.50 = 534.20 ; marj 534.2/2066.7 = 0.258487 -> 25.85
    assert (e["gross_profit_total"], e["margin_pct"], e["gross_profit_unit"]) == ("534.20", "25.85", "6.4361")
    assert e["warnings"] == []


def test_ur003_elle():
    e = prod("UR-003")
    # hizmet 85 x 5.50 = 467.50 ; toplam 1543.50 ; 1543.50 / 83 = 18.596385 -> 18.5964
    assert (e["service_cost"], e["total_cost"], e["ekp_unit"], e["ekp_display"]) == ("467.50", "1543.50", "18.5964", "18.60")
    # kar 2066.70 - 1543.50 = 523.20 ; marj 523.2/2066.7 = 0.253157 -> 25.32
    assert (e["gross_profit_total"], e["margin_pct"]) == ("523.20", "25.32")
    assert e["warnings"] == ["INVOICED_QTY_MISMATCH"]


def test_ur004_yuvarlama_kaniti():
    e = prod("UR-004")
    # 18.16 x 85 = 18 x 85 + 0.16 x 85 = 1530 + 13.60 = 1543.60 ; gercek toplam 1543.50 ; fark 0.10
    assert e["rounding_proof"] == {"ekp_display_times_qty": "1543.60", "total_cost": "1543.50", "difference": "0.10"}
    assert Decimal("18.16") * 85 == Decimal("1543.60") != Decimal("1543.50")
    # dogru yol: 4 ondalikli birim ile 18.1588 x 85 = 1543.498 -> 1543.50
    assert (Decimal("18.1588") * 85).quantize(Decimal("0.01"), ROUND_HALF_UP) == Decimal("1543.50")


def test_ur005_verim():
    e = prod("UR-005")
    # girdi 100 + 2 + 5 = 107 kg ; 85/107 = 0.794392 -> %79.44 ; et bazli 85/100 = 0.85
    assert (e["input_kg_total"], e["yield_input_pct"], e["yield_meat_ratio"]) == ("107.000", "79.44", "0.8500")


# ---------------- Gorev 2
def test_kz001_dengesizlik():
    e = cut("KZ-2026-001-QTY")
    # parcalar 7.5 + 4.5 + 2.5 + 3.0 + 2.0 + 2.5 = 22.0 ; 25 - 22 = 3.0 kg fark (> 0.05)
    assert e["parts_weight_kg"] == "22.000" and e["balance_diff_kg"] == "3.000"
    assert e["closable"] is False and "WEIGHT_IMBALANCE" in e["warnings"]
    # 25 x 11.50 = 287.50 ; beyan 351.00 farkli
    assert e["total_input_cost"] == "287.50" and "PURCHASE_TOTAL_MISMATCH" in e["warnings"]


def test_kz002_quantity_elle():
    e = cut("KZ-2026-002-QTY")
    assert e["closable"] is True and e["balance_diff_kg"] == "0.000"
    # 287.50 x kg / 22: but 2156.25/22 = 98.0114 ; kol 1293.75/22 = 58.8068 ; sirt 718.75/22 = 32.6705
    # kaburga 862.50/22 = 39.2045 ; gerdan 575/22 = 26.1364 ; kiyma 32.6705 ; fire 0
    assert costs(e) == ["98.01", "58.81", "32.67", "39.20", "26.14", "32.67", "0.00"]
    # toplam 98.01 + 58.81 + 32.67 + 39.20 + 26.14 + 32.67 = 287.50 (fark yok)
    assert e["total_allocated"] == "287.50"


def test_kz002_value_elle():
    e = cut("KZ-2026-002-VAL")
    # degerler: 120 + 67.5 + 45 + 36 + 20 + 27.5 = 316 ; oran 287.50/316
    # but 109.1772 ; kol 61.4123 ; sirt 40.9415 ; kaburga 32.7532 ; gerdan 18.1962 ; kiyma 25.0198
    assert costs(e) == ["109.18", "61.41", "40.94", "32.75", "18.20", "25.02", "0.00"]
    # toplam 109.18 + 61.41 + 40.94 + 32.75 + 18.20 + 25.02 = 287.50
    assert e["total_allocated"] == "287.50"


def test_kz003_declared_elle():
    q = cut("KZ-2026-003-QTY")
    # 351 x kg / 22: but 119.6591 ; kol 71.7955 ; sirt 39.8864 ; kaburga 47.8636 ; gerdan 31.9091 ; kiyma 39.8864
    # yuvarlanmis toplam 119.66 + 71.80 + 39.89 + 47.86 + 31.91 + 39.89 = 351.01 -> fark -0.01 en buyuk satir (but): 119.65
    assert costs(q) == ["119.65", "71.80", "39.89", "47.86", "31.91", "39.89", "0.00"] and q["total_allocated"] == "351.00"
    v = cut("KZ-2026-003-VAL")
    # 351/316 orani: but 133.2911 ; kol 74.9763 ; sirt 49.9842 ; kaburga 39.9873 ; gerdan 22.2152 ; kiyma 30.5459
    # yuvarlanmis toplam 133.29 + 74.98 + 49.98 + 39.99 + 22.22 + 30.55 = 351.01 -> fark -0.01: but 133.28
    assert costs(v) == ["133.28", "74.98", "49.98", "39.99", "22.22", "30.55", "0.00"] and v["total_allocated"] == "351.00"


def test_kz004_yan_urun_elle():
    # ana parcalar 7.0 + 4.5 + 2.5 + 3.0 + 2.0 + 2.0 = 21.0 kg ; ciger 1.0 x 6.00 = 6.00 ; fire 3.0 ; toplam 25.0
    # havuz 287.50 - 6.00 = 281.50
    q = cut("KZ-2026-004-QTY")
    assert q["allocation_pool"] == "281.50" and q["by_product_value"] == "6.00" and q["closable"] is True
    # 281.50 x kg / 21: but 93.8333 ; kol 60.3214 ; sirt 33.5119 ; kaburga 40.2143 ; gerdan 26.8095 ; kiyma 26.8095
    # yuvarlanmis toplam 93.83 + 60.32 + 33.51 + 40.21 + 26.81 + 26.81 = 281.49 -> fark +0.01 but: 93.84
    assert costs(q) == ["93.84", "60.32", "33.51", "40.21", "26.81", "26.81", "6.00", "0.00"]
    v = cut("KZ-2026-004-VAL")
    # degerler 112 + 67.5 + 45 + 36 + 20 + 22 = 302.5 ; 281.50 x deger / 302.5:
    # but 104.2248 ; kol 62.8140 ; sirt 41.8760 ; kaburga 33.5008 ; gerdan 18.6116 ; kiyma 20.4727
    # yuvarlanmis toplam 104.22 + 62.81 + 41.88 + 33.50 + 18.61 + 20.47 = 281.49 -> fark +0.01 but: 104.23
    assert costs(v) == ["104.23", "62.81", "41.88", "33.50", "18.61", "20.47", "6.00", "0.00"]
    assert q["total_allocated"] == v["total_allocated"] == "287.50"


def test_kz005_esit_satir_ilk_satira_eklenir():
    e = cut("KZ-2026-005-QTY")
    # 100.00 / 3 = 33.3333 -> 33.33 x 3 = 99.99 ; fark +0.01 esitlikte ilk satira
    assert costs(e) == ["33.34", "33.33", "33.33", "0.00"]


def test_kz006_tolerans_siniri():
    e = cut("KZ-2026-006-QTY")
    # 10 - 9.95 = 0.05 (|diff| > 0.05 degil) -> uyari yok, closable
    assert e["closable"] is True and e["warnings"] == []
    # 100 x 5.00/9.95 = 50.2513 ; 100 x 4.95/9.95 = 49.7487
    assert costs(e) == ["50.25", "49.75"]


def test_tolerans_uzeri_kapatilamaz():
    # 10 - 9.94 = 0.06 > 0.05
    e = cutting_allocation.allocate(
        {
            "input_weight_kg": "10",
            "unit_cost_per_kg": "10.00",
            "declared_total_cost": None,
            "cost_basis": "computed",
            "method": "quantity",
            "parts": [
                {"name": "A", "kg": "5.00", "reference_price": "10", "is_waste": False, "is_by_product": False},
                {"name": "B", "kg": "4.94", "reference_price": "10", "is_waste": False, "is_by_product": False},
            ],
        }
    )
    assert e["closable"] is False and e["warnings"] == ["WEIGHT_IMBALANCE"]


# ---------------- Fraction ile bagimsiz ikinci uygulama
def _round_half_up(x: Fraction, places: int) -> Fraction:
    scale = 10**places
    return Fraction((x * scale + Fraction(1, 2)).__floor__(), scale)


def frac_distribute(total: Fraction, weights: list[Fraction]) -> list[Fraction]:
    s = sum(weights)
    r = [_round_half_up(total * w / s, 2) for w in weights]
    diff = _round_half_up(total, 2) - sum(r)
    idx = min(range(len(r)), key=lambda i: (-r[i], i))
    r[idx] += diff
    return r


@pytest.mark.parametrize("case", cli.cutting_cases(), ids=lambda c: c["case_id"])
def test_fraction_capraz_kontrol(case):
    inp = case["input"]
    parts = inp["parts"]
    if inp["cost_basis"] == "computed":
        total = _round_half_up(Fraction(inp["input_weight_kg"]) * Fraction(inp["unit_cost_per_kg"]), 2)
    else:
        total = Fraction(inp["declared_total_cost"])
    bp = {
        i: _round_half_up(Fraction(p["kg"]) * Fraction(p["reference_price"]), 2)
        for i, p in enumerate(parts)
        if p["is_by_product"]
    }
    pool = total - sum(bp.values())
    main = [i for i, p in enumerate(parts) if not p["is_waste"] and not p["is_by_product"]]
    w = [
        Fraction(parts[i]["kg"]) * (Fraction(parts[i]["reference_price"]) if inp["method"] == "value" else 1)
        for i in main
    ]
    alloc = dict(zip(main, frac_distribute(pool, w)))
    for i, p in enumerate(case["expected"]["parts"]):
        want = alloc.get(i, bp.get(i, Fraction(0)))
        assert Decimal(p["allocated_cost"]) == Decimal(want.numerator) / Decimal(want.denominator)
