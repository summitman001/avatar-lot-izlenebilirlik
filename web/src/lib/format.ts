// Ekran biçimlendirme (tr-TR): binlik ayracı nokta, ondalık ayracı virgül ("1.543,50 €").
//
// API tutar ve miktarları STRING gönderir ("18.1588"); burada Number'a çevrilmeden metin olarak
// biçimlenir (api.ts KURAL). Gereken yuvarlama da metin üzerinde (yarım yukarı) yapılır, yani
// gösterilen hane hiçbir zaman kayan nokta hatası taşımaz. Yalnızca GÖSTERİM içindir: API'ye
// gönderilen değerler bu fonksiyonlardan geçmez.

export type Num = string | number | null | undefined

const DASH = '—'

function parse(v: string | number): { neg: boolean; int: string; frac: string } | null {
  const s = typeof v === 'number' ? (Number.isFinite(v) ? v.toFixed(10) : '') : v.trim()
  const m = /^([+-]?)(\d*)(?:\.(\d*))?$/.exec(s)
  if (!m || (m[2] === '' && !m[3])) return null
  return { neg: m[1] === '-', int: m[2].replace(/^0+(?=\d)/, '') || '0', frac: m[3] ?? '' }
}

// Kesir hanelerini `max` haneye yarım yukarı yuvarlar (metin üzerinde).
function round(int: string, frac: string, max: number): [string, string] {
  if (frac.length <= max) return [int, frac]
  const digits = (int + frac.slice(0, max)).split('')
  if (frac.charCodeAt(max) >= 53 /* '5' */) {
    let i = digits.length - 1
    for (; i >= 0; i--) {
      if (digits[i] !== '9') { digits[i] = String(Number(digits[i]) + 1); break }
      digits[i] = '0'
    }
    if (i < 0) digits.unshift('1')
  }
  const s = digits.join('')
  return [s.slice(0, s.length - max) || '0', s.slice(s.length - max)]
}

// num: genel sayı. minFrac kadar hane her zaman yazılır, fazlası sondaki sıfırlar atılarak maxFrac'e kadar.
export function num(v: Num, minFrac = 0, maxFrac = 6): string {
  if (v == null || v === '') return DASH
  const p = parse(v)
  if (!p) return String(v)
  const [int, rawFrac] = round(p.int, p.frac, maxFrac)
  const frac = rawFrac.replace(/0+$/, '').padEnd(minFrac, '0')
  const zero = /^0*$/.test(int + frac)
  return (p.neg && !zero ? '-' : '') + int.replace(/\B(?=(\d{3})+(?!\d))/g, '.') + (frac ? ',' + frac : '')
}

// Miktar: sondaki sıfırlar atılır, birim ayrı yazılır. "11.000" → "11 kg", "7.500" → "7,5 kg".
export const qty = (v: Num, uom?: string | null): string =>
  v == null || v === '' ? DASH : uom ? `${num(v)} ${uom}` : num(v)

// Para: her zaman 2 ondalık. "1543.5000" → "1.543,50 €".
export const money = (v: Num): string => (v == null || v === '' ? DASH : `${num(v, 2, 2)} €`)

// Birim maliyet/fiyat (EKP vb.): 4 ondalık korunur. "18.1588" → "18,1588 €".
export const unitCost = (v: Num): string => (v == null || v === '' ? DASH : `${num(v, 4, 4)} €`)

// Yüzde (değer zaten yüzde cinsinden gelir, 27.07 = %27,07): 2 ondalık. Negatifte işaret başta: "-%5,00".
export function pct(v: Num): string {
  if (v == null || v === '') return DASH
  const s = num(v, 2, 2)
  return s.startsWith('-') ? `-%${s.slice(1)}` : `%${s}`
}
