// Kullaniciya gorunen metinler. API alan adlari ve kodlari (lines[0], INSUFFICIENT_STOCK, VERIM_DUSUK)
// sozlesmenin parcasidir, Android ve testler bunlara dayanir; bu yuzden API'de degil, burada cevrilir.

const ROW_LABEL: Record<string, string> = {
  inputs: 'girdi satırı', lines: 'satır', returns: 'iade satırı', outputs: 'çıktı satırı',
}

const FIELD_LABEL: Record<string, string> = {
  qty: 'miktar', unit_price: 'birim fiyat', declared_line_total: 'beyan edilen tutar', lot_id: 'lot', input_lot_id: 'giriş lotu',
  lot_no: 'lot no', output_lot_no: 'çıktı lot no', item_id: 'ürün', product_item_id: 'ürün', part_item_id: 'parça',
  warehouse_id: 'depo', external_warehouse_id: 'dış depo', from_warehouse_id: 'kaynak depo', to_warehouse_id: 'hedef depo',
  supplier_id: 'tedarikçi', producer_supplier_id: 'üretici', customer_id: 'müşteri', recipe_id: 'reçete', sales_line_id: 'satış satırı',
  order_no: 'emir no', return_no: 'iade no', received_at: 'tarih', sold_at: 'satış tarihi', cut_at: 'parçalama tarihi',
  distributed_at: 'dağıtım tarihi', expiry_date: 'son kullanma tarihi', received_qty: 'teslim alınan miktar', planned_qty: 'planlanan miktar',
  input_weight_kg: 'giriş ağırlığı', actual_weight_kg: 'gerçek ağırlık', service_unit_price: 'hizmet birim fiyatı', sale_price: 'satış fiyatı',
  service_invoice_no: 'servis fatura no', service_invoiced_qty: 'faturalanan miktar', allocation_method: 'dağıtım yöntemi',
  cost_basis: 'maliyet esası', expected_ratio: 'beklenen oran', reference_sale_price: 'referans satış fiyatı',
  unit_weight_kg: 'birim ağırlık', default_sale_price: 'varsayılan satış fiyatı', shelf_life_days: 'raf ömrü',
  material_category: 'hammadde kategorisi', code: 'kod', name: 'ad', email: 'e-posta', note: 'not',
}

const STATUS_LABEL: Record<string, string> = {
  active: 'aktif', blocked: 'bloke', recalled: 'geri çağrılmış', consumed: 'tükenmiş', draft: 'taslak',
  materials_sent: 'hammadde sevk edilmiş', received: 'teslim alınmış', closed: 'kapalı', open: 'açık',
  extracted: 'öneri bekliyor', confirmed: 'onaylanmış', discarded: 'iptal edilmiş',
}

// Sunucu ve ML mesajlari ASCII yazilir; ekranda Turkce harflerle gosterilir.
const WORDS: Record<string, string> = {
  acik: 'açık', alinamaz: 'alınamaz', alinmis: 'alınmış', alis: 'alış', cagirma: 'çağırma', degisim: 'değişim', dusurur: 'düşürür',
  kaynagi: 'kaynağı', kayitlarca: 'kayıtlarca', numarasi: 'numarası', once: 'önce', saklanir: 'saklanır', alisin: 'alışın', alanlari: 'alanları', artti: 'arttı', asagida: 'aşağıda',
  asiyor: 'aşıyor', ayni: 'aynı', baska: 'başka', belgeye: 'belgeye', birakilan: 'bırakılan', bulunamadi: 'bulunamadı',
  buyuk: 'büyük', cikti: 'çıktı', ciktilari: 'çıktıları', degil: 'değil', dagitim: 'dağıtım', dis: 'dış', dusuk: 'düşük',
  edilen: 'edilen', edilenden: 'edilenden', edilmemis: 'edilmemiş', edilmis: 'edilmiş', fisinin: 'fişinin', gecersiz: 'geçersiz',
  Gercek: 'Gerçek', giris: 'giriş', girisi: 'girişi', guven: 'güven', hic: 'hiç', hicbir: 'hiçbir', iceriyor: 'içeriyor',
  icin: 'için', imkansiz: 'imkânsız', islenemez: 'işlenemez', kapali: 'kapalı', kapanis: 'kapanış', kapatilamaz: 'kapatılamaz',
  kayit: 'kayıt', kaydi: 'kaydı', kullaniliyor: 'kullanılıyor', miktari: 'miktarı', musteri: 'müşteri', musteriye: 'müşteriye',
  okunamadi: 'okunamadı', olmali: 'olmalı', olusturulamadi: 'oluşturulamadı', onaylanmis: 'onaylanmış', ortalamasindan: 'ortalamasından',
  parca: 'parça', Parcalama: 'Parçalama', parcalama: 'parçalama', parcalanamaz: 'parçalanamaz', recetede: 'reçetede', satilan: 'satılan',
  satilmamis: 'satılmamış', satir: 'satır', satirda: 'satırda', satiri: 'satırı', satis: 'satış', sifirdan: 'sıfırdan',
  siparis: 'sipariş', siparise: 'siparişe', stogu: 'stoğu', stogunu: 'stoğunu', tanimli: 'tanımlı', tasinabilir: 'taşınabilir',
  tedarikci: 'tedarikçi', toplami: 'toplamı', tum: 'tüm', tutari: 'tutarı', uretici: 'üretici', uretim: 'üretim', uretimle: 'üretimle', urun: 'ürün',
  uyarilar: 'uyarılar', uyusmazlik: 'uyuşmazlık', uyusmuyor: 'uyuşmuyor', veritabani: 'veritabanı', yalnizca: 'yalnızca', yuksek: 'yüksek',
}

const WARNING_LABEL: Record<string, string> = {
  PURCHASE_TOTAL_MISMATCH: 'Alış tutarı uyuşmazlığı',
  MISMATCH_RESOLUTION_MISSING: 'Uyuşmazlık çözümü eksik',
  WEIGHT_IMBALANCE: 'Ağırlık dengesi tutmuyor',
  QTY_VARIANCE: 'Miktar sapması',
  INVOICED_QTY_MISMATCH: 'Fatura miktarı farklı',
}
const WARNING_RE = new RegExp(`\\b(${Object.keys(WARNING_LABEL).join('|')})\\b`, 'g')

const trNumber = (s: string) => s.replace('.', ',')

function capitalize(s: string): string {
  return s.charAt(0).toLocaleUpperCase('tr') + s.slice(1)
}

const FIELD_RE = new RegExp(`\\b(${Object.keys(FIELD_LABEL).join('|')})\\b`, 'g')
const WORD_RE = new RegExp(`\\b(${Object.keys(WORDS).join('|')})\\b`, 'g')

// humanizeMessage: "inputs[0]: depoda stok yetersiz (lot X, eldeki 0.000, istenen 1.000, INSUFFICIENT_STOCK)"
// -> "1. girdi satırı: "X" lotu için seçilen depoda yeterli stok yok (depodaki 0,000, istenen 1,000)".
export function humanizeMessage(raw: string): string {
  const m = raw
    .replace(/,\s*(INSUFFICIENT_STOCK|LOT_BLOCKED)\)/g, ')')
    .replace(/(kaynak depoda stok|depoda stok|lot stogu|stok) yetersiz \(lot (.+?), eldeki ([\d.]+), (istenen|fire) ([\d.]+)\)/,
      (_, where: string, lot: string, have: string, kind: string, want: string) => {
        const place = where.startsWith('kaynak') ? 'kaynak depoda' : where.startsWith('depoda') ? 'seçilen depoda' : 'stokta'
        return `"${lot}" lotu için ${place} yeterli miktar yok (${place === 'stokta' ? 'stoktaki' : 'depodaki'} ${trNumber(have)}, ${kind === 'fire' ? 'fire' : 'istenen'} ${trNumber(want)})`
      })
    .replace(/^(inputs|lines|returns|outputs)\[(\d+)\](?:\.(\w+))?:?\s*/,
      (_, row: string, i: string, field?: string) =>
        `${Number(i) + 1}. ${ROW_LABEL[row]}: ${field ? `${FIELD_LABEL[field] ?? field} ` : ''}`)
    .replace(/\boutput\.qty\b/g, 'çıktı miktarı')
    .replace(/\boutput\./g, 'çıktı ')
    .replace(/ \((lines|inputs|outputs)\)/g, '')
    .replace(WARNING_RE, (c) => WARNING_LABEL[c])
    .replace(/ != /g, ' ≠ ')
    .replace(/durum: (\w+)/g, (s, st: string) => (STATUS_LABEL[st] ? `durum: ${STATUS_LABEL[st]}` : s))
    .replace(/\b(\w+) durumunda/g, (s, st: string) => (STATUS_LABEL[st] ? `${STATUS_LABEL[st]} durumunda` : s))
    .replace(/([\d.]+) sigma\b/g, (_, z: string) => `${trNumber(z)} standart sapma`)
    .replace(FIELD_RE, (f) => FIELD_LABEL[f])
    .replace(WORD_RE, (w) => WORDS[w])
  return capitalize(m)
}

const ALERT_TYPE_LABEL: Record<string, string> = {
  VERIM_DUSUK: 'Düşük verim',
  EKP_SAPMA: 'EKP sapması',
  FIRE_YUKSEK: 'Yüksek fire',
  MALIYET_ARTISI: 'Maliyet artışı',
  KUTLE_DENGESI: 'Kütle dengesi',
  NEGATIVE_STOCK_OFFLINE: 'Çevrimdışı eksi stok',
  LOT_BLOCKED_AT_SALE: 'Bloke lot satışı',
}
export const alertTypeLabel = (code: string) => ALERT_TYPE_LABEL[code] ?? code

export const warningLabel = (code: string) => WARNING_LABEL[code] ?? code

const PROCESS_LABEL: Record<string, string> = {
  production: 'Dış üretim', cutting: 'Parçalama', reprocess: 'Yeniden işleme', repack: 'Yeniden paketleme',
}
export const processLabel = (code: string | null | undefined) => (code ? PROCESS_LABEL[code] ?? code : '—')

const REF_TYPE_LABEL: Record<string, string> = {
  purchase_receipt: 'Alış', production_order: 'Dış üretim', cutting_order: 'Parçalama', reprocess_order: 'Yeniden işleme',
  sales_order: 'Satış', sales_line: 'Satış', shipment: 'Sevkiyat', customer_return: 'İade', stock_transfer: 'Transfer',
  recall: 'Geri çağırma', adjustment: 'Düzeltme', movement: 'Stok hareketi', stock_movement: 'Stok hareketi',
}
export const refTypeLabel = (code: string) => REF_TYPE_LABEL[code] ?? code

const MODEL_LABEL: Record<string, string> = {
  production_baseline_supplier_mean: 'Temel model (tedarikçi ortalaması)',
  production_ridge: 'Ridge regresyon',
  production_gbm: 'Gradient boosting',
  cutting_baseline_breed_mean: 'Temel model (ırk ortalaması)',
  cutting_ridge_multi: 'Ridge regresyon',
  cutting_gbm_multi: 'Gradient boosting',
}
export const modelLabel = (name: string) => MODEL_LABEL[name] ?? name

// OCR uyarilari "KOD: aciklama" bicimindedir; kod atilir, yalnizca aciklama gosterilir.
export const ocrWarning = (w: string) => humanizeMessage(w.replace(/^[A-Z_]+:\s*/, ''))
