// API istemcisi. Sozlesme: docs/api-contract.md, backend/api/openapi.yaml.
//
// KURAL: tutar ve miktarlar JSON'da STRING gelir ("18.1588"). Bunlari Number/parseFloat ile
// HESAPLAMAYIN; yalnizca gosterin. Hesap sunucuda (Go, shopspring/decimal) yapilir.

export const API_URL: string = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1'

export type Decimal = string

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message)
  }
}

export async function apiGet<T>(path: string, params?: Record<string, string | undefined>): Promise<T> {
  const url = new URL(API_URL + path)
  for (const [k, v] of Object.entries(params ?? {})) {
    if (v !== undefined && v !== '') url.searchParams.set(k, v)
  }
  let res: Response
  try {
    res = await fetch(url)
  } catch {
    throw new ApiError(0, 'network', `API'ye ulaşılamıyor (${API_URL})`)
  }
  const body = await res.json().catch(() => null)
  if (!res.ok) {
    throw new ApiError(res.status, body?.error?.code ?? 'unknown', body?.error?.message ?? res.statusText)
  }
  return body as T
}

export interface Item {
  id: string
  code: string
  name: string
  item_type: 'raw' | 'finished' | 'cut_part' | 'by_product' | 'waste'
  base_uom: 'kg' | 'adet'
  unit_weight_kg: Decimal | null
  default_sale_price: Decimal | null
  is_active: boolean
  created_at: string
}

export interface LotPurchase {
  receipt_id: string
  doc_no: string
  invoice_no: string | null
  unit_price: Decimal
  line_total: Decimal
  declared_line_total: Decimal | null
  has_mismatch: boolean
}

export interface Lot {
  id: string
  lot_no: string
  item_id: string
  item_code: string
  item_name: string
  supplier_name: string | null
  origin_type: string
  received_or_produced_at: string
  expiry_date: string | null
  initial_qty: Decimal
  uom: 'kg' | 'adet'
  unit_cost: Decimal
  status: 'active' | 'blocked' | 'recalled' | 'consumed'
  blocked_reason: string | null
  purchase: LotPurchase | null
  qty_on_hand?: Decimal
}

export interface List<T> {
  items: T[]
}
