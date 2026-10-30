// UUIDv7 (RFC 9562): 48 bit milisaniye zaman damgasi + rastgele bitler. Sunucu istemci id'lerini
// yalnizca v7 olarak kabul eder (bkz. docs/api-contract.md); formlar bunu idempotency anahtari olarak
// gonderir: ayni form tekrar gonderilirse (yanit kayboldu, cift tiklama) sunucu ayni kaydi doner.
export function uuidv7(): string {
  const bytes = new Uint8Array(16)
  crypto.getRandomValues(bytes)
  let ts = Date.now()
  for (let i = 5; i >= 0; i--) {
    bytes[i] = ts & 0xff
    ts = Math.floor(ts / 256)
  }
  bytes[6] = (bytes[6] & 0x0f) | 0x70 // surum 7
  bytes[8] = (bytes[8] & 0x3f) | 0x80 // RFC 4122 varyanti
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
