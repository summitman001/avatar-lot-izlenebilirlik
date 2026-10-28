# Anomali Tespiti Entegrasyonu (ML -> Go)

> **Durum:** ML servisi tarafı (`ml/app/anomaly.py`, `POST /detect/anomaly`) ve **Go backend
> entegrasyonu** (bu dosyanın tanımladığı sözleşme + `anomaly_alert` tablosuna yazma, çağrı
> noktaları, GET/PATCH uç noktaları, web sayfası) ikisi de **tamamlandı**. Go tarafının gerçek
> davranışı aşağıdaki "Go entegrasyonu" bölümündedir; uygulama `backend/internal/repo/anomaly.go`'dadır.

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

## Go entegrasyonu

Uygulama: `backend/internal/repo/anomaly.go` (+ `mlclient.go`'da `DetectAnomaly`). **İlke: anomali
uyarıları SADECE BİLGİLENDİRMEDİR.** Bu dosyadaki hiçbir kod bir lotu bloke etmez, bir işlemi
reddetmez veya başka bir transaction'ı geri aldırmaz (bkz. `docs/assumptions.md`). Lot
izlenebilirliği, geri çağırma ve stok kararları kesin kayıt ilişkilerine dayanır, anomaliye değil.

### 1. Tetikleme noktaları (ML, en iyi gayret)

Her biri kendi işleminin transaction'ı **COMMIT olduktan SONRA** (ayrı bir best-effort fonksiyonla)
çağrılır; ML çağrısı başarısız olursa (kapalı/zaman aşımı) yalnızca loglanır, asıl işlem bozulmaz:

| Kural | Çağrı noktası | Kaynak veri |
|---|---|---|
| `VERIM_DUSUK` | `ReceiveProduction` sonrası (`detectProductionAnomaly`) | `et_kg` = tüketilen et miktarı (`production_input.qty_consumed`, kategori=`meat`); `actual_yield_ratio = (received_qty × unit_weight_kg) / et_kg`; opsiyonel `supplier_id` yalnızca TÜM sevk edilen hammaddeler AYNI tedarikçidense doldurulur (`singleSupplierOf`) |
| `EKP_SAPMA` | `ReceiveProduction` sonrası (aynı fonksiyon) | `predicted_ekp_unit` = en güncel `prediction` satırının `predicted_ekp_unit`'i (materials_sent aşaması, bkz. madde 12); `actual_ekp_unit` = `production_order.ekp_unit`. İkisi de yoksa ML hiç çağrılmaz |
| `FIRE_YUKSEK` | `CloseCuttingOrder` sonrası (`detectCuttingAnomaly`) | `actual_fire_kg` = reçetede `is_waste=true` olan parçanın fiili ağırlığı; `breed` = `cutting_recipe.animal_type` (sistem ırk değil hayvan türü tutar, bilinçli yaklaşım) |
| `KUTLE_DENGESI` (ML) | `CloseCuttingOrder` sonrası (aynı fonksiyon) | `input_weight_kg` + `output_weights_kg` = tüm `cutting_output.actual_weight_kg` (fire dahil) |
| `MALIYET_ARTISI` | `CreatePurchaseLot` (`POST /lots`) sonrası (`detectPurchaseCostAnomaly`) | `recent_unit_costs` = aynı `item_id` için bu satır HARİÇ son 5 `purchase_receipt_line.unit_price` (`ORDER BY created_at DESC LIMIT 5`) |

Bulgular `applyAnomalyFindings` ile `anomaly_alert`'e yazılır (bkz. madde 3, mükerrer önleme).

### 2. Kural tabanlı ek kontrol (ML'den BAĞIMSIZ, her zaman çalışır)

İki kontrol ML servisinin açık/kapalı olmasından TAMAMEN bağımsızdır:

- **`KUTLE_DENGESİ` (deterministik hali):** `UpdateCuttingOutputs` (`PUT .../outputs`) her çağrıldığında,
  AYNI transaction'da `checkMassBalanceRule` çalışır: `balance_diff_kg` negatifse (çıktılar — fire
  dahil — girişi aşıyor, fiziksel olarak imkansız) `critical` uyarı yazılır. Bu durum zaten
  `closable=false` ile kapatmayı engelliyordu; bu kontrol aynı durumu AYRICA `anomaly_alert`'e
  kaydeder — iki ayrı mekanizma (ML'in kendi `KUTLE_DENGESI` kuralı ile bu) **aynı `alert_type`'ı**
  paylaşır, kayıtlar `upsertAnomalyAlert` ile birleşir (mükerrer olmaz).
- **`NEGATIVE_STOCK_OFFLINE`** (`backend/internal/repo/sales.go`): bir lotun bir depodaki bakiyesi
  offline satış senaryosunda negatife düşerse. Bu, görev tanımındaki "LOT_HAREKET_TUTARSIZ" kavramıyla
  **kavramsal olarak aynıdır** (bir lotun hareketi tutarsız/imkansız bir bakiye üretiyor); **iki ayrı
  sistem olmasın diye** AYRI bir `LOT_HAREKET_TUTARSIZ` alert_type'ı eklenmedi, mevcut
  `NEGATIVE_STOCK_OFFLINE` kodu `upsertAnomalyAlert` üzerinden yeniden kullanıldı (bkz.
  `docs/assumptions.md`, "Satış/sevkiyat/iade" bölümü).

### 3. Mükerrer önleme (`upsertAnomalyAlert`)

Aynı `ref_type` + `ref_id` + `alert_type` için zaten AÇIK (`acknowledged=false`) bir uyarı varsa
yenisi eklenmez, mevcut satır (`severity`/`message`/`evidence_json`/`updated_at`) güncellenir. Böylece
aynı siparişe/emre tekrar tetiklenen aynı kural kayıt yığmaz (örnek: `PUT .../outputs` draft iken
birden çok kez çağrılabilir).

### 4. API ve web

- `GET /api/v1/anomaly-alerts` (filtreler: `severity`, `acknowledged`, `ref_type`, `from`/`to`,
  sayfalama) — her kayıtta `ref_no` (ilgili sipariş/belge numarası, SQL `CASE` ile çözülür).
- `PATCH /api/v1/anomaly-alerts/{id}` — `acknowledged`/`note` günceller; SADECE uyarının kendisini
  etkiler, hiçbir lotu/siparişi değiştirmez.
- `GET /api/v1/anomaly-alerts/summary` — severity bazında AÇIK uyarı sayısı (web dashboard kartı).
- `GET /production-orders/{id}` ve `GET /cutting-orders/{id}` yanıtlarına `anomaly_alerts` dizisi
  gömülüdür (`loadAnomalyAlerts`).
- Web: sol menüde "Uyarılar" sayfası (liste + filtre + okundu işaretleme), üst barda açık kritik
  uyarı rozeti, Dış Üretim/Parçalama detay sayfalarında uyarı kartı, Dashboard'da severity bazlı özet.

## Sınırlılıklar

- `VERIM_DUSUK`/`FIRE_YUKSEK` sigma eşikleri **sentetik veri** üzerinden hesaplanan CV
  kalıntı standart sapmalarıdır (`reports/model_comparison.json`, `residual_std_yield_ratio`
  / `residual_std_fire_ratio`); gerçek veriyle yeniden hesaplanmalıdır (bkz.
  `docs/ml-report.md` SINIRLILIKLAR).
- `MALIYET_ARTISI` için geçmiş fiyat listesini ML servisi SAKLAMAZ (veritabanı yok);
  her çağrıda Go'nun göndermesi gerekir.
- `FIRE_YUKSEK` için gönderilen `breed`, sistemin `cutting_recipe.animal_type`'ıdır (örn. `lamb`) —
  gerçek ırk (örn. "Akkaraman") hiçbir yerde tutulmaz; ML bu alanı tanımadığı değerde `breed_used`'ı
  kendi varsayılanına düşürür (bkz. `ml/app/main.py`), sonucu etkilemez ama ırka özgü bir kalibrasyon
  sağlamaz.
- `sex` (FIRE_YUKSEK opsiyonel alan) Go tarafından hiç gönderilmez: sistemde hayvan cinsiyeti
  tutulmaz.
