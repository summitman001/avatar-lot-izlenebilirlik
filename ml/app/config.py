"""Anomali esikleri: sabit kodlanmaz, ortam degiskeniyle degistirilebilir (bkz. docs/anomaly-integration.md)."""
from __future__ import annotations

import os
from dataclasses import dataclass


def _float_env(name: str, default: float) -> float:
    raw = os.environ.get(name)
    return default if raw is None else float(raw)


@dataclass(frozen=True)
class AnomalyThresholds:
    yield_sigma: float = _float_env("ANOMALY_YIELD_SIGMA", 2.0)
    fire_sigma: float = _float_env("ANOMALY_FIRE_SIGMA", 2.0)
    cost_increase_pct: float = _float_env("ANOMALY_COST_INCREASE_PCT", 10.0)
    cost_lookback_purchases: int = int(_float_env("ANOMALY_COST_LOOKBACK_PURCHASES", 5))
    ekp_deviation_pct: float = _float_env("ANOMALY_EKP_DEVIATION_PCT", 5.0)


THRESHOLDS = AnomalyThresholds()


# Fatura/irsaliye OCR (bkz. docs/invoice-ocr.md). Harici API/internet GEREKTIRMEZ: Tesseract
# (pytesseract) imaj icinde kuruludur. Esikler sabit kodlanmaz, ortam degiskeniyle degisir.
@dataclass(frozen=True)
class OcrConfig:
    languages: str = os.environ.get("OCR_LANGUAGES", "tur+eng")
    # Belge/alan guveni bu esiklerin USTUNDEYSE yuksek/orta, aksi halde dusuk sayilir (web'de de
    # ayni esikler kullanilir, bkz. docs/invoice-ocr.md).
    confidence_high: float = _float_env("OCR_CONFIDENCE_HIGH", 0.80)
    confidence_medium: float = _float_env("OCR_CONFIDENCE_MEDIUM", 0.50)
    max_file_size_bytes: int = int(_float_env("OCR_MAX_FILE_SIZE_BYTES", 15_000_000))


OCR = OcrConfig()
