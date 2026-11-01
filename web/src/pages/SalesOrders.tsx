import { ActionIcon, Alert, Badge, Button, Checkbox, Group, Loader, Modal, NumberInput, Select, Table, Text } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { IconAlertOctagon, IconCash, IconPlus, IconReceipt, IconTrash, IconUsers } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { FilterChips, PageHeader, StatCard, StatGrid, SUMMARY_LIMIT, eur } from '../components/Page'
import { ApiError, apiGet, apiPost, type Customer, type Item, type List, type Lot, type SalesOrder, type Warehouse } from '../lib/api'
import { money, qty } from '../lib/format'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

export default function SalesOrders() {
  const navigate = useNavigate()
  const { data, isLoading, error } = useQuery({
    queryKey: ['sales-orders'],
    queryFn: () => apiGet<List<SalesOrder>>('/sales-orders', { limit: String(SUMMARY_LIMIT) }),
    refetchInterval: 10_000,
  })
  const [opened, { open, close }] = useDisclosure(false)
  const [filter, setFilter] = useState<string | null>(null)
  const orders = data?.items
  const violations = orders?.filter((o) => o.has_violation).length
  const rows = orders?.filter((o) => filter !== 'violation' || o.has_violation) ?? []
  const n = (v?: number) => (isLoading ? '…' : v ?? 0)

  return (
    <>
      <PageHeader eyebrow="Kim aldı, ne satıldı, hangi lottan sevk edildi" title="Satış / Sevkiyat"
        actions={<Button leftSection={<IconPlus size={16} />} onClick={open}>Yeni Satış</Button>} />
      <StatGrid capped={orders?.length === SUMMARY_LIMIT}>
        <StatCard label="Sipariş" value={n(orders?.length)} icon={IconReceipt} />
        <StatCard label="Toplam tutar" icon={IconCash}
          value={orders ? eur(orders.reduce((sum, o) => sum + Number(o.total_amount), 0)) : '…'} />
        <StatCard label="Müşteri" value={n(orders && new Set(orders.map((o) => o.customer_id)).size)} icon={IconUsers} color="blue" />
        <StatCard label="Kural ihlali" value={n(violations)} icon={IconAlertOctagon} color={violations ? 'red' : 'gray'} />
      </StatGrid>
      <Group mb="md">
        <FilterChips value={filter} onChange={setFilter} allCount={orders?.length}
          options={[{ value: 'violation', label: 'İhlalli', count: violations, color: 'red' }]} />
      </Group>
      {error && <Alert color="red">{error.message}</Alert>}
      {isLoading && <Loader />}
      {data && (
        <Table striped highlightOnHover withTableBorder>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Sipariş No</Table.Th>
              <Table.Th>Müşteri</Table.Th>
              <Table.Th>Tarih</Table.Th>
              <Table.Th ta="right">Tutar</Table.Th>
              <Table.Th>Durum</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {rows.map((o) => (
              <Table.Tr key={o.id} onClick={() => navigate(`/satis-sevkiyat/${o.id}`)} style={{ cursor: 'pointer' }}>
                <Table.Td>{o.order_no}</Table.Td>
                <Table.Td>{o.customer_name}</Table.Td>
                <Table.Td>{new Date(o.sold_at).toLocaleString('tr-TR')}</Table.Td>
                <Table.Td ta="right" style={{ whiteSpace: 'nowrap' }}>{money(o.total_amount)}</Table.Td>
                <Table.Td>
                  <Badge color="teal" variant="light">Onaylandı</Badge>
                  {o.has_violation && <Badge color="red" variant="filled" ml="xs">İhlal</Badge>}
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
      <NewSaleModal opened={opened} onClose={close} />
    </>
  )
}

interface DraftLine {
  itemId: string
  lotId: string
  qty: number | ''
  unitPrice: number | ''
}

function NewSaleModal({ opened, onClose }: { opened: boolean; onClose: () => void }) {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [customerId, setCustomerId] = useState<string | null>(null)
  const [warehouseId, setWarehouseId] = useState<string | null>(null)
  const [createShipment, setCreateShipment] = useState(true)
  const [lines, setLines] = useState<DraftLine[]>([{ itemId: '', lotId: '', qty: '', unitPrice: '' }])
  const [formError, setFormError] = useState<string | null>(null)

  const customers = useQuery({ queryKey: ['customers'], queryFn: () => apiGet<List<Customer>>('/customers') })
  const warehouses = useQuery({ queryKey: ['warehouses', 'internal'], queryFn: () => apiGet<List<Warehouse>>('/warehouses', { warehouse_type: 'internal' }) })
  const items = useQuery({ queryKey: ['items', 'finished'], queryFn: () => apiGet<List<Item>>('/items', { item_type: 'finished', is_active: 'true' }) })
  const activeLots = useQuery({
    queryKey: ['lots', 'active-for-sale'],
    queryFn: () => apiGet<List<Lot>>('/lots', { status: 'active' }),
  })

  const create = useMutation({
    mutationFn: () =>
      apiPost<SalesOrder>('/sales-orders', {
        customer_id: customerId,
        warehouse_id: warehouseId,
        sold_at: new Date().toISOString(),
        create_shipment: createShipment,
        lines: lines
          .filter((l) => l.itemId && l.lotId && l.qty !== '' && l.unitPrice !== '')
          .map((l) => ({ item_id: l.itemId, lot_id: l.lotId, qty: String(l.qty), unit_price: String(l.unitPrice) })),
      }),
    onSuccess: (order) => {
      qc.invalidateQueries({ queryKey: ['sales-orders'] })
      onClose()
      navigate(`/satis-sevkiyat/${order.id}`)
    },
    onError: (e) => setFormError(err(e)),
  })

  const itemOptions = (items.data?.items ?? []).map((i) => ({ value: i.id, label: `${i.name} (${i.code})` }))
  const lotsForItem = (itemId: string) =>
    (activeLots.data?.items ?? []).filter((l) => l.item_id === itemId).map((l) => ({
      value: l.id, label: `${l.lot_no} (${qty(l.qty_on_hand ?? '0', l.uom)} mevcut)`,
    }))

  const canSubmit =
    !!customerId && !!warehouseId &&
    lines.every((l) => l.itemId && l.lotId && l.qty !== '' && l.unitPrice !== '')

  return (
    <Modal opened={opened} onClose={onClose} title="Yeni Satış" size="lg">
      {formError && <Alert color="red" mb="sm">{formError}</Alert>}
      <Group grow mb="sm">
        <Select
          label="Müşteri" placeholder="Müşteri seçin" required searchable
          data={customers.data?.items.map((c) => ({ value: c.id, label: `${c.name} (${c.code})` })) ?? []}
          value={customerId} onChange={setCustomerId}
        />
        <Select
          label="Depo" placeholder="Depo seçin" required
          data={warehouses.data?.items.map((w) => ({ value: w.id, label: `${w.name} (${w.code})` })) ?? []}
          value={warehouseId} onChange={setWarehouseId}
        />
      </Group>
      <Text fw={500} size="sm" mb="xs">Satış Satırları (yalnız aktif lotlar listelenir)</Text>
      {lines.map((line, idx) => (
        <Group key={idx} mb="xs" wrap="nowrap">
          <Select
            placeholder="Ürün" data={itemOptions} style={{ flex: 1.2 }}
            value={line.itemId || null}
            onChange={(v) => setLines((ls) => ls.map((l, i) => (i === idx ? { ...l, itemId: v ?? '', lotId: '' } : l)))}
          />
          <Select
            placeholder="Aktif lot" data={lotsForItem(line.itemId)} style={{ flex: 1.2 }} disabled={!line.itemId}
            value={line.lotId || null}
            onChange={(v) => setLines((ls) => ls.map((l, i) => (i === idx ? { ...l, lotId: v ?? '' } : l)))}
          />
          <NumberInput
            placeholder="Miktar" min={0} decimalScale={3} w={110}
            value={line.qty} onChange={(v) => setLines((ls) => ls.map((l, i) => (i === idx ? { ...l, qty: v as number } : l)))}
          />
          <NumberInput
            placeholder="Birim Fiyat" min={0} decimalScale={4} w={120}
            value={line.unitPrice} onChange={(v) => setLines((ls) => ls.map((l, i) => (i === idx ? { ...l, unitPrice: v as number } : l)))}
          />
          <ActionIcon color="red" variant="subtle" disabled={lines.length === 1}
            onClick={() => setLines((ls) => ls.filter((_, i) => i !== idx))}>
            <IconTrash size={16} />
          </ActionIcon>
        </Group>
      ))}
      <Button variant="light" size="xs" leftSection={<IconPlus size={14} />} mb="md"
        onClick={() => setLines((ls) => [...ls, { itemId: '', lotId: '', qty: '', unitPrice: '' }])}>
        Satır Ekle
      </Button>
      <Checkbox label="Sevkiyat da oluştur" checked={createShipment} onChange={(e) => setCreateShipment(e.currentTarget.checked)} mb="md" />
      <Group justify="flex-end">
        <Button variant="default" onClick={onClose}>Vazgeç</Button>
        <Button loading={create.isPending} disabled={!canSubmit} onClick={() => create.mutate()}>Satışı Oluştur</Button>
      </Group>
    </Modal>
  )
}
