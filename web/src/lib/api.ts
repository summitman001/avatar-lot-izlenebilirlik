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

// apiSend POST istekleri icin (govde opsiyonel). Status, body yoksa (reverse/close gibi) atlanir.
export async function apiSend<T>(method: 'POST' | 'PUT', path: string, body?: unknown): Promise<T> {
  let res: Response
  try {
    res = await fetch(API_URL + path, {
      method,
      headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
  } catch {
    throw new ApiError(0, 'network', `API'ye ulaşılamıyor (${API_URL})`)
  }
  const raw = await res.text()
  const parsed = raw ? JSON.parse(raw) : null
  if (!res.ok) {
    throw new ApiError(res.status, parsed?.error?.code ?? 'unknown', parsed?.error?.message ?? res.statusText)
  }
  return parsed as T
}

export const apiPost = <T>(path: string, body?: unknown) => apiSend<T>('POST', path, body)

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

// ---------------------------------------------------------------------------
// Dis uretim (Gorev 1)
// ---------------------------------------------------------------------------

export type ProductionOrderStatus = 'draft' | 'materials_sent' | 'received' | 'closed'

export interface ProductionInputLine {
  id: string
  lot_id: string
  lot_no: string
  item_id: string
  item_code: string
  item_name: string
  material_category: string | null
  qty_sent: Decimal
  qty_consumed: Decimal | null
  qty_returned: Decimal | null
  unit_cost_at_send: Decimal
}

export interface ProductionStatusEvent {
  status: ProductionOrderStatus
  at: string
}

// ProductionPrediction: EN GUNCEL tahmin kaydi (draft veya materials_sent, hangisi daha yeniyse).
// predicted_ekp_unit yalnizca 'materials_sent' asamasinda doludur (malzeme maliyeti o asamada
// kesinlesir); hibrit yaklasim: ML'in fiziksel verim tahmini + kesin malzeme maliyeti.
export interface ProductionPrediction {
  model_name: string
  model_version: string
  baseline: boolean
  stage: 'draft' | 'materials_sent'
  predicted_yield_ratio: Decimal
  predicted_output_kg: Decimal
  predicted_delivered_qty: Decimal
  predicted_ekp_unit: Decimal | null
  actual_delivered_qty: Decimal | null
  actual_ekp_unit: Decimal | null
  delivered_qty_error_abs: Decimal | null
  delivered_qty_error_pct: Decimal | null
  ekp_error_abs: Decimal | null
  ekp_error_pct: Decimal | null
}

export interface ProductionOrder {
  id: string
  order_no: string
  product_item_id: string
  product_item_code: string
  product_item_name: string
  product_unit_weight_kg: Decimal | null
  producer_supplier_id: string
  producer_supplier_name: string
  external_warehouse_id: string
  external_warehouse_code: string
  planned_qty: Decimal
  received_qty: Decimal | null
  service_unit_price: Decimal
  service_invoice_no: string | null
  service_invoiced_qty: Decimal | null
  status: ProductionOrderStatus
  output_lot_id: string | null
  output_lot_no: string | null
  material_cost: Decimal | null
  service_cost: Decimal | null
  total_cost: Decimal | null
  ekp_unit: Decimal | null
  sale_price: Decimal | null
  closed_at: string | null
  created_at: string
  warnings: string[]
  inputs?: ProductionInputLine[]
  status_history?: ProductionStatusEvent[]
  prediction?: ProductionPrediction | null
}

export interface CostReportLine {
  label: string
  qty: Decimal | null
  unit_price: Decimal | null
  amount: Decimal
}

export interface ProductionCostReport {
  order_no: string
  status: ProductionOrderStatus
  lines: CostReportLine[]
  material_cost: Decimal
  service_cost: Decimal
  total_cost: Decimal
  planned_qty: Decimal
  received_qty: Decimal
  invoiced_qty: Decimal
  qty_variance_pct: Decimal
  ekp_unit: Decimal
  ekp_display: Decimal
  ekp_per_kg: Decimal | null
  unit_weight_kg: Decimal | null
  output_kg: Decimal | null
  input_kg_total: Decimal
  yield_input_pct: Decimal | null
  yield_meat_ratio: Decimal | null
  sale_price: Decimal | null
  revenue: Decimal | null
  gross_profit_unit: Decimal | null
  gross_profit_total: Decimal | null
  margin_pct: Decimal | null
  warnings: string[]
  prediction: ProductionPrediction | null
}

export interface StockMovement {
  id: string
  lot_id: string
  lot_no: string
  warehouse_id: string
  movement_type: string
  qty: Decimal
  unit_cost: Decimal
  ref_type: string
  ref_id: string
  reverses_movement_id: string | null
  device_id: string | null
  client_created_at: string | null
  note: string | null
  created_at: string
}

export interface Supplier {
  id: string
  code: string
  name: string
  supplier_type: string
}

export interface Warehouse {
  id: string
  code: string
  name: string
  warehouse_type: string
}
