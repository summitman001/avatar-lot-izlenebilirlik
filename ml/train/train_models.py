"""Nihai modelleri TUM veriyle egitip diske kaydeder: python -m train.train_models

Baseline haric tum kayitli modeller icin calisir (baseline anlik hesaplanir, kaydedilmez).
Sonuc ml/models/*.joblib (commitlenir) -- servis/Docker imaji bu dosyalari yukler,
yeniden egitim gerekmez. CV ile degerlendirme (train.evaluate) bundan AYRIDIR: o,
dis dunyaya gosterilecek metrikler icin katman katman egitir/degerlendirir; bu script
sadece servise konacak NIHAI modeli (tum veriyle) uretir.
"""
from __future__ import annotations

import argparse
import json
from datetime import datetime, timezone
from pathlib import Path

from app import datasets, model_store, registry
from app.models.base import CUTTING, PRODUCTION


def train_and_save(data_dir: Path | None = None) -> dict[str, dict[str, object]]:
    full = {PRODUCTION: datasets.load_production(data_dir), CUTTING: datasets.load_cutting(data_dir)}
    meta: dict[str, dict[str, object]] = {}
    for task in (PRODUCTION, CUTTING):
        for spec in registry.specs(task):
            if spec.info.baseline:
                continue
            model = spec.factory().fit(full[task])
            path = model_store.save(task, spec.info.name, model)
            meta[f"{task}__{spec.info.name}"] = {
                "task": task,
                "name": spec.info.name,
                "version": spec.info.version,
                "n_train": len(full[task]),
                "trained_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
                "path": path.name,
            }
    return meta


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--data-dir", type=Path, default=None)
    args = parser.parse_args(argv)
    meta = train_and_save(args.data_dir)
    model_store.MODELS_DIR.mkdir(parents=True, exist_ok=True)
    meta_path = model_store.MODELS_DIR / "metadata.json"
    meta_path.write_text(json.dumps(meta, indent=2, ensure_ascii=False) + "\n", encoding="utf-8", newline="\n")
    for key, m in meta.items():
        print(f"egitildi: {key} -> {m['path']} (n_train={m['n_train']})")
    print(f"yazildi: {meta_path}")


if __name__ == "__main__":
    main()
