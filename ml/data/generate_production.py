"""Sentetik dis uretim emirleri: python -m data.generate_production [--out DIR]

60 emir, 2025-01 ile 2026-10 arasi. Hedef: teslim edilen adet (kg).
BU VERI SENTETIKTIR; gercek dunya performansini kanitlamaz (bkz. docs/ml-data.md).
EKP her emir icin oracle ile hesaplanir. Cikti ve maliyet hesaplari Decimal'dir.
"""
from __future__ import annotations

import argparse
import csv
from datetime import date
from decimal import ROUND_HALF_UP, Decimal
from pathlib import Path

import numpy as np

from oracle import production_cost
from oracle.decimal_utils import dstr, ratio4

SEED = 20250101
N_ORDERS = 60
START_YEAR, START_MONTH = 2025, 1
N_MONTHS = 22  # 2025-01 ... 2026-10

PARAMS = {
    "et_kg": {"dagilim": "Uniform tamsayi", "min": 60, "max": 150},
    "tedarikci_verim_etkisi": {"TED-A": "+0.02", "TED-B": "0.00", "TED-C": "-0.02"},
    "tedarikci_olasiliklari": {"TED-A": 0.35, "TED-B": 0.40, "TED-C": 0.25},
    "uretici_hizmet_fiyati": {"URT-1": "5.50", "URT-2": "5.80"},
    "uretici_olasiliklari": {"URT-1": 0.60, "URT-2": 0.40},
    "taban_verim_kg_per_kg_et": 0.85,
    "yaz_etkisi_haziran_agustos": -0.01,
    "verim_gurultu_sigma": 0.012,
    "baharat_orani_et": {"dagilim": "Uniform", "min": 0.017, "max": 0.023, "yuvarlama": "0.1 kg"},
    "bagirsak_orani_et": {"dagilim": "Uniform", "min": 0.040, "max": 0.060, "yuvarlama": "0.1 kg"},
    "et_fiyati": {"baslangic_eur_kg": 8.60, "aylik_enflasyon": 0.006, "gurultu_sigma": 0.015},
    "baharat_fiyati": {"baslangic_eur_kg": 23.20, "aylik_enflasyon": 0.004, "gurultu_sigma": 0.010},
    "bagirsak_fiyati": {"baslangic_eur_kg": 11.20, "aylik_enflasyon": 0.004, "gurultu_sigma": 0.010},
    "teslim_adet": "yuvarlanmis tamsayi: et_kg x gercek_verim",
}

SUPPLIERS = {"TED-A": 0.02, "TED-B": 0.0, "TED-C": -0.02}
SUPPLIER_P = [0.35, 0.40, 0.25]
PRODUCERS = {"URT-1": Decimal("5.50"), "URT-2": Decimal("5.80")}
PRODUCER_P = [0.60, 0.40]

COLUMNS = [
    "order_id", "order_date", "year", "month", "month_index", "is_summer", "supplier_id", "producer_id",
    "et_kg", "spice_kg", "casing_kg", "meat_unit_price", "spice_unit_price", "casing_unit_price",
    "service_unit_price", "delivered_qty", "yield_ratio", "material_cost", "service_cost", "total_cost",
    "ekp_unit", "ekp_display",
]


def _dec(x: float, places: int) -> Decimal:
    return Decimal(repr(float(x))).quantize(Decimal(1).scaleb(-places), rounding=ROUND_HALF_UP)


def generate() -> list[dict[str, str]]:
    rng = np.random.default_rng(SEED)
    month_idx = np.sort(rng.integers(0, N_MONTHS, N_ORDERS))
    day = rng.integers(1, 29, N_ORDERS)
    et_kg = rng.integers(60, 151, N_ORDERS)
    supplier = rng.choice(list(SUPPLIERS), size=N_ORDERS, p=SUPPLIER_P)
    producer = rng.choice(list(PRODUCERS), size=N_ORDERS, p=PRODUCER_P)
    spice_ratio = rng.uniform(0.017, 0.023, N_ORDERS)
    casing_ratio = rng.uniform(0.040, 0.060, N_ORDERS)
    meat_noise = rng.normal(0, 0.015, N_ORDERS)
    spice_noise = rng.normal(0, 0.010, N_ORDERS)
    casing_noise = rng.normal(0, 0.010, N_ORDERS)
    yield_noise = rng.normal(0, 0.012, N_ORDERS)

    rows = []
    for i in range(N_ORDERS):
        m = int(month_idx[i])
        year = START_YEAR + (START_MONTH - 1 + m) // 12
        month = (START_MONTH - 1 + m) % 12 + 1
        d = date(year, month, int(day[i]))
        is_summer = 1 if month in (6, 7, 8) else 0
        true_yield = 0.85 + SUPPLIERS[str(supplier[i])] - 0.01 * is_summer + float(yield_noise[i])
        et = Decimal(int(et_kg[i]))
        delivered = (et * Decimal(repr(true_yield))).quantize(Decimal(1), rounding=ROUND_HALF_UP)
        spice_kg = max(_dec(float(et) * spice_ratio[i], 1), Decimal("0.5"))
        casing_kg = max(_dec(float(et) * casing_ratio[i], 1), Decimal("0.5"))
        meat_price = _dec(8.60 * (1 + 0.006 * m) * (1 + meat_noise[i]), 2)
        spice_price = _dec(23.20 * (1 + 0.004 * m) * (1 + spice_noise[i]), 2)
        casing_price = _dec(11.20 * (1 + 0.004 * m) * (1 + casing_noise[i]), 2)
        service_price = PRODUCERS[str(producer[i])]

        lots = [
            production_cost.Lot("Dana eti", "meat", et, meat_price),
            production_cost.Lot("Baharat", "spice", spice_kg, spice_price),
            production_cost.Lot("Bagirsak", "casing", casing_kg, casing_price),
        ]
        res = production_cost.calculate_production_cost(lots, delivered, service_price)
        rows.append(
            {
                "order_id": f"UE-{i + 1:03d}",
                "order_date": d.isoformat(),
                "year": str(year),
                "month": str(month),
                "month_index": str(m),
                "is_summer": str(is_summer),
                "supplier_id": str(supplier[i]),
                "producer_id": str(producer[i]),
                "et_kg": dstr(et),
                "spice_kg": dstr(spice_kg),
                "casing_kg": dstr(casing_kg),
                "meat_unit_price": dstr(meat_price),
                "spice_unit_price": dstr(spice_price),
                "casing_unit_price": dstr(casing_price),
                "service_unit_price": dstr(service_price),
                "delivered_qty": dstr(delivered),
                "yield_ratio": dstr(ratio4(delivered / et)),
                "material_cost": dstr(res.material_cost),
                "service_cost": dstr(res.service_cost),
                "total_cost": dstr(res.total_cost),
                "ekp_unit": dstr(res.ekp_unit),
                "ekp_display": dstr(res.ekp_display),
            }
        )
    rows.sort(key=lambda r: (r["order_date"], r["order_id"]))
    for i, r in enumerate(rows):  # tarih sirasina gore yeniden numarala
        r["order_id"] = f"UE-{i + 1:03d}"
    return rows


def write_csv(rows: list[dict[str, str]], path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=COLUMNS, lineterminator="\n")
        writer.writeheader()
        writer.writerows(rows)


DEFAULT_OUT = Path(__file__).resolve().parent / "out"


def main(argv: list[str] | None = None) -> Path:
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", type=Path, default=DEFAULT_OUT)
    args = parser.parse_args(argv)
    path = args.out / "production_orders.csv"
    write_csv(generate(), path)
    print(f"yazildi: {path}")
    return path


if __name__ == "__main__":
    main()
