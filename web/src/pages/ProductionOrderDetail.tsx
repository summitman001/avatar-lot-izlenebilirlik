import {
  ActionIcon, Alert, Anchor, Badge, Button, Card, Group, Loader, NumberInput, Select, SimpleGrid, Stack,
  Stepper, Table, Text, TextInput, Timeline, Tooltip,
} from '@mantine/core'
import { IconPlus, IconTrash } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { PageHeader } from '../components/Page'
import { AnomalyAlertsCard } from '../components/AnomalyAlerts'
import {
  ApiError, apiGet, apiPost, type List, type Lot, type ProductionCostReport,
  type ProductionOrder, type StockMovement,
} from '../lib/api'
import { modelLabel, warningLabel } from '../lib/messages'

const STEPS: ProductionOrder['status'][] = ['draft', 'materials_sent', 'received', 'closed']
const STEP_LABEL: Record<ProductionOrder['status'], string> = {
  draft: 'Taslak', materials_sent: 'Sevk Edildi', received: 'Teslim Alındı', closed: 'Kapandı',
}
const MOVEMENT_LABEL: Record<string, string> = {
  send_to_producer: 'Üreticiye Sevk', transfer_in: 'Depo Girişi', transfer_out: 'Depo Çıkışı',
  production_consume: 'Üretimde Tüketim', production_in: 'Mamul Girişi',
}

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

export default function ProductionOrderDetail() {
  const { id = '' } = useParams()
  const qc = useQueryClient()
  const orderQuery = useQuery({
    queryKey: ['production-order', id],
    queryFn: () => apiGet<ProductionOrder>(`/production-orders/${id}`),
    refetchInterval: 5_000,
  })
  const movementsQuery = useQuery({
    queryKey: ['production-order', id, 'movements'],
    queryFn: () => apiGet<List<StockMovement>>(`/production-orders/${id}/movements`),
    enabled: orderQuery.data?.status !== 'draft',
  })
  const costReportQuery = useQuery({
    queryKey: ['production-order', id, 'cost-report'],
    queryFn: () => apiGet<ProductionCostReport>(`/production-orders/${id}/cost-report`),
    enabled: orderQuery.data?.status === 'received' || orderQuery.data?.status === 'closed',
  })

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['production-order', id] })
    qc.invalidateQueries({ queryKey: ['production-orders'] })
  }

  if (orderQuery.isLoading) return <Loader />
  if (orderQuery.error) return <Alert color="red">{orderQuery.error.message}</Alert>
  const order = orderQuery.data!

  return (
    <Stack>
      <PageHeader mb="xs" title={order.order_no}
        eyebrow={<><Anchor component={Link} to="/dis-uretim" size="sm">Dış Üretim</Anchor> · {order.product_item_name}</>}
        badges={order.warnings.map((w) => <Badge key={w} color="orange" variant="light">{warningLabel(w)}</Badge>)} />

      <Stepper active={STEPS.indexOf(order.status)} size="sm">
        {STEPS.map((s) => <Stepper.Step key={s} label={STEP_LABEL[s]} />)}
      </Stepper>

      <SimpleGrid cols={{ base: 1, md: 2 }}>
        <SummaryCard order={order} />
        {order.prediction && <PredictionCard prediction={order.prediction} />}
      </SimpleGrid>

      {order.status === 'draft' || order.status === 'materials_sent' ? (
        <SendMaterialsForm orderId={id} externalWarehouseId={order.external_warehouse_id} onDone={invalidate} />
      ) : null}

      {order.anomaly_alerts && (
        <AnomalyAlertsCard alerts={order.anomaly_alerts} queryKeysToInvalidate={[['production-order', id]]} />
      )}

      {order.inputs && order.inputs.length > 0 && <InputsTable order={order} />}

      {order.status === 'materials_sent' && <ReceiveForm orderId={id} onDone={invalidate} />}

      {order.status === 'received' && <CloseForm orderId={id} invoiceSet={!!order.service_invoice_no} onDone={invalidate} />}

      {costReportQuery.data && <CostReportCard report={costReportQuery.data} />}

      {movementsQuery.data && <MovementsTimeline movements={movementsQuery.data.items} />}
    </Stack>
  )
}

function SummaryCard({ order }: { order: ProductionOrder }) {
  return (
    <Card withBorder>
      <Stack gap="xs">
        <Group justify="space-between"><Text fw={500}>Özet</Text></Group>
        <Group justify="space-between"><Text size="sm" c="dimmed">Dış Üretici</Text><Text size="sm">{order.producer_supplier_name}</Text></Group>
        <Group justify="space-between"><Text size="sm" c="dimmed">Dış Üretici Deposu</Text><Text size="sm">{order.external_warehouse_code}</Text></Group>
        <Group justify="space-between"><Text size="sm" c="dimmed">Planlanan / Teslim Alınan</Text><Text size="sm">{order.planned_qty} / {order.received_qty ?? '—'} adet</Text></Group>
        <Group justify="space-between"><Text size="sm" c="dimmed">Hizmet Birim Fiyatı</Text><Text size="sm">{order.service_unit_price} €</Text></Group>
        <Group justify="space-between"><Text size="sm" c="dimmed">Hizmet Faturası</Text><Text size="sm">{order.service_invoice_no ?? '—'}</Text></Group>
        <Group justify="space-between"><Text size="sm" c="dimmed">Çıktı Lotu</Text>
          <Text size="sm">{order.output_lot_id
            ? <Link to={`/lotlar?q=${order.output_lot_no}`}>{order.output_lot_no}</Link>
            : '—'}</Text>
        </Group>
        {order.ekp_unit && (
          <>
            <Text ta="center" fw={700} size="xl" mt="xs">EKP {order.ekp_unit.replace('.', ',')} €</Text>
            {order.sale_price && <Text ta="center" c="dimmed" size="sm">VKP {order.sale_price.replace('.', ',')} €</Text>}
          </>
        )}
      </Stack>
    </Card>
  )
}

const STAGE_LABEL: Record<NonNullable<ProductionOrder['prediction']>['stage'], string> = {
  draft: 'taslak tahmini', materials_sent: 'sevk sonrası tahmini',
}

function PredictionCard({ prediction: p }: { prediction: NonNullable<ProductionOrder['prediction']> }) {
  return (
    <Card withBorder>
      <Stack gap="xs">
        <Group justify="space-between">
          <Text fw={500}>Tahmin vs Gerçek</Text>
          <Group gap="xs">
            <Badge variant="light" color="gray">{STAGE_LABEL[p.stage]}</Badge>
            <Badge variant="light" tt="none">{modelLabel(p.model_name)}</Badge>
          </Group>
        </Group>
        <Group justify="space-between"><Text size="sm" c="dimmed">Beklenen Adet</Text><Text size="sm">{p.predicted_delivered_qty}</Text></Group>
        <Group justify="space-between"><Text size="sm" c="dimmed">Gerçek Adet</Text><Text size="sm">{p.actual_delivered_qty ?? '—'}</Text></Group>
        <Group justify="space-between"><Text size="sm" c="dimmed">Adet Sapması</Text>
          <Text size="sm">{p.delivered_qty_error_abs ? `${p.delivered_qty_error_abs} (%${p.delivered_qty_error_pct})` : '—'}</Text>
        </Group>
        <Group justify="space-between"><Text size="sm" c="dimmed">Beklenen EKP</Text>
          <Text size="sm">{p.predicted_ekp_unit ? `${p.predicted_ekp_unit} €` : '—'}</Text>
        </Group>
        <Group justify="space-between"><Text size="sm" c="dimmed">Gerçek EKP</Text>
          <Text size="sm">{p.actual_ekp_unit ? `${p.actual_ekp_unit} €` : '—'}</Text>
        </Group>
        <Group justify="space-between"><Text size="sm" c="dimmed">EKP Sapması</Text>
          <Text size="sm">{p.ekp_error_abs ? `${p.ekp_error_abs} € (%${p.ekp_error_pct})` : '—'}</Text>
        </Group>
        <Text size="xs" c="dimmed">
          Beklenen EKP hibrit bir tahmindir: ML&apos;in fiziksel verim tahmini + kesin malzeme maliyeti
          (yalnızca hammaddeler sevk edildikten sonra hesaplanabilir).
        </Text>
      </Stack>
    </Card>
  )
}

function InputsTable({ order }: { order: ProductionOrder }) {
  return (
    <Card withBorder>
      <Text fw={600} mb="sm">Sevk Edilen Hammaddeler</Text>
      <Table>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Lot</Table.Th><Table.Th>Ürün</Table.Th>
            <Table.Th ta="right">Sevk</Table.Th><Table.Th ta="right">Tüketilen</Table.Th>
            <Table.Th ta="right">İade</Table.Th><Table.Th ta="right">Birim Maliyet</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {order.inputs!.map((i) => (
            <Table.Tr key={i.id}>
              <Table.Td><Link to={`/lotlar?q=${i.lot_no}`}>{i.lot_no}</Link></Table.Td>
              <Table.Td>{i.item_name}</Table.Td>
              <Table.Td ta="right">{i.qty_sent}</Table.Td>
              <Table.Td ta="right">{i.qty_consumed ?? '—'}</Table.Td>
              <Table.Td ta="right">{i.qty_returned ?? '—'}</Table.Td>
              <Table.Td ta="right">{i.unit_cost_at_send}</Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Card>
  )
}

function CostReportCard({ report: r }: { report: ProductionCostReport }) {
  return (
    <Card withBorder>
      <Text fw={600} mb="sm">Maliyet Raporu</Text>
      <Table mb="sm">
        <Table.Thead>
          <Table.Tr><Table.Th>Maliyet Unsuru</Table.Th><Table.Th ta="right">Miktar</Table.Th><Table.Th ta="right">Birim Fiyat</Table.Th><Table.Th ta="right">Tutar</Table.Th></Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {r.lines.map((l, i) => (
            <Table.Tr key={i}>
              <Table.Td>{l.label}</Table.Td>
              <Table.Td ta="right">{l.qty ?? '—'}</Table.Td>
              <Table.Td ta="right">{l.unit_price ?? '—'}</Table.Td>
              <Table.Td ta="right">{l.amount} €</Table.Td>
            </Table.Tr>
          ))}
          <Table.Tr><Table.Td colSpan={3} fw={700}>Toplam Maliyet</Table.Td><Table.Td ta="right" fw={700}>{r.total_cost} €</Table.Td></Table.Tr>
        </Table.Tbody>
      </Table>
      <SimpleGrid cols={{ base: 2, md: 4 }}>
        <Stat label="EKP (birim)" value={`${r.ekp_display} €`} />
        <Stat label="EKP (kg başına)" value={r.ekp_per_kg ? `${r.ekp_per_kg} €` : '—'} />
        <Stat label="Verim (girdi kg)" value={r.yield_input_pct ? `%${r.yield_input_pct}` : '—'} />
        <Stat label="Verim (et oranı)" value={r.yield_meat_ratio ?? '—'} />
        <Stat label="Ciro" value={r.revenue ? `${r.revenue} €` : '—'} />
        <Stat label="Brüt Kâr (toplam)" value={r.gross_profit_total ? `${r.gross_profit_total} €` : '—'} />
        <Stat label="Marj" value={r.margin_pct ? `%${r.margin_pct}` : '—'} />
        <Stat label="Planlanan/Teslim Farkı" value={`%${r.qty_variance_pct}`} />
      </SimpleGrid>
    </Card>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <Text size="xs" c="dimmed">{label}</Text>
      <Text fw={600}>{value}</Text>
    </div>
  )
}

function MovementsTimeline({ movements }: { movements: StockMovement[] }) {
  return (
    <Card withBorder>
      <Text fw={600} mb="sm">Hareket Zaman Çizelgesi</Text>
      <Timeline bulletSize={14} lineWidth={2}>
        {movements.slice().reverse().map((m) => (
          <Timeline.Item key={m.id} title={`${MOVEMENT_LABEL[m.movement_type] ?? m.movement_type} — ${m.lot_no}`}>
            <Text c="dimmed" size="sm">{m.qty} kg/adet, birim maliyet {m.unit_cost} €</Text>
            <Text size="xs" c="dimmed">{new Date(m.created_at).toLocaleString('tr-TR')}</Text>
          </Timeline.Item>
        ))}
      </Timeline>
    </Card>
  )
}

// ---------------------------------------------------------------------------
// Formlar
// ---------------------------------------------------------------------------

function SendMaterialsForm({ orderId, externalWarehouseId, onDone }: { orderId: string; externalWarehouseId: string; onDone: () => void }) {
  const [lines, setLines] = useState<{ lotId: string; qty: number | '' }[]>([{ lotId: '', qty: '' }])
  const [formError, setFormError] = useState<string | null>(null)

  const lots = useQuery({
    queryKey: ['lots', 'active'],
    queryFn: () => apiGet<List<Lot>>('/lots', { status: 'active' }),
  })

  const send = useMutation({
    mutationFn: () =>
      apiPost(`/production-orders/${orderId}/send-materials`, {
        lines: lines.filter((l) => l.lotId && l.qty !== '').map((l) => ({ lot_id: l.lotId, qty: String(l.qty) })),
      }),
    onSuccess: () => {
      setLines([{ lotId: '', qty: '' }])
      onDone()
    },
    onError: (e) => setFormError(err(e)),
  })

  const lotOptions = (lots.data?.items ?? []).map((l) => ({
    value: l.id, label: `${l.lot_no} — ${l.item_name} (${l.qty_on_hand ?? '0'} ${l.uom} mevcut)`,
  }))

  return (
    <Card withBorder>
      <Text fw={600} mb="sm">Hammadde Sevk Et{' '}
        <Text span c="dimmed" size="sm">(→ {externalWarehouseId ? 'dış üretici deposu' : ''})</Text>
      </Text>
      {formError && <Alert color="red" mb="sm">{formError}</Alert>}
      <Stack gap="xs">
        {lines.map((line, idx) => (
          <Group key={idx} wrap="nowrap">
            <Select
              placeholder="Aktif lot seçin"
              data={lotOptions}
              value={line.lotId}
              onChange={(v) => setLines((ls) => ls.map((l, i) => (i === idx ? { ...l, lotId: v ?? '' } : l)))}
              style={{ flex: 1 }}
              searchable
            />
            <NumberInput
              placeholder="Miktar"
              min={0}
              decimalScale={3}
              value={line.qty}
              onChange={(v) => setLines((ls) => ls.map((l, i) => (i === idx ? { ...l, qty: v as number } : l)))}
              w={140}
            />
            <ActionIcon color="red" variant="subtle" onClick={() => setLines((ls) => ls.filter((_, i) => i !== idx))} disabled={lines.length === 1}>
              <IconTrash size={16} />
            </ActionIcon>
          </Group>
        ))}
        <Group justify="space-between">
          <Button variant="light" leftSection={<IconPlus size={16} />} onClick={() => setLines((ls) => [...ls, { lotId: '', qty: '' }])}>
            Satır Ekle
          </Button>
          <Button loading={send.isPending} onClick={() => send.mutate()}>Sevk Et</Button>
        </Group>
      </Stack>
    </Card>
  )
}

function ReceiveForm({ orderId, onDone }: { orderId: string; onDone: () => void }) {
  const [receivedQty, setReceivedQty] = useState<number | ''>('')
  const [outputLotNo, setOutputLotNo] = useState('')
  const [serviceInvoiceNo, setServiceInvoiceNo] = useState('')
  const [serviceInvoicedQty, setServiceInvoicedQty] = useState<number | ''>('')
  const [formError, setFormError] = useState<string | null>(null)

  const receive = useMutation({
    mutationFn: () =>
      apiPost(`/production-orders/${orderId}/receive`, {
        received_qty: String(receivedQty),
        output_lot_no: outputLotNo || undefined,
        service_invoice_no: serviceInvoiceNo || undefined,
        service_invoiced_qty: serviceInvoicedQty === '' ? undefined : String(serviceInvoicedQty),
      }),
    onSuccess: onDone,
    onError: (e) => setFormError(err(e)),
  })

  return (
    <Card withBorder>
      <Text fw={600} mb="sm">Mamulü Teslim Al</Text>
      {formError && <Alert color="red" mb="sm">{formError}</Alert>}
      <Group grow mb="sm">
        <NumberInput label="Teslim Alınan Adet" required min={0} decimalScale={3} value={receivedQty} onChange={(v) => setReceivedQty(v as number)} />
        <TextInput label="Çıktı Lot No (opsiyonel)" placeholder="SUC-2026-NNN" value={outputLotNo} onChange={(e) => setOutputLotNo(e.currentTarget.value)} />
      </Group>
      <Group grow mb="md">
        <TextInput label="Hizmet Fatura No (opsiyonel)" value={serviceInvoiceNo} onChange={(e) => setServiceInvoiceNo(e.currentTarget.value)} />
        <NumberInput label="Faturalanan Adet (opsiyonel)" min={0} decimalScale={3} value={serviceInvoicedQty} onChange={(v) => setServiceInvoicedQty(v as number)} />
      </Group>
      <Group justify="flex-end">
        <Button loading={receive.isPending} disabled={receivedQty === ''} onClick={() => receive.mutate()}>Teslim Al</Button>
      </Group>
    </Card>
  )
}

function CloseForm({ orderId, invoiceSet, onDone }: { orderId: string; invoiceSet: boolean; onDone: () => void }) {
  const [serviceInvoiceNo, setServiceInvoiceNo] = useState('')
  const [formError, setFormError] = useState<string | null>(null)

  const close = useMutation({
    mutationFn: () => apiPost(`/production-orders/${orderId}/close`, serviceInvoiceNo ? { service_invoice_no: serviceInvoiceNo } : undefined),
    onSuccess: onDone,
    onError: (e) => setFormError(err(e)),
  })

  return (
    <Card withBorder>
      <Text fw={600} mb="sm">Siparişi Kapat</Text>
      {formError && <Alert color="red" mb="sm">{formError}</Alert>}
      {!invoiceSet && (
        <TextInput
          label="Hizmet Fatura No (zorunlu — teslimde girilmedi)"
          value={serviceInvoiceNo}
          onChange={(e) => setServiceInvoiceNo(e.currentTarget.value)}
          mb="sm"
        />
      )}
      <Group justify="flex-end">
        <Tooltip label="Fatura numarası olmadan kapatılamaz" disabled={invoiceSet || !!serviceInvoiceNo}>
          <Button loading={close.isPending} disabled={!invoiceSet && !serviceInvoiceNo} onClick={() => close.mutate()}>
            Kapat
          </Button>
        </Tooltip>
      </Group>
    </Card>
  )
}
