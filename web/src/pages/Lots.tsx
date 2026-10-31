import { Alert, Badge, Button, Group, Loader, Modal, NumberInput, Select, Table, Text, Textarea, TextInput, Tooltip, UnstyledButton } from '@mantine/core'
import { useDebouncedValue, useDisclosure } from '@mantine/hooks'
import { IconAlertTriangle, IconArrowBackUp, IconLock, IconSearch, IconStack2 } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { FilterChips, PageHeader, StatCard, StatGrid, SUMMARY_LIMIT, countBy } from '../components/Page'
import { ApiError, apiGet, apiPost, type List, type Lot, type StockMovement, type StockTransfer, type Warehouse } from '../lib/api'
import { uuidv7 } from '../lib/uuid7'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

const STATUS: Record<Lot['status'], { label: string; color: string }> = {
  active: { label: 'Aktif', color: 'green' },
  blocked: { label: 'Bloke', color: 'orange' },
  recalled: { label: 'Geri çağrıldı', color: 'red' },
  consumed: { label: 'Tükendi', color: 'gray' },
}

// Alış belgesinde hesaplanan tutar ile beyan edilen tutar farklıysa (has_mismatch) uyarı rozeti.
function MismatchBadge({ lot }: { lot: Lot }) {
  const p = lot.purchase
  if (!p?.has_mismatch) return null
  return (
    <Tooltip label={`Belge ${p.doc_no}: hesaplanan ${p.line_total} ≠ beyan edilen ${p.declared_line_total}`}>
      <Badge color="red" variant="filled" ml="xs">
        Tutar uyuşmazlığı
      </Badge>
    </Tooltip>
  )
}

export default function Lots() {
  const [params] = useSearchParams()
  const qc = useQueryClient()
  const [q, setQ] = useState(params.get('q') ?? '')
  const [status, setStatus] = useState<string | null>(params.get('status')) // Dashboard: ?status=blocked
  const [onlyMismatch, setOnlyMismatch] = useState(false)
  const [debouncedQ] = useDebouncedValue(q, 300)
  const [blockTarget, setBlockTarget] = useState<Lot | null>(null)
  const [blockReason, setBlockReason] = useState('')
  const [actionError, setActionError] = useState<string | null>(null)
  const [opened, { open, close }] = useDisclosure(false)
  const [transferTarget, setTransferTarget] = useState<Lot | null>(null)
  const [wasteTarget, setWasteTarget] = useState<Lot | null>(null)

  const { data, isLoading, error } = useQuery({
    queryKey: ['lots', debouncedQ, status, onlyMismatch],
    queryFn: () =>
      apiGet<List<Lot>>('/lots', {
        q: debouncedQ,
        status: status ?? undefined,
        has_mismatch: onlyMismatch ? 'true' : undefined,
      }),
  })

  const all = useQuery({
    queryKey: ['lots', 'summary'],
    queryFn: () => apiGet<List<Lot>>('/lots', { limit: String(SUMMARY_LIMIT) }),
  })
  const lots = all.data?.items
  const byStatus = countBy(lots, (l) => l.status)
  const mismatchCount = lots?.filter((l) => l.purchase?.has_mismatch).length
  const n = (v?: number) => (all.isLoading ? '…' : v ?? 0)

  const invalidate = () => qc.invalidateQueries({ queryKey: ['lots'] })

  const block = useMutation({
    mutationFn: (lot: Lot) => apiPost(`/lots/${lot.id}/block`, { reason: blockReason }),
    onSuccess: () => { invalidate(); close(); setBlockReason('') },
    onError: (e) => setActionError(err(e)),
  })
  const unblock = useMutation({
    mutationFn: (lot: Lot) => apiPost(`/lots/${lot.id}/unblock`),
    onSuccess: invalidate,
    onError: (e) => setActionError(err(e)),
  })

  return (
    <>
      <PageHeader eyebrow="Her lotun kaynağı, eldeki miktarı ve durumu" title="Lotlar" />
      <StatGrid capped={lots?.length === SUMMARY_LIMIT}>
        <StatCard label="Toplam lot" value={n(lots?.length)} icon={IconStack2} />
        <StatCard label="Aktif" value={n(byStatus.active)} icon={IconStack2} color="green" />
        <StatCard label="Bloke" value={n(byStatus.blocked)} icon={IconLock} color="orange" />
        <StatCard label="Geri çağrılan" value={n(byStatus.recalled)} icon={IconArrowBackUp} color="red" />
      </StatGrid>
      <Group justify="space-between" mb="md">
        <FilterChips value={status} onChange={setStatus} allCount={lots?.length}
          options={(Object.keys(STATUS) as Lot['status'][]).map((s) => (
            { value: s, label: STATUS[s].label, count: byStatus[s] ?? 0, color: STATUS[s].color }))} />
        <Group gap="xs">
          <UnstyledButton className="filter-chip" data-active={onlyMismatch || undefined} aria-pressed={onlyMismatch}
            onClick={() => setOnlyMismatch((v) => !v)}>
            <IconAlertTriangle size={14} color="var(--mantine-color-red-filled)" />
            Tutar uyuşmazlığı
            {mismatchCount != null && <span className="chip-count">{mismatchCount}</span>}
          </UnstyledButton>
          <TextInput radius="xl" w={240} leftSection={<IconSearch size={16} />} placeholder="Lot no veya ürün ara"
            value={q} onChange={(e) => setQ(e.currentTarget.value)} />
        </Group>
      </Group>
      {error && <Alert color="red">{error.message}</Alert>}
      {actionError && <Alert color="red" mb="sm" onClose={() => setActionError(null)} withCloseButton>{actionError}</Alert>}
      {isLoading && <Loader />}
      {data && (
        <Table striped highlightOnHover withTableBorder>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Lot no</Table.Th>
              <Table.Th>Ürün</Table.Th>
              <Table.Th>Tedarikçi</Table.Th>
              <Table.Th>Giriş tarihi</Table.Th>
              <Table.Th ta="right">İlk miktar</Table.Th>
              <Table.Th ta="right">Eldeki miktar</Table.Th>
              <Table.Th ta="right">Birim maliyet</Table.Th>
              <Table.Th>Durum</Table.Th>
              <Table.Th></Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {data.items.map((lot) => (
              <Table.Tr key={lot.id}>
                <Table.Td>
                  {lot.lot_no}
                  <MismatchBadge lot={lot} />
                </Table.Td>
                <Table.Td>
                  {lot.item_name} <Text span size="xs" c="dimmed">({lot.item_code})</Text>
                </Table.Td>
                <Table.Td>{lot.supplier_name ?? '—'}</Table.Td>
                <Table.Td>{lot.received_or_produced_at}</Table.Td>
                {/* Miktar ve maliyet string olarak gösterilir; Number'a çevrilmez. */}
                <Table.Td ta="right">{lot.initial_qty} {lot.uom}</Table.Td>
                <Table.Td ta="right">{lot.qty_on_hand ?? '—'} {lot.uom}</Table.Td>
                <Table.Td ta="right">{lot.unit_cost}</Table.Td>
                <Table.Td>
                  <Badge color={STATUS[lot.status].color} variant="light">{STATUS[lot.status].label}</Badge>
                  {lot.blocked_reason && (
                    <Tooltip label={lot.blocked_reason}><Text span size="xs" c="dimmed" ml="xs">(sebep)</Text></Tooltip>
                  )}
                </Table.Td>
                <Table.Td>
                  {lot.status === 'blocked' ? (
                    <Button size="xs" variant="light" color="green" loading={unblock.isPending} onClick={() => unblock.mutate(lot)}>
                      Aç
                    </Button>
                  ) : lot.status === 'active' ? (
                    <Button size="xs" variant="light" color="orange" onClick={() => { setBlockTarget(lot); open() }}>
                      Blokla
                    </Button>
                  ) : null}
                  {lot.status !== 'consumed' && Number(lot.qty_on_hand ?? '0') > 0 && (
                    <>
                      <Button size="xs" variant="light" ml="xs" onClick={() => setTransferTarget(lot)}>
                        Transfer
                      </Button>
                      <Button size="xs" variant="light" color="red" ml="xs" onClick={() => setWasteTarget(lot)}>
                        Fire
                      </Button>
                    </>
                  )}
                  <Button size="xs" variant="subtle" ml="xs" component={Link} to={`/stok-hareketleri?lot_id=${lot.id}`}>
                    Hareketler
                  </Button>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
      {transferTarget && <TransferModal lot={transferTarget} onClose={() => setTransferTarget(null)} />}
      {wasteTarget && <WasteModal lot={wasteTarget} onClose={() => setWasteTarget(null)} />}
      <Modal opened={opened} onClose={close} title={`Lotu Blokla: ${blockTarget?.lot_no ?? ''}`}>
        <Textarea label="Sebep" required value={blockReason} onChange={(e) => setBlockReason(e.currentTarget.value)} mb="md" />
        <Group justify="flex-end">
          <Button variant="default" onClick={close}>Vazgeç</Button>
          <Button color="orange" loading={block.isPending} disabled={!blockReason}
            onClick={() => blockTarget && block.mutate(blockTarget)}>
            Blokla
          </Button>
        </Group>
      </Modal>
    </>
  )
}

// Dış üretici deposundaki stok üretim emrine aittir (yalnızca sevk/teslim/iade): transfer/fire kaynağı
// olamaz, sunucu da reddeder. Depolar yüklenmeden liste boş kalır.
function ownBalances(lot: Lot | undefined, warehouses: Warehouse[] | undefined) {
  if (!warehouses) return []
  const external = new Set(warehouses.filter((w) => w.warehouse_type === 'external_producer').map((w) => w.id))
  return (lot?.balances ?? []).filter((b) => Number(b.qty_on_hand) > 0 && !external.has(b.warehouse_id))
}

// Depolar arasi transfer: kaynak depo lotun bakiyeli depolarindan secilir. Bloke/geri cagrilmis lot
// yalnizca karantina deposuna tasinabilir (sunucu da zorlar).
function TransferModal({ lot, onClose }: { lot: Lot; onClose: () => void }) {
  const qc = useQueryClient()
  const detail = useQuery({ queryKey: ['lot', lot.id], queryFn: () => apiGet<Lot>(`/lots/${lot.id}`) })
  const warehouses = useQuery({ queryKey: ['warehouses'], queryFn: () => apiGet<List<Warehouse>>('/warehouses') })
  const [fromId, setFromId] = useState<string | null>(null)
  const [toId, setToId] = useState<string | null>(null)
  const [qty, setQty] = useState<number | ''>('')
  const [note, setNote] = useState('')
  const [formError, setFormError] = useState<string | null>(null)
  const [transferId] = useState(uuidv7) // idempotency anahtari

  const sources = ownBalances(detail.data, warehouses.data?.items)
  const source = sources.find((b) => b.warehouse_id === fromId)
  const targets = (warehouses.data?.items ?? [])
    .filter((w) => w.id !== fromId && w.warehouse_type !== 'external_producer')
    .filter((w) => lot.status === 'active' || w.warehouse_type === 'quarantine')

  const create = useMutation({
    mutationFn: () => apiPost<StockTransfer>('/stock-transfers', {
      id: transferId, from_warehouse_id: fromId, to_warehouse_id: toId, note: note || undefined,
      lines: [{ lot_id: lot.id, qty: String(qty) }],
    }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['lots'] }); onClose() },
    onError: (e) => setFormError(err(e)),
  })

  return (
    <Modal opened onClose={onClose} title={`Depo Transferi: ${lot.lot_no}`}>
      {formError && <Alert color="red" mb="sm">{formError}</Alert>}
      {lot.status !== 'active' && (
        <Alert color="orange" mb="sm">Lot {STATUS[lot.status].label.toLowerCase()}: yalnızca karantina deposuna taşınabilir.</Alert>
      )}
      <Select label="Kaynak depo" required mb="sm" value={fromId} onChange={setFromId}
        data={sources.map((b) => ({ value: b.warehouse_id, label: `${b.warehouse_code} (${b.qty_on_hand} ${lot.uom})` }))} />
      <Select label="Hedef depo" required mb="sm" value={toId} onChange={setToId}
        data={targets.map((w) => ({ value: w.id, label: `${w.name} (${w.code})` }))} />
      <NumberInput label={`Miktar (${lot.uom})`} required mb="sm" min={0} decimalScale={3} value={qty}
        onChange={(v) => setQty(typeof v === 'number' ? v : '')}
        description={source ? `En fazla ${source.qty_on_hand}` : undefined} />
      <TextInput label="Not" mb="md" maxLength={400} value={note} onChange={(e) => setNote(e.currentTarget.value)} />
      <Group justify="flex-end">
        <Button variant="default" onClick={onClose}>Vazgeç</Button>
        <Button loading={create.isPending} disabled={!fromId || !toId || qty === '' || qty <= 0}
          onClick={() => create.mutate()}>
          Transfer et
        </Button>
      </Group>
    </Modal>
  )
}

// Fire kaydı (Görev 3: "fire hareketleri lot bazında kaydedilir"): seçilen depodaki miktar waste_out
// hareketiyle düşülür. Geri çağrılmış/bloke lotun imhası da buradan kaydedilir.
function WasteModal({ lot, onClose }: { lot: Lot; onClose: () => void }) {
  const qc = useQueryClient()
  const detail = useQuery({ queryKey: ['lot', lot.id], queryFn: () => apiGet<Lot>(`/lots/${lot.id}`) })
  const warehouses = useQuery({ queryKey: ['warehouses'], queryFn: () => apiGet<List<Warehouse>>('/warehouses') })
  const [warehouseId, setWarehouseId] = useState<string | null>(null)
  const [qty, setQty] = useState<number | ''>('')
  const [reason, setReason] = useState('')
  const [formError, setFormError] = useState<string | null>(null)
  const [movementId] = useState(uuidv7) // idempotency anahtarı

  const sources = ownBalances(detail.data, warehouses.data?.items)
  const source = sources.find((b) => b.warehouse_id === warehouseId)
  const save = useMutation({
    mutationFn: () => apiPost<StockMovement>(`/lots/${lot.id}/waste`, {
      id: movementId, warehouse_id: warehouseId, qty: String(qty), reason,
    }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['lots'] }); onClose() },
    onError: (e) => setFormError(err(e)),
  })

  return (
    <Modal opened onClose={onClose} title={`Fire Kaydı: ${lot.lot_no}`}>
      {formError && <Alert color="red" mb="sm">{formError}</Alert>}
      <Select label="Depo" required mb="sm" value={warehouseId} onChange={setWarehouseId}
        data={sources.map((b) => ({ value: b.warehouse_id, label: `${b.warehouse_code} (${b.qty_on_hand} ${lot.uom})` }))} />
      <NumberInput label={`Fire miktarı (${lot.uom})`} required mb="sm" min={0} decimalScale={3} value={qty}
        onChange={(v) => setQty(typeof v === 'number' ? v : '')}
        description={source ? `En fazla ${source.qty_on_hand}` : undefined} />
      <TextInput label="Sebep" required mb="md" maxLength={400} placeholder="bozulma, imha, …" value={reason} onChange={(e) => setReason(e.currentTarget.value)} />
      <Group justify="flex-end">
        <Button variant="default" onClick={onClose}>Vazgeç</Button>
        <Button color="red" loading={save.isPending} disabled={!warehouseId || qty === '' || qty <= 0 || !reason.trim()}
          onClick={() => save.mutate()}>
          Fire kaydet
        </Button>
      </Group>
    </Modal>
  )
}
