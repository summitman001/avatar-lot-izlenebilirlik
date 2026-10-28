from decimal import Decimal

from app import anomaly


def test_verim_dusuk_tetiklenir():
    f = anomaly.check_verim_dusuk(Decimal("0.70"), Decimal("0.85"), residual_std=0.015, sigma_threshold=2.0)
    assert f is not None and f.code == "VERIM_DUSUK" and f.severity == "critical"


def test_verim_dusuk_tetiklenmez_esik_altinda():
    f = anomaly.check_verim_dusuk(Decimal("0.845"), Decimal("0.85"), residual_std=0.015, sigma_threshold=2.0)
    assert f is None


def test_verim_dusuk_gercek_yuksekse_tetiklenmez():
    f = anomaly.check_verim_dusuk(Decimal("0.95"), Decimal("0.85"), residual_std=0.015, sigma_threshold=2.0)
    assert f is None


def test_fire_yuksek_tetiklenir():
    f = anomaly.check_fire_yuksek(Decimal("0.20"), Decimal("0.11"), residual_std=0.003, sigma_threshold=2.0)
    assert f is not None and f.code == "FIRE_YUKSEK"


def test_fire_yuksek_tetiklenmez():
    f = anomaly.check_fire_yuksek(Decimal("0.115"), Decimal("0.11"), residual_std=0.003, sigma_threshold=2.0)
    assert f is None


def test_maliyet_artisi_tetiklenir():
    f = anomaly.check_maliyet_artisi(Decimal("15"), [Decimal("10"), Decimal("10"), Decimal("10")], threshold_pct=10.0)
    assert f is not None and f.code == "MALIYET_ARTISI" and f.evidence["increase_pct"] == "50.00"


def test_maliyet_artisi_tetiklenmez_esik_altinda():
    f = anomaly.check_maliyet_artisi(Decimal("10.05"), [Decimal("10"), Decimal("10")], threshold_pct=10.0)
    assert f is None


def test_maliyet_artisi_veri_yoksa_none():
    f = anomaly.check_maliyet_artisi(Decimal("15"), [], threshold_pct=10.0)
    assert f is None


def test_ekp_sapma_tetiklenir():
    f = anomaly.check_ekp_sapma(Decimal("11.2"), Decimal("10"), threshold_pct=5.0)
    assert f is not None and f.code == "EKP_SAPMA"


def test_ekp_sapma_tetiklenmez():
    f = anomaly.check_ekp_sapma(Decimal("10.3"), Decimal("10"), threshold_pct=5.0)
    assert f is None


def test_ekp_sapma_gercek_dahaazsa_tetiklenmez():
    f = anomaly.check_ekp_sapma(Decimal("9.0"), Decimal("10"), threshold_pct=5.0)
    assert f is None


def test_kutle_dengesi_tetiklenir_her_zaman_critical():
    f = anomaly.check_kutle_dengesi(Decimal("20"), [Decimal("10"), Decimal("15")])
    assert f is not None and f.code == "KUTLE_DENGESI" and f.severity == "critical"


def test_kutle_dengesi_tetiklenmez():
    f = anomaly.check_kutle_dengesi(Decimal("20"), [Decimal("10"), Decimal("9.99")])
    assert f is None


def test_kutle_dengesi_tam_esitlikte_tetiklenmez():
    f = anomaly.check_kutle_dengesi(Decimal("20"), [Decimal("10"), Decimal("10")])
    assert f is None


def test_severity_olcekleri():
    # z=2 -> info, z=3 -> warning, z=4+ -> critical
    assert anomaly._sigma_severity(2.0) == anomaly.INFO
    assert anomaly._sigma_severity(3.0) == anomaly.WARNING
    assert anomaly._sigma_severity(4.0) == anomaly.CRITICAL
    assert anomaly._sigma_severity(1.0) is None
