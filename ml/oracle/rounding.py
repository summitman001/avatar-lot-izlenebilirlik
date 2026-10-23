"""Dagitim yuvarlamasi: toplam kaynaga birebir esit olur."""
from __future__ import annotations

from decimal import Decimal

from .decimal_utils import quantize_places


def distribute(total: Decimal, weights: list[Decimal], places: int = 2) -> list[Decimal]:
    """`total` tutarini `weights` oraninda dagitir.

    1. Her satir tam hassasiyetle hesaplanir: total * w_i / sum(w).
    2. Her satir `places` ondaliga ROUND_HALF_UP ile yuvarlanir.
    3. Kalan fark (total - yuvarlanmis toplam) en buyuk tutarli satira eklenir.
       Esitlikte girdi sirasindaki ilk satir secilir.

    Sonuc: sum(sonuc) == quantize(total) her zaman.
    """
    if not weights:
        raise ValueError("dagitim icin en az bir satir gerekli")
    if any(w < 0 for w in weights):
        raise ValueError("agirlik negatif olamaz")
    denom = sum(weights, Decimal(0))
    if denom <= 0:
        raise ValueError("agirliklarin toplami sifirdan buyuk olmali")
    target = quantize_places(total, places)
    rounded = [quantize_places(total * w / denom, places) for w in weights]
    diff = target - sum(rounded, Decimal(0))
    if diff != 0:
        idx = max(range(len(rounded)), key=lambda i: (rounded[i], -i))
        rounded[idx] += diff
    return rounded
