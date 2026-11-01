// API istemcisi. Sozlesme: docs/api-contract.md, backend/api/openapi.yaml.
//
// KURAL: tutar ve miktarlar JSON'da STRING gelir ("18.1588"). Bunlari Number/parseFloat ile
// HESAPLAMAYIN; yalnizca gosterin. Hesap sunucuda (Go, shopspring/decimal) yapilir.

import { humanizeMessage } from './messages'

export const API_URL: string = import.meta.env.VITE_API_URL ?? 'http://localhost:8080/api/v1'

export type Decimal = string

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(humanizeMessage(message))
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

// apiSend POST/PUT/PATCH/DELETE istekleri icin (govde opsiyonel). Status, body yoksa (reverse/close gibi) atlanir.
export async function apiSend<T>(method: 'POST' | 'PUT' | 'PATCH' | 'DELETE', path: string, body?: unknown): Promise<T> {
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
export const apiPatch = <T>(path: string, body?: unknown) => apiSend<T>('PATCH', path, body)
export const apiDelete = (path: string) => apiSend<void>('DELETE', path)

// apiUpload: multipart/form-data dosya yuklemesi (fatura/irsaliye OCR, bkz. docs/invoice-ocr.md).
// Content-Type HEADER'I ELLE AYARLANMAZ: tarayici boundary'yi kendisi ekler.
export async function apiUpload<T>(path: string, file: File): Promise<T> {
  const form = new FormData()
  form.append('file', file)
  let res: Response
  try {
    res = await fetch(API_URL + path, { method: 'POST', body: form })
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

// apiFileURL: sunucuda saklanan bir dosyayi (ornek: yuklenen fatura goruntusu) dogrudan gosterir.
export const apiFileURL = (path: string): string => API_URL + path

export interface Item {
  id: string
  code: string
  name: string
  item_type: 'raw' | 'finished' | 'cut_part' | 'by_product' | 'waste'
  base_uom: 'kg' | 'adet'
  unit_weight_kg: Decimal | null
  default_sale_price: Decimal | null
  /** raf ömrü (gün); lot SKT'si verilmezse lot tarihinden türetilir */
  shelf_life_days: number | null
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
  balances?: LotBalanceEntry[]
}

// Depolar arasi transfer (POST /stock-transfers)
export interface StockTransfer {
  id: string
  transfer_no: string
  from_warehouse_id: string
  from_warehouse_code: string
  to_warehouse_id: string
  to_warehouse_code: string
  note: string | null
  created_at: string
  lines: { lot_id: string; lot_no: string; item_id: string; qty: Decimal }[]
}

export interface List<T> {
  items: T[]
}

// ---------------------------------------------------------------------------
// Alışlar (purchase-receipts)
// ---------------------------------------------------------------------------

export interface PurchaseReceiptListItem {
  id: string
  doc_no: string
  supplier_id: string
  supplier_name: string
  invoice_no: string | null
  received_at: string
  line_count: number
  total_amount: Decimal
  has_mismatch: boolean
  created_at: string
}

export interface PurchaseReceiptLineView {
  id: string
  item_id: string
  item_code: string
  item_name: string
  lot_id: string
  lot_no: string
  qty: Decimal
  unit_price: Decimal
  line_total: Decimal
  declared_line_total: Decimal | null
  has_mismatch: boolean
}

export interface PurchaseReceiptDetail {
  id: string
  doc_no: string
  supplier_id: string
  supplier_name: string
  invoice_no: string | null
  received_at: string
  total_amount: Decimal
  has_mismatch: boolean
  created_at: string
  lines: PurchaseReceiptLineView[]
  lots: Lot[]
  movements: StockMovement[]
}

export interface CreatePurchaseReceiptLineRequest {
  lot_no: string
  item_id: string
  warehouse_id: string
  qty: string
  unit_price: string
  declared_line_total?: string
  expiry_date?: string
}

export interface CreatePurchaseReceiptRequest {
  id?: string
  doc_no?: string
  supplier_id: string
  invoice_no?: string
  received_at: string
  lines: CreatePurchaseReceiptLineRequest[]
}

// ---------------------------------------------------------------------------
// Fatura/irsaliye okuma (invoice-imports) -- bkz. docs/invoice-ocr.md.
//
// KESIN KURAL: OCR SADECE oneri uretir, "dogru" diye sunulmaz. Onay (confirm) olmadan hicbir
// lot/stok hareketi/alis fisi olusmaz -- confirm govdesi OCR ciktisi DEGIL, kullanicinin
// duzelttigi veridir (CreatePurchaseReceiptRequest ile aynidir, mevcut alis fisi olusturma yolu
// aynen kullanilir).
// ---------------------------------------------------------------------------

export interface OcrField {
  value: string | null
  confidence: number // 0..1
  source_text: string
  warnings: string[]
}

export interface OcrLine {
  product_name: OcrField
  quantity: OcrField
  unit: OcrField
  unit_price: OcrField
  line_total: OcrField
  lot_no: OcrField
  expiry_date: OcrField
  warnings: string[]
}

export interface OcrInvoiceExtraction {
  supplier_name: OcrField
  document_no: OcrField
  document_date: OcrField
  currency: OcrField
  lines: OcrLine[]
  overall_confidence: number
  raw_text: string
  warnings: string[]
}

export interface MatchCandidate {
  id: string
  code: string
  name: string
  score: number
}

export type InvoiceImportStatus = 'extracted' | 'confirmed' | 'discarded'

export interface InvoiceImportListItem {
  id: string
  file_name: string
  uploaded_at: string
  status: InvoiceImportStatus
  overall_confidence: number
  purchase_receipt_id: string | null
  purchase_receipt_doc_no: string | null
}

export interface InvoiceImportDetail {
  id: string
  file_name: string
  content_type: string
  uploaded_at: string
  status: InvoiceImportStatus
  ocr: OcrInvoiceExtraction
  overall_confidence: number
  supplier_candidates: MatchCandidate[]
  line_item_candidates: MatchCandidate[][]
  purchase_receipt_id: string | null
  confirmed_at: string | null
  discarded_at: string | null
  created_at: string
}

// Guven esikleri (ml/app/config.py OCR_CONFIDENCE_HIGH/MEDIUM ile ayni, bkz. docs/invoice-ocr.md).
export const OCR_CONFIDENCE_HIGH = 0.8
export const OCR_CONFIDENCE_MEDIUM = 0.5

// ---------------------------------------------------------------------------
// Tahminler (predictions) -- bkz. docs/ml-report.md
// ---------------------------------------------------------------------------

export type PredictionTargetType = 'production' | 'cutting'

export interface PredictionPartDiff {
  part_key: string
  predicted_kg: Decimal | null
  actual_kg: Decimal | null
  diff_kg: Decimal | null
  diff_pct: Decimal | null
}

export interface PredictionListItem {
  id: string
  created_at: string
  target_type: PredictionTargetType
  stage: 'draft' | 'materials_sent' | null
  ref_type: 'production_order' | 'cutting_order'
  ref_id: string
  ref_no: string | null
  model_name: string
  model_version: string
  baseline: boolean
  metric: string
  metric_unit: string
  predicted_value: Decimal | null
  actual_value: Decimal | null
  error_abs: Decimal | null
  error_pct: Decimal | null
  has_actual: boolean
  predicted_ekp_unit?: Decimal | null
  actual_ekp_unit?: Decimal | null
  ekp_error_abs?: Decimal | null
  ekp_error_pct?: Decimal | null
  parts?: PredictionPartDiff[]
}

export interface PredictionSummaryEntry {
  count: number
  count_with_actual: number
  mae: Decimal | null
  mape: Decimal | null
}

export interface PredictionSummary {
  production: PredictionSummaryEntry
  cutting: PredictionSummaryEntry
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
  anomaly_alerts?: AnomalyAlert[]
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

export interface Customer {
  id: string
  code: string
  name: string
  customer_type: string
}

// ---------------------------------------------------------------------------
// Satış / Sevkiyat / İade
// ---------------------------------------------------------------------------

export interface SalesLine {
  id: string
  sales_order_id: string
  item_id: string
  item_code: string
  item_name: string
  lot_id: string
  lot_no: string
  qty: Decimal
  unit_price: Decimal
  line_total: Decimal
  stock_movement_id: string | null
  recall_violation: boolean
  negative_stock_violation: boolean
  created_at: string
}

export interface ShipmentLine {
  id: string
  lot_id: string
  lot_no: string
  item_id: string
  qty: Decimal
}

export interface Shipment {
  id: string
  shipment_no: string
  customer_id: string
  customer_name: string
  sales_order_id: string | null
  shipped_at: string
  vehicle: string | null
  created_at: string
  lines?: ShipmentLine[]
}

export interface SalesOrder {
  id: string
  order_no: string
  customer_id: string
  customer_code: string
  customer_name: string
  device_id: string | null
  warehouse_id: string
  warehouse_code: string
  sold_at: string
  status: 'confirmed'
  total_amount: Decimal
  has_violation: boolean
  created_at: string
  lines?: SalesLine[]
  shipments?: Shipment[]
}

export interface CustomerReturn {
  id: string
  return_no: string
  customer_id: string
  customer_name: string
  sales_line_id: string | null
  lot_id: string
  lot_no: string
  qty: Decimal
  reason: string
  returned_at: string
  created_at: string
}

export interface Warehouse {
  id: string
  code: string
  name: string
  warehouse_type: string
}

// ---------------------------------------------------------------------------
// Parçalama (Görev 2)
// ---------------------------------------------------------------------------

export interface CuttingRecipeLine {
  part_item_id: string
  part_item_code: string
  part_item_name: string
  expected_ratio: Decimal
  reference_sale_price: Decimal
  is_by_product: boolean
  is_waste: boolean
}

export interface CuttingRecipe {
  id: string
  code: string
  name: string
  animal_type: string
  lines: CuttingRecipeLine[]
  created_at: string
}

export type CuttingAllocationMethod = 'quantity' | 'value'
export type CuttingOrderStatus = 'draft' | 'closed'

export interface CuttingResultPart {
  name: string
  role: 'main' | 'waste' | 'by_product'
  kg: Decimal
  yield_pct: Decimal
  allocated_cost: Decimal
  unit_cost: Decimal
}

export interface CuttingAlternativeBasis {
  cost_basis: 'computed' | 'declared'
  total_input_cost: Decimal
  parts: { name: string; allocated_cost: Decimal }[]
  total_allocated: Decimal
}

// CuttingResult: domain.AllocateCutting'in ciktisi, hem golden dosyalariyla hem de bu tip ile
// BIREBIR aynidir (bkz. docs/golden/cutting_cases.json).
export interface CuttingResult {
  method: CuttingAllocationMethod
  cost_basis: 'computed' | 'declared' | null
  cost_basis_used: 'computed' | 'declared'
  resolution_note: string | null
  input_weight_kg: Decimal
  computed_total_cost: Decimal | null
  declared_total_cost: Decimal | null
  total_input_cost: Decimal
  parts_weight_kg: Decimal
  balance_diff_kg: Decimal
  closable: boolean
  sellable_kg: Decimal
  by_product_value: Decimal
  allocation_pool: Decimal
  parts: CuttingResultPart[]
  total_allocated: Decimal
  alternative_basis: CuttingAlternativeBasis | null
  warnings: string[]
}

export interface CuttingOutputLine {
  part_item_id: string
  part_item_code: string
  part_item_name: string
  is_waste: boolean
  is_by_product: boolean
  expected_ratio: Decimal
  expected_weight_kg: Decimal
  reference_sale_price: Decimal
  predicted_weight_kg: Decimal | null
  actual_weight_kg: Decimal | null
  output_lot_id: string | null
  output_lot_no: string | null
  allocated_cost: Decimal | null
  unit_cost: Decimal | null
}

export interface CuttingAllocationPreview {
  quantity: CuttingResult | null
  value: CuttingResult | null
}

export interface CuttingPredictionView {
  model_name: string
  model_version: string
  baseline: boolean
  breed_used: string
  expected_fire_kg: Decimal
  expected_fire_ratio: Decimal
  parts_kg: Record<string, Decimal>
}

export interface CuttingOrder {
  id: string
  order_no: string
  recipe_id: string
  recipe_code: string
  recipe_name: string
  input_lot_id: string
  input_lot_no: string
  input_item_code: string
  input_item_name: string
  input_weight_kg: Decimal
  unit_cost_per_kg: Decimal
  computed_total_cost: Decimal
  declared_total_cost: Decimal | null
  total_input_cost: Decimal
  allocation_method: CuttingAllocationMethod
  cost_basis: 'computed' | 'declared' | null
  cost_basis_used: 'computed' | 'declared'
  resolution_note: string | null
  status: CuttingOrderStatus
  cut_at: string
  balance_diff_kg: Decimal | null
  closable: boolean
  warnings: string[]
  outputs?: CuttingOutputLine[]
  allocation?: CuttingAllocationPreview
  ml_prediction?: CuttingPredictionView | null
  closed_at: string | null
  created_at: string
  anomaly_alerts?: AnomalyAlert[]
}

// ---------------------------------------------------------------------------
// İzlenebilirlik + Geri Çağırma (Görev 3)
// ---------------------------------------------------------------------------

export interface LotBalanceEntry {
  warehouse_id: string
  warehouse_code: string
  qty_on_hand: Decimal
}

export interface TraceShipmentLine {
  shipment_no: string
  customer_id: string
  customer_name: string
  qty: Decimal
  shipped_at: string
}

export interface TraceSaleLine {
  order_no: string
  customer_id: string
  customer_name: string
  qty: Decimal
  sold_at: string
  recall_violation: boolean
}

export interface TraceForwardNode {
  lot_id: string
  lot_no: string
  item_id: string
  item_name: string
  uom: 'kg' | 'adet'
  status: Lot['status']
  depth: number
  path: string
  parent_lot_id: string | null
  qty_from_parent: Decimal | null
  process_type: string | null
  process_id: string | null
  total_produced_qty: Decimal
  ratio_from_parent: Decimal | null
  current_stock: LotBalanceEntry[]
  qty_on_hand_total: Decimal
  shipments: TraceShipmentLine[]
  sales: TraceSaleLine[]
  returned_qty: Decimal
}

export interface TraceForwardCustomerSummary {
  customer_id: string
  customer_name: string
  qty_shipped: Decimal
  qty_returned: Decimal
  downstream_known: boolean
  downstream_note?: string
  downstream: DownstreamDistribution[]
}

// İkinci kademe dağıtım kaydı (POST /downstream-distributions)
export interface DownstreamDistribution {
  id: string
  customer_id: string
  customer_name: string
  lot_id: string
  lot_no: string
  recipient_name: string
  recipient_phone: string | null
  qty: Decimal
  distributed_at: string
  note: string | null
  created_at: string
}

// Yeniden işleme (POST /reprocess-orders)
export interface ReprocessOrder {
  id: string
  order_no: string
  warehouse_id: string
  warehouse_code: string
  output_lot_id: string | null
  output_lot_no: string | null
  output_item_name: string | null
  output_qty: Decimal | null
  output_uom: string | null
  output_unit_cost: Decimal | null
  total_cost: Decimal
  note: string | null
  created_at: string
  inputs: { lot_id: string; lot_no: string; item_name: string; qty: Decimal; uom: string; unit_cost: Decimal }[]
}

export interface TraceForwardResponse {
  root_lot_id: string
  root_lot_no: string
  nodes: TraceForwardNode[]
  summary: {
    affected_lot_count: number
    remaining_stock: { uom: string; qty: Decimal }[]
    customers: TraceForwardCustomerSummary[]
  }
}

export interface TraceBackwardNode {
  lot_id: string
  lot_no: string
  item_id: string
  item_name: string
  uom: 'kg' | 'adet'
  supplier_id: string | null
  supplier_name: string | null
  supplier_contact_name: string | null
  supplier_phone: string | null
  supplier_email: string | null
  received_or_produced_at: string
  expiry_date: string | null
  depth: number
  path: string
  child_lot_id: string | null
  qty_used: Decimal | null
  process_type: string | null
  process_id: string | null
  order_no: string | null
}

export interface TraceBackwardResponse {
  root_lot_id: string
  root_lot_no: string
  nodes: TraceBackwardNode[]
  summary: { raw_lot_count: number; supplier_count: number }
}

export interface GenealogyNode {
  lot_id: string
  lot_no: string
  item_name: string
  level: number
  status: Lot['status']
}

export interface GenealogyEdge {
  from_lot_id: string
  to_lot_id: string
  qty: Decimal
  uom: string
  process_type: string
  process_id: string
}

export interface GenealogyResponse {
  root_lot_id: string
  root_lot_no: string
  nodes: GenealogyNode[]
  edges: GenealogyEdge[]
}

export interface RecallLot {
  lot_id: string
  lot_no: string
  item_name: string
  depth: number
  path: string
  status: Lot['status']
}

export interface RecallCustomerView {
  id: string
  customer_id: string
  customer_name: string
  customer_phone: string | null
  customer_email: string | null
  lot_id: string
  lot_no: string
  qty_shipped: Decimal
  qty_returned: Decimal
  remaining_risk_qty: Decimal
  contact_status: 'pending' | 'contacted' | 'confirmed'
  notes: string | null
  downstream_known: boolean
  downstream_note?: string
  has_violation: boolean
  downstream: DownstreamDistribution[]
}

export interface Recall {
  id: string
  recall_no: string
  root_lot_id: string
  root_lot_no: string
  reason: string
  status: 'open' | 'closed'
  opened_at: string
  closed_at: string | null
  created_at: string
  affected_lot_count: number
  pending_customer_count: number
  affected_lots?: RecallLot[]
  customers?: RecallCustomerView[]
}

// ---------------------------------------------------------------------------
// Anomali uyarıları (bkz. docs/anomaly-integration.md)
// ---------------------------------------------------------------------------

export type AnomalySeverity = 'info' | 'warning' | 'critical'
export type AnomalyRefType = 'production_order' | 'cutting_order' | 'purchase_receipt' | 'sales_line'

// SADECE BİLGİLENDİRMEDİR: hiçbir uyarı bir lotu bloke etmez veya bir işlemi reddetmez.
export interface AnomalyAlert {
  id: string
  alert_type: string
  severity: AnomalySeverity
  ref_type: AnomalyRefType
  ref_id: string
  ref_no: string | null
  message: string
  evidence?: Record<string, string>
  acknowledged: boolean
  note: string | null
  created_at: string
  updated_at: string
}

export interface AnomalyAlertSummary {
  critical: number
  warning: number
  info: number
  total: number
}
