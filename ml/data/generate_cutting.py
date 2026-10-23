"""Sentetik kuzu parcalama verisi: python -m data.generate_cutting [--out DIR]

200 kuzu; irk, cinsiyet, karkas agirligi (14-28 kg). Parca oranlari Turk literaturu
etrafinda; irk ve karkas agirligina bagli kucuk sistematik etkiler + gurultu; oranlar
toplami 1'e normalize edilir. BU VERI SENTETIKTIR (bkz. docs/ml-data.md).
Parca kg degerleri 3 ondalik; dagitim yuvarlamasi ile parcalar + fire = karkas (birebir).
"""
from __future__ import annotations

import argparse
import csv
from decimal import ROUND_HALF_UP, Decimal
from pathlib import Path

import numpy as np

from oracle.decimal_utils import dstr
from oracle.rounding import distribute

SEED = 20260101
N_LAMBS = 200
PARTS = ["but", "kol", "sirt", "kaburga", "gerdan", "kiyma", "fire"]

BASE_RATIO = {"but": 0.31, "kol": 0.17, "sirt": 0.105, "kaburga": 0.12, "gerdan": 0.085, "kiyma": 0.10, "fire": 0.11}
BREEDS = {"Morkaraman": 0.40, "Akkaraman": 0.35, "Merinos": 0.25}
BREED_CARCASS_MEAN = {"Morkaraman": 22.0, "Akkaraman": 20.5, "Merinos": 19.0}
CARCASS_SD = 3.0
CARCASS_MIN, CARCASS_MAX = 14.0, 28.0
REF_CARCASS = 21.0

BREED_EFFECT = {
    "Morkaraman": {"but": 0.004, "kol": 0.002, "kiyma": -0.002, "fire": -0.003},
    "Akkaraman": {"kol": -0.001, "kaburga": 0.003, "fire": 0.003},
    "Merinos": {"but": 0.006, "kol": -0.004, "kaburga": -0.002, "gerdan": -0.002},
}
SEX_EFFECT = {"M": {"but": 0.002, "fire": -0.001}, "F": {"but": -0.002, "fire": 0.001}}
# karkas agirligi REF_CARCASS ustunde kg basina oran degisimi (yagli parcalar artar, but azalir)
WEIGHT_SLOPE = {"but": -0.0012, "kol": -0.0003, "sirt": 0.0, "kaburga": 0.0010, "gerdan": 0.0003, "kiyma": 0.0006, "fire": 0.0004}
NOISE_SIGMA = {"but": 0.006, "kol": 0.004, "sirt": 0.003, "kaburga": 0.003, "gerdan": 0.002, "kiyma": 0.003, "fire": 0.004}

PARAMS = {
    "seed": SEED,
    "n_lambs": N_LAMBS,
    "base_ratio": BASE_RATIO,
    "breed_probabilities": BREEDS,
    "carcass_mean_by_breed": BREED_CARCASS_MEAN,
    "carcass_sd": CARCASS_SD,
    "carcass_range": [CARCASS_MIN, CARCASS_MAX],
    "breed_effect": BREED_EFFECT,
    "sex_effect": SEX_EFFECT,
    "weight_slope_per_kg": WEIGHT_SLOPE,
    "noise_sigma": NOISE_SIGMA,
}

COLUMNS = ["lamb_id", "breed", "sex", "carcass_kg"] + [f"{p}_kg" for p in PARTS]


def generate() -> list[dict[str, str]]:
    rng = np.random.default_rng(SEED)
    breed_names = list(BREEDS)
    rows = []
    for i in range(N_LAMBS):
        breed = str(rng.choice(breed_names, p=list(BREEDS.values())))
        sex = str(rng.choice(["M", "F"]))
        carcass = float(np.clip(rng.normal(BREED_CARCASS_MEAN[breed], CARCASS_SD), CARCASS_MIN, CARCASS_MAX))
        carcass_d = Decimal(repr(round(carcass, 1)))
        ratios = []
        for p in PARTS:
            r = (
                BASE_RATIO[p]
                + BREED_EFFECT[breed].get(p, 0.0)
                + SEX_EFFECT[sex].get(p, 0.0)
                + WEIGHT_SLOPE[p] * (float(carcass_d) - REF_CARCASS)
                + rng.normal(0, NOISE_SIGMA[p])
            )
            ratios.append(max(r, 0.01))
        total = sum(ratios)
        weights = [Decimal(repr(round(r / total, 6))) for r in ratios]
        kgs = distribute(carcass_d, weights, 3)
        row = {"lamb_id": f"KZ-{i + 1:04d}", "breed": breed, "sex": sex, "carcass_kg": dstr(carcass_d)}
        row.update({f"{p}_kg": dstr(k) for p, k in zip(PARTS, kgs)})
        rows.append(row)
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
    path = args.out / "lamb_cutting.csv"
    write_csv(generate(), path)
    print(f"yazildi: {path}")
    return path


if __name__ == "__main__":
    main()
