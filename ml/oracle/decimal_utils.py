"""Decimal yardimcilari: yuvarlama kurallari Go ve Android ile birebir aynidir.

Kurallar:
- birim maliyet: 4 ondalik, ROUND_HALF_UP
- para toplami: 2 ondalik, ROUND_HALF_UP
- ara degerler yuvarlanmaz (tam hassasiyetle tasinir, sadece cikti aninda yuvarlanir)
- float yasak: float girdi TypeError verir
"""
from __future__ import annotations

from decimal import ROUND_HALF_UP, Decimal
from typing import Any

_Q2 = Decimal("0.01")
_Q3 = Decimal("0.001")
_Q4 = Decimal("0.0001")


def D(value: Any) -> Decimal:
    """str, int veya Decimal'i Decimal'e cevirir; float'i reddeder."""
    if isinstance(value, bool) or isinstance(value, float):
        raise TypeError(f"float/bool yasak, string kullanin: {value!r}")
    if isinstance(value, Decimal):
        return value
    if isinstance(value, (str, int)):
        return Decimal(value)
    raise TypeError(f"Decimal'e cevrilemez: {value!r}")


def money(x: Decimal) -> Decimal:
    """Para toplami: 2 ondalik."""
    return x.quantize(_Q2, rounding=ROUND_HALF_UP)


def qty3(x: Decimal) -> Decimal:
    """Miktar (kg): 3 ondalik."""
    return x.quantize(_Q3, rounding=ROUND_HALF_UP)


def unit4(x: Decimal) -> Decimal:
    """Birim maliyet: 4 ondalik."""
    return x.quantize(_Q4, rounding=ROUND_HALF_UP)


def pct2(x: Decimal) -> Decimal:
    """Yuzde: 2 ondalik."""
    return x.quantize(_Q2, rounding=ROUND_HALF_UP)


def ratio4(x: Decimal) -> Decimal:
    """Oran (kg/kg): 4 ondalik."""
    return x.quantize(_Q4, rounding=ROUND_HALF_UP)


def quantize_places(x: Decimal, places: int) -> Decimal:
    return x.quantize(Decimal(1).scaleb(-places), rounding=ROUND_HALF_UP)


def dstr(x: Decimal) -> str:
    """JSON icin sabit nokta string (us gosterimi yok)."""
    return format(x, "f")
