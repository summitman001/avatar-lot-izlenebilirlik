import {
  ActionIcon, Alert, Badge, Button, Group, Loader, Modal, NumberInput, Select, Table, Text,
  TextInput, Tooltip,
} from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { IconAlertTriangle, IconCash, IconFileInvoice, IconPlus, IconTruckDelivery } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { FilterChips, PageHeader, StatCard, StatGrid, SUMMARY_LIMIT, eur } from '../components/Page'
import {
  ApiError, apiGet, apiPost, type CreatePurchaseReceiptRequest, type Item, type List,
  type PurchaseReceiptListItem, type Supplier, type Warehouse,
} from '../lib/api'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

export default function Purchases() {
  const navigate = useNavigate()
  const [supplierId, setSupplierId] = useState<string | null>(null)
  const [onlyMismatch, setOnlyMismatch] = useState<string | null>(null)
  const [opened, { open, close }] = useDisclosure(false)

  const suppliers = useQuery({ queryKey: ['suppliers'], queryFn: () => apiGet<List<Supplier>>('/suppliers') })
  const { data, isLoading, error } = useQuery({
    queryKey: ['purchase-receipts', supplierId, onlyMismatch],
    queryFn: () =>
      apiGet<List<PurchaseReceiptListItem>>('/purchase-receipts', {
        supplier_id: supplierId ?? undefined,
        has_mismatch: onlyMismatch ?? undefined,
      }),
    refetchInterval: 10_000,
  })
  const all = useQuery({
    queryKey: ['purchase-receipts', 'summary'],
    queryFn: () => apiGet<List<PurchaseReceiptListItem>>('/purchase-receipts', { limit: String(SUMMARY_LIMIT) }),
    refetchInterval: 10_000,
  })
  const receipts = all.data?.items
  const mismatches = receipts?.filter((r) => r.has_mismatch).length
  const n = (v?: number) => (all.isLoading ? '…' : v ?? 0)

  return (
    <>
      <PageHeader eyebrow="Tedarikçiden gelen hammadde ve malzeme belgeleri" title="Alışlar"
        actions={<Button leftSection={<IconPlus size={16} />} onClick={open}>Yeni Alış</Button>} />
      <StatGrid capped={receipts?.length === SUMMARY_LIMIT}>
        <StatCard label="Alış belgesi" value={n(receipts?.length)} icon={IconFileInvoice} />
        <StatCard label="Toplam tutar" icon={IconCash}
          value={receipts ? eur(receipts.reduce((sum, r) => sum + Number(r.total_amount), 0)) : '…'} />
        <StatCard label="Tutar uyuşmazlığı" value={n(mismatches)} icon={IconAlertTriangle} color={mismatches ? 'red' : 'gray'} />
        <StatCard label="Tedarikçi" value={n(receipts && new Set(receipts.map((r) => r.supplier_name)).size)} icon={IconTruckDelivery} color="orange" />
      </StatGrid>
      <Group justify="space-between" mb="md">
        <FilterChips value={onlyMismatch} onChange={setOnlyMismatch} allCount={receipts?.length}
          options={[
            { value: 'true', label: 'Uyuşmazlıklı', count: mismatches, color: 'red' },
            { value: 'false', label: 'Uyuşmazlıksız', count: receipts && receipts.length - (mismatches ?? 0), color: 'green' },
          ]} />
        <Select radius="xl" w={240} placeholder="Tedarikçi" clearable searchable
          data={suppliers.data?.items.map((s) => ({ value: s.id, label: s.name })) ?? []}
          value={supplierId} onChange={setSupplierId} />
      </Group>
      {error && <Alert color="red">{err(error)}</Alert>}
      {isLoading && <Loader />}
      {data && (
        <Table striped highlightOnHover withTableBorder>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Belge No</Table.Th>
              <Table.Th>Tarih</Table.Th>
              <Table.Th>Tedarikçi</Table.Th>
              <Table.Th>Fatura No</Table.Th>
              <Table.Th ta="right">Satır</Table.Th>
              <Table.Th ta="right">Toplam Tutar</Table.Th>
              <Table.Th>Durum</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {data.items.map((r) => (
              <Table.Tr key={r.id} onClick={() => navigate(`/alislar/${r.id}`)} style={{ cursor: 'pointer' }}>
                <Table.Td>{r.doc_no}</Table.Td>
                <Table.Td>{r.received_at}</Table.Td>
                <Table.Td>{r.supplier_name}</Table.Td>
                <Table.Td>{r.invoice_no ?? '—'}</Table.Td>
                <Table.Td ta="right">{r.line_count}</Table.Td>
                <Table.Td ta="right">{r.total_amount} €</Table.Td>
                <Table.Td>
                  {r.has_mismatch && (
                    <Tooltip label="En az bir satırda hesaplanan tutar beyan edilenden farklı">
                      <Badge color="red" variant="filled">Tutar uyuşmazlığı</Badge>
                    </Tooltip>
                  )}
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
      <NewReceiptModal opened={opened} onClose={close} />
    </>
  )
}

interface LineForm {
  key: number
  lotNo: string
  itemId: string | null
  warehouseId: string | null
  qty: number | ''
  unitPrice: number | ''
  declaredLineTotal: number | ''
}

let nextKey = 1
function emptyLine(): LineForm {
  return { key: nextKey++, lotNo: '', itemId: null, warehouseId: null, qty: '', unitPrice: '', declaredLineTotal: '' }
}

function NewReceiptModal({ opened, onClose }: { opened: boolean; onClose: () => void }) {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [supplierId, setSupplierId] = useState<string | null>(null)
  const [invoiceNo, setInvoiceNo] = useState('')
  const [docNo, setDocNo] = useState('')
  const [receivedAt, setReceivedAt] = useState(() => new Date().toISOString().slice(0, 10))
  const [lines, setLines] = useState<LineForm[]>([emptyLine()])
  const [formError, setFormError] = useState<string | null>(null)

  const suppliers = useQuery({ queryKey: ['suppliers'], queryFn: () => apiGet<List<Supplier>>('/suppliers') })
  const items = useQuery({ queryKey: ['items', 'active'], queryFn: () => apiGet<List<Item>>('/items', { is_active: 'true' }) })
  const warehouses = useQuery({ queryKey: ['warehouses'], queryFn: () => apiGet<List<Warehouse>>('/warehouses') })

  const reset = () => {
    setSupplierId(null); setInvoiceNo(''); setDocNo(''); setLines([emptyLine()]); setFormError(null)
  }

  const create = useMutation({
    mutationFn: () => {
      const body: CreatePurchaseReceiptRequest = {
        supplier_id: supplierId!,
        doc_no: docNo || undefined,
        invoice_no: invoiceNo || undefined,
        received_at: receivedAt,
        lines: lines.map((l) => ({
          lot_no: l.lotNo,
          item_id: l.itemId!,
          warehouse_id: l.warehouseId!,
          qty: String(l.qty),
          unit_price: String(l.unitPrice),
          declared_line_total: l.declaredLineTotal === '' ? undefined : String(l.declaredLineTotal),
        })),
      }
      return apiPost('/purchase-receipts', body)
    },
    onSuccess: (receipt) => {
      qc.invalidateQueries({ queryKey: ['purchase-receipts'] })
      reset()
      onClose()
      navigate(`/alislar/${(receipt as { id: string }).id}`)
    },
    onError: (e) => setFormError(err(e)),
  })

  const itemOptions = items.data?.items.map((i) => ({ value: i.id, label: `${i.name} (${i.code})` })) ?? []
  const warehouseOptions = warehouses.data?.items.map((w) => ({ value: w.id, label: `${w.name} (${w.code})` })) ?? []

  const linesValid = lines.length > 0 && lines.every((l) => l.lotNo && l.itemId && l.warehouseId && l.qty !== '' && l.unitPrice !== '')
  const canSubmit = !!supplierId && !!receivedAt && linesValid

  const updateLine = (key: number, patch: Partial<LineForm>) =>
    setLines((ls) => ls.map((l) => (l.key === key ? { ...l, ...patch } : l)))

  return (
    <Modal opened={opened} onClose={() => { reset(); onClose() }} title="Yeni Alış" size="xl">
      {formError && <Alert color="red" mb="sm">{formError}</Alert>}
      <Group grow mb="sm">
        <Select
          label="Tedarikçi"
          placeholder="Tedarikçi seçin"
          required
          searchable
          data={suppliers.data?.items.map((s) => ({ value: s.id, label: s.name })) ?? []}
          value={supplierId}
          onChange={setSupplierId}
        />
        <TextInput label="Fatura No (opsiyonel)" value={invoiceNo} onChange={(e) => setInvoiceNo(e.currentTarget.value)} />
      </Group>
      <Group grow mb="md">
        <TextInput type="date" label="Tarih" required value={receivedAt} onChange={(e) => setReceivedAt(e.currentTarget.value)} />
        <TextInput label="Belge No (opsiyonel, boşsa AF-YYYY-NNN üretilir)" value={docNo} onChange={(e) => setDocNo(e.currentTarget.value)} />
      </Group>

      <Text fw={500} mb="xs">Satırlar</Text>
      <Table mb="sm">
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Lot No</Table.Th>
            <Table.Th>Ürün</Table.Th>
            <Table.Th>Depo</Table.Th>
            <Table.Th ta="right">Miktar</Table.Th>
            <Table.Th ta="right">Birim Fiyat</Table.Th>
            <Table.Th ta="right">Beyan Edilen Tutar</Table.Th>
            <Table.Th></Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {lines.map((l) => (
            <Table.Tr key={l.key}>
              <Table.Td>
                <TextInput placeholder="KZ-2026-NNN" value={l.lotNo} onChange={(e) => updateLine(l.key, { lotNo: e.currentTarget.value })} w={130} />
              </Table.Td>
              <Table.Td>
                <Select placeholder="Ürün" searchable data={itemOptions} value={l.itemId} onChange={(v) => updateLine(l.key, { itemId: v })} w={190} />
              </Table.Td>
              <Table.Td>
                <Select placeholder="Depo" data={warehouseOptions} value={l.warehouseId} onChange={(v) => updateLine(l.key, { warehouseId: v })} w={150} />
              </Table.Td>
              <Table.Td>
                <NumberInput min={0} decimalScale={3} value={l.qty} onChange={(v) => updateLine(l.key, { qty: v as number })} w={100} ml="auto" />
              </Table.Td>
              <Table.Td>
                <NumberInput min={0} decimalScale={4} value={l.unitPrice} onChange={(v) => updateLine(l.key, { unitPrice: v as number })} w={110} ml="auto" />
              </Table.Td>
              <Table.Td>
                <NumberInput min={0} decimalScale={4} placeholder="—" value={l.declaredLineTotal} onChange={(v) => updateLine(l.key, { declaredLineTotal: v as number })} w={120} ml="auto" />
              </Table.Td>
              <Table.Td>
                <ActionIcon color="red" variant="subtle" disabled={lines.length === 1} onClick={() => setLines((ls) => ls.filter((x) => x.key !== l.key))}>
                  ✕
                </ActionIcon>
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
      <Button variant="light" size="xs" mb="md" onClick={() => setLines((ls) => [...ls, emptyLine()])}>
        + Satır Ekle
      </Button>

      <Group justify="flex-end">
        <Button variant="default" onClick={() => { reset(); onClose() }}>Vazgeç</Button>
        <Button loading={create.isPending} disabled={!canSubmit} onClick={() => create.mutate()}>
          Oluştur
        </Button>
      </Group>
    </Modal>
  )
}
