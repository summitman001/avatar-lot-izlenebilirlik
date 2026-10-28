# Anomali Tespiti Entegrasyonu (ML -> Go)

> **Kapsam notu:** Bu dosya yalnızca **sözleşmeyi** tanımlar. Go backend entegrasyonu
> (`anomaly_alert` tablosuna yazma, hangi adımlardan sonra çağrılacağı, cron/poll vb.)
> bu işte **yapılmadı** — bir sonraki iş kalemidir (bkz. `docs/HANDOFF.md`, "Sıradaki
> 5 iş"). `ml/app/anomaly.py` ve `POST /detect/anomaly` zaten servis içinde çalışır
> durumdadır ve `ml/tests/test_anomaly.py` + `ml/tests/test_api.py` ile test edilmiştir.

## Genel ilkeler

- **Best-effort, tek uç nokta, beş kural.** `POST /detect/anomaly` tek bir gövde alır;
  gövdedeki alanlardan hangileri **doluysa** o kural çalışır, diğerleri atlanır. Go
  backend elinde olan veriyi gönderir, eksik alan hata DÖNDÜRMEZ.
- **ML "en iyi gayret"tir** (dış üretim/parçalama'daki ML entegrasyonuyla aynı ilke,
  bkz. `docs/assumptions.md` madde 13): `ML_URL` erişilemezse/zaman aşımına uğrarsa
  anomali kontrolü atlanır, çağıran işlem (satış, teslim, kapanış) **başarısız olmaz**.
- **Eşikler sabit kodlanmamıştır** (`ml/app/config.py`, ortam değişkeniyle değişir):
  `ANOMALY_YIELD_SIGMA` (varsayılan 2.0), `ANOMALY_FIRE_SIGMA` (2.0),
  `ANOMALY_COST_INCREASE_PCT` (10.0), `ANOMALY_EKP_DEVIATION_PCT` (5.0).
- **Severity ölçeği:** sigma-tabanlı kurallarda (VERIM_DUSUK, FIRE_YUKSEK) z-skoru
  2-3 arası `info`, 3-4 arası `warning`, 4+ `critical`; yüzde-tabanlı kurallarda
  (MALIYET_ARTISI, EKP_SAPMA) eşiğin 1-1.5 katı `info`, 1.5-2 katı `warning`, 2 katı
  ve üstü `critical`. KUTLE_DENGESI her zaman `critical` (fiziksel imkansızlık).

## İstek (`POST /detect/anomaly`)

```json
{
  "reference_id": "URT-2026-045",
  "actual_yield_ratio": "0.78",
  "et_kg": "100",
  "supplier_id": "TED-A",
  "producer_id": "URT-1",

  "actual_fire_kg": "3.2",
  "carcass_kg": "22",
  "breed": "Akkaraman",
  "sex": "F",

  "current_unit_cost": "9.80",
  "recent_unit_costs": ["8.60", "8.70", "8.55", "8.80", "8.65"],

  "predicted_ekp_unit": "18.1588",
  "actual_ekp_unit": "19.40",

  "input_weight_kg": "25",
  "output_weights_kg": ["8.0", "4.0", "2.5", "3.0", "2.0", "2.5", "3.0"]
}
```

Tüm alanlar **opsiyoneldir**; `reference_id` yalnızca yanıtta aynen geri döner (log/eşleme
için, iş mantığına girmez). Her kural grubu için gereken alanlar:

| Kural | Gereken alanlar |
|---|---|
| `VERIM_DUSUK` | `actual_yield_ratio`, `et_kg` (+ opsiyonel `supplier_id`, `producer_id`) |
| `FIRE_YUKSEK` | `actual_fire_kg`, `carcass_kg` (+ opsiyonel `breed`, `sex`) |
| `MALIYET_ARTISI` | `current_unit_cost`, `recent_unit_costs` (boş olmayan liste) |
| `EKP_SAPMA` | `predicted_ekp_unit`, `actual_ekp_unit` |
| `KUTLE_DENGESI` | `input_weight_kg`, `output_weights_kg` |

`VERIM_DUSUK`/`FIRE_YUKSEK` için **beklenen değer ML servisinin kendi aktif modeliyle
hesaplanır** (Go bu değeri göndermez, yalnızca gerçekleşeni ve girdileri gönderir) —
tıpkı `/predict/production` ve `/predict/cutting` gibi, aktif model `GET /models`'da
`active: true` olandır.

## Yanıt

```json
{
  "reference_id": "URT-2026-045",
  "findings": [
    {
      "code": "EKP_SAPMA",
      "severity": "warning",
      "message": "Gercek EKP (19.40) tahmin edilenden (18.1588) %6.84 fazla.",
      "evidence": {
        "actual_ekp_unit": "19.40",
        "predicted_ekp_unit": "18.1588",
        "deviation_pct": "6.84",
        "threshold_pct": "5.0"
      }
    }
  ]
}
```

- `findings` boş liste olabilir (hiçbir kural tetiklenmedi veya hiç girdi verilmedi) —
  bu **hata değildir**, `200` döner.
- Her `finding.evidence` sözlüğü yalnızca string (Decimal-güvenli) değerler içerir; Go
  tarafı bunları doğrudan `anomaly_alert.evidence_json` gibi bir JSON alanına yazabilir.
- `code` sabit bir İngilizce kümedir (Türkçe `message`'a göre DALLANMAYIN, tıpkı backend'in
  kendi `code` kuralı gibi, bkz. `docs/HANDOFF.md` madde 13):
  `VERIM_DUSUK`, `FIRE_YUKSEK`, `MALIYET_ARTISI`, `EKP_SAPMA`, `KUTLE_DENGESI`.

## Önerilen Go entegrasyon noktaları (yapılmadı, öneri)

| Kural | Önerilen çağrı noktası | Kaynak veri |
|---|---|---|
| `VERIM_DUSUK` | `receive` (dış üretim teslim alındığında) | `actual_yield_ratio = output_kg/et_kg` (zaten hesaplı, bkz. `production_cost.go`) |
| `FIRE_YUKSEK` | `close` (parçalama kapanışında) | `actual_fire_kg` = fire çıktısının kg'ı, `cutting_order`'dan |
| `MALIYET_ARTISI` | `POST /lots` (alış lotu oluşturulduğunda) | `recent_unit_costs` = aynı hammadde için son 5 `purchase_receipt_line.unit_cost` (SQL: `ORDER BY created_at DESC LIMIT 5`) |
| `EKP_SAPMA` | `receive` (dış üretim) | `predicted_ekp_unit` zaten `prediction.materials_sent` satırında var (bkz. `docs/assumptions.md` madde 12); `actual_ekp_unit` aynı `receive` çağrısında hesaplanıyor |
| `KUTLE_DENGESI` | `PUT .../outputs` veya `close` (parçalama) | `input_weight_kg` = `cutting_order.input_weight_kg`; `output_weights_kg` = tüm `cutting_output.actual_kg` + fire |

Bulgular `anomaly_alert` tablosuna (zaten var, `NEGATIVE_STOCK_OFFLINE` için kullanılıyor,
bkz. `docs/HANDOFF.md` "Stub/TODO") `code`/`severity`/`message`/`evidence_json` olarak
yazılabilir; okuma uç noktası/web sayfası da henüz yok (aynı TODO).

## Sınırlılıklar

- `VERIM_DUSUK`/`FIRE_YUKSEK` sigma eşikleri **sentetik veri** üzerinden hesaplanan CV
  kalıntı standart sapmalarıdır (`reports/model_comparison.json`, `residual_std_yield_ratio`
  / `residual_std_fire_ratio`); gerçek veriyle yeniden hesaplanmalıdır (bkz.
  `docs/ml-report.md` SINIRLILIKLAR).
- `MALIYET_ARTISI` için geçmiş fiyat listesini ML servisi SAKLAMAZ (veritabanı yok);
  her çağrıda Go'nun göndermesi gerekir.
