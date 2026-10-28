"""Anomali tespiti: ML kalintilari (CV protokolunden) + basit esik kurallari.

Bes kural (hepsi best-effort, girdi eksikse o kural atlanir):
- VERIM_DUSUK: gercek verim, aktif production modelinin CV kalinti std'sine (sigma)
  gore tahmininden asagida.
- FIRE_YUKSEK: parcalama firesi, aktif cutting modelinin CV kalinti std'sine gore
  o irk/agirlik icin beklenenin ustunde.
- MALIYET_ARTISI: hammadde birim maliyeti, son N alisin ortalamasindan %X fazla.
- EKP_SAPMA: gercek EKP, tahmin edilenden %X fazla.
- KUTLE_DENGESI: parcalama ciktilari (parcalar + fire) girisi asiyor (fiziksel
  imkansizlik), her zaman critical.

Esikler `app/config.py` icindedir (ortam degiskeniyle degistirilebilir, sabit
kodlanmamistir). Go entegrasyonu bu iste YAPILMADI; sozlesme docs/anomaly-integration.md'de.
"""
from __future__ import annotations

from dataclasses import dataclass
from decimal import Decimal
from typing import Any

from .config import AnomalyThresholds

INFO, WARNING, CRITICAL = "info", "warning", "critical"


@dataclass(frozen=True)
class AnomalyFinding:
    code: str
    severity: str
    message: str
    evidence: dict[str, str]

    def to_json(self) -> dict[str, Any]:
        return {"code": self.code, "severity": self.severity, "message": self.message, "evidence": self.evidence}


def _sigma_severity(z: float) -> str | None:
    if z >= 4:
        return CRITICAL
    if z >= 3:
        return WARNING
    if z >= 2:
        return INFO
    return None


def _pct_severity(ratio_over_threshold: float) -> str | None:
    """ratio_over_threshold = gercek_asim_yuzdesi / esik_yuzdesi (1.0 = esikte)."""
    if ratio_over_threshold < 1.0:
        return None
    if ratio_over_threshold >= 2.0:
        return CRITICAL
    if ratio_over_threshold >= 1.5:
        return WARNING
    return INFO


def check_verim_dusuk(
    actual_yield_ratio: Decimal, predicted_yield_ratio: Decimal, residual_std: float, sigma_threshold: float
) -> AnomalyFinding | None:
    if residual_std <= 0:
        return None
    deviation = float(predicted_yield_ratio - actual_yield_ratio)  # pozitif = gercek dusuk
    z = deviation / residual_std
    if z < sigma_threshold:
        return None
    severity = _sigma_severity(z) or INFO
    return AnomalyFinding(
        code="VERIM_DUSUK",
        severity=severity,
        message=f"Gercek verim ({actual_yield_ratio}) model tahmininden ({predicted_yield_ratio}) {z:.2f} sigma asagida.",
        evidence={
            "actual_yield_ratio": str(actual_yield_ratio),
            "predicted_yield_ratio": str(predicted_yield_ratio),
            "residual_std": f"{residual_std:.6f}",
            "z_score": f"{z:.4f}",
            "threshold_sigma": str(sigma_threshold),
        },
    )


def check_fire_yuksek(
    actual_fire_ratio: Decimal, predicted_fire_ratio: Decimal, residual_std: float, sigma_threshold: float
) -> AnomalyFinding | None:
    if residual_std <= 0:
        return None
    deviation = float(actual_fire_ratio - predicted_fire_ratio)  # pozitif = fire fazla
    z = deviation / residual_std
    if z < sigma_threshold:
        return None
    severity = _sigma_severity(z) or INFO
    return AnomalyFinding(
        code="FIRE_YUKSEK",
        severity=severity,
        message=f"Parcalama firesi ({actual_fire_ratio}) beklenenden ({predicted_fire_ratio}) {z:.2f} sigma yuksek.",
        evidence={
            "actual_fire_ratio": str(actual_fire_ratio),
            "predicted_fire_ratio": str(predicted_fire_ratio),
            "residual_std": f"{residual_std:.6f}",
            "z_score": f"{z:.4f}",
            "threshold_sigma": str(sigma_threshold),
        },
    )


def check_maliyet_artisi(
    current_unit_cost: Decimal, recent_unit_costs: list[Decimal], threshold_pct: float
) -> AnomalyFinding | None:
    if not recent_unit_costs:
        return None
    recent_mean = sum(recent_unit_costs, Decimal(0)) / len(recent_unit_costs)
    if recent_mean <= 0:
        return None
    increase_pct = float((current_unit_cost - recent_mean) / recent_mean * 100)
    if increase_pct <= 0:
        return None
    severity = _pct_severity(increase_pct / threshold_pct)
    if severity is None:
        return None
    return AnomalyFinding(
        code="MALIYET_ARTISI",
        severity=severity,
        message=f"Hammadde birim maliyeti ({current_unit_cost}) son {len(recent_unit_costs)} alisin ortalamasindan (%{increase_pct:.2f}) daha fazla artti.",
        evidence={
            "current_unit_cost": str(current_unit_cost),
            "recent_mean_unit_cost": str(recent_mean.quantize(Decimal("0.0001"))),
            "increase_pct": f"{increase_pct:.2f}",
            "threshold_pct": str(threshold_pct),
            "n_recent_purchases": str(len(recent_unit_costs)),
        },
    )


def check_ekp_sapma(
    actual_ekp_unit: Decimal, predicted_ekp_unit: Decimal, threshold_pct: float
) -> AnomalyFinding | None:
    if predicted_ekp_unit <= 0:
        return None
    deviation_pct = float((actual_ekp_unit - predicted_ekp_unit) / predicted_ekp_unit * 100)
    if deviation_pct <= 0:
        return None
    severity = _pct_severity(deviation_pct / threshold_pct)
    if severity is None:
        return None
    return AnomalyFinding(
        code="EKP_SAPMA",
        severity=severity,
        message=f"Gercek EKP ({actual_ekp_unit}) tahmin edilenden ({predicted_ekp_unit}) %{deviation_pct:.2f} fazla.",
        evidence={
            "actual_ekp_unit": str(actual_ekp_unit),
            "predicted_ekp_unit": str(predicted_ekp_unit),
            "deviation_pct": f"{deviation_pct:.2f}",
            "threshold_pct": str(threshold_pct),
        },
    )


def check_kutle_dengesi(input_weight_kg: Decimal, output_weights_kg: list[Decimal]) -> AnomalyFinding | None:
    total_output = sum(output_weights_kg, Decimal(0))
    epsilon = Decimal("0.001")
    if total_output <= input_weight_kg + epsilon:
        return None
    diff = total_output - input_weight_kg
    return AnomalyFinding(
        code="KUTLE_DENGESI",
        severity=CRITICAL,
        message=f"Parcalama ciktilari toplami ({total_output} kg) girisi ({input_weight_kg} kg) asiyor; fiziksel olarak imkansiz.",
        evidence={
            "input_weight_kg": str(input_weight_kg),
            "total_output_kg": str(total_output),
            "difference_kg": str(diff),
        },
    )


def residual_std_from_cv_folds(fold_metrics: list[dict[str, Any]], residual_key: str) -> float:
    """cv_folds.json icindeki bir modelin katmanlarindaki kalintilari havuzlayip std hesaplar."""
    import numpy as np

    pooled: list[float] = []
    for fold in fold_metrics:
        pooled.extend(fold.get(residual_key, []))
    return float(np.std(pooled, ddof=1)) if len(pooled) > 1 else 0.0
