import { Alert, Badge, Button, Group, Loader, Modal, NumberInput, Select, Table, TextInput } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { IconBuildingFactory2, IconCircleCheck, IconPackageImport, IconPlus, IconTruckDelivery } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { FilterChips, PageHeader, StatCard, StatGrid, SUMMARY_LIMIT, countBy } from '../components/Page'
import { ApiError, apiGet, apiPost, type Item, type List, type ProductionOrder, type Supplier, type Warehouse } from '../lib/api'

export const STATUS: Record<ProductionOrder['status'], { label: string; color: string }> = {
  draft: { label: 'Taslak', color: 'gray' },
  materials_sent: { label: 'Sevk Edildi', color: 'blue' },
  received: { label: 'Teslim Alındı', color: 'teal' },
  closed: { label: 'Kapandı', color: 'green' },
}

export default function ProductionOrders() {
  const navigate = useNavigate()
  const { data, isLoading, error } = useQuery({
    queryKey: ['production-orders'],
    queryFn: () => apiGet<List<ProductionOrder>>('/production-orders', { limit: String(SUMMARY_LIMIT) }),
    refetchInterval: 10_000,
  })
  const [opened, { open, close }] = useDisclosure(false)
  const [status, setStatus] = useState<string | null>(null)
  const byStatus = countBy(data?.items, (o) => o.status)
  const rows = data?.items.filter((o) => !status || o.status === status) ?? []
  const n = (v?: number) => (isLoading ? '…' : v ?? 0)

  return (
    <>
      <PageHeader eyebrow="Hammaddeyi dış üreticiye gönder, mamulü teslim al, maliyeti kapat" title="Dış Üretim"
        actions={<Button leftSection={<IconPlus size={16} />} onClick={open}>Yeni Sipariş</Button>} />
      <StatGrid capped={data?.items.length === SUMMARY_LIMIT}>
        <StatCard label="Toplam sipariş" value={n(data?.items.length)} icon={IconBuildingFactory2} />
        <StatCard label="Üreticide" value={n(byStatus.materials_sent)} icon={IconTruckDelivery} color="blue" />
        <StatCard label="Teslim alındı" value={n(byStatus.received)} icon={IconPackageImport} color="teal" />
        <StatCard label="Kapandı" value={n(byStatus.closed)} icon={IconCircleCheck} color="green" />
      </StatGrid>
      <Group mb="md">
        <FilterChips value={status} onChange={setStatus} allCount={data?.items.length}
          options={(Object.keys(STATUS) as ProductionOrder['status'][]).map((s) => (
            { value: s, label: STATUS[s].label, count: byStatus[s] ?? 0, color: STATUS[s].color }))} />
      </Group>
      {error && <Alert color="red">{error.message}</Alert>}
      {isLoading && <Loader />}
      {data && (
        <Table striped highlightOnHover withTableBorder>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Sipariş No</Table.Th>
              <Table.Th>Ürün</Table.Th>
              <Table.Th>Durum</Table.Th>
              <Table.Th ta="right">Planlanan</Table.Th>
              <Table.Th ta="right">Teslim Alınan</Table.Th>
              <Table.Th ta="right">EKP</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {rows.map((o) => (
              <Table.Tr key={o.id} onClick={() => navigate(`/dis-uretim/${o.id}`)} style={{ cursor: 'pointer' }}>
                <Table.Td>{o.order_no}</Table.Td>
                <Table.Td>{o.product_item_name}</Table.Td>
                <Table.Td>
                  <Badge color={STATUS[o.status].color} variant="light">{STATUS[o.status].label}</Badge>
                  {o.warnings.length > 0 && (
                    <Badge color="orange" variant="light" ml="xs">{o.warnings.join(', ')}</Badge>
                  )}
                </Table.Td>
                <Table.Td ta="right">{o.planned_qty} adet</Table.Td>
                <Table.Td ta="right">{o.received_qty ?? '—'} adet</Table.Td>
                <Table.Td ta="right">{o.ekp_unit ?? '—'}</Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
      <NewOrderModal opened={opened} onClose={close} />
    </>
  )
}

function NewOrderModal({ opened, onClose }: { opened: boolean; onClose: () => void }) {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [productItemId, setProductItemId] = useState<string | null>(null)
  const [producerSupplierId, setProducerSupplierId] = useState<string | null>(null)
  const [externalWarehouseId, setExternalWarehouseId] = useState<string | null>(null)
  const [orderNo, setOrderNo] = useState('')
  const [plannedQty, setPlannedQty] = useState<number | ''>('')
  const [serviceUnitPrice, setServiceUnitPrice] = useState<number | ''>('')
  const [salePrice, setSalePrice] = useState<number | ''>('')
  const [formError, setFormError] = useState<string | null>(null)

  const products = useQuery({
    queryKey: ['items', 'finished'],
    queryFn: () => apiGet<List<Item>>('/items', { item_type: 'finished', is_active: 'true' }),
  })
  const producers = useQuery({
    queryKey: ['suppliers', 'external_producer'],
    queryFn: () => apiGet<List<Supplier>>('/suppliers', { supplier_type: 'external_producer' }),
  })
  const warehouses = useQuery({
    queryKey: ['warehouses', 'external_producer'],
    queryFn: () => apiGet<List<Warehouse>>('/warehouses', { warehouse_type: 'external_producer' }),
  })

  const create = useMutation({
    mutationFn: () =>
      apiPost<ProductionOrder>('/production-orders', {
        order_no: orderNo || undefined,
        product_item_id: productItemId,
        producer_supplier_id: producerSupplierId,
        external_warehouse_id: externalWarehouseId,
        planned_qty: String(plannedQty),
        service_unit_price: String(serviceUnitPrice),
        sale_price: salePrice === '' ? undefined : String(salePrice),
      }),
    onSuccess: (order) => {
      qc.invalidateQueries({ queryKey: ['production-orders'] })
      onClose()
      navigate(`/dis-uretim/${order.id}`)
    },
    onError: (e) => setFormError(e instanceof ApiError ? e.message : 'Bilinmeyen hata'),
  })

  return (
    <Modal opened={opened} onClose={onClose} title="Yeni Dış Üretim Siparişi">
      {formError && <Alert color="red" mb="sm">{formError}</Alert>}
      <Select
        label="Mamul"
        placeholder="Ürün seçin"
        required
        data={products.data?.items.map((i) => ({ value: i.id, label: `${i.name} (${i.code})` })) ?? []}
        value={productItemId}
        onChange={setProductItemId}
        mb="sm"
      />
      <Select
        label="Dış Üretici"
        placeholder="Tedarikçi seçin"
        required
        data={producers.data?.items.map((s) => ({ value: s.id, label: s.name })) ?? []}
        value={producerSupplierId}
        onChange={setProducerSupplierId}
        mb="sm"
      />
      <Select
        label="Dış Üretici Deposu"
        placeholder="Depo seçin"
        required
        data={warehouses.data?.items.map((w) => ({ value: w.id, label: `${w.name} (${w.code})` })) ?? []}
        value={externalWarehouseId}
        onChange={setExternalWarehouseId}
        mb="sm"
      />
      <TextInput label="Sipariş No (opsiyonel)" placeholder="URT-2026-NNN" value={orderNo} onChange={(e) => setOrderNo(e.currentTarget.value)} mb="sm" />
      <Group grow mb="sm">
        <NumberInput label="Planlanan Adet" required min={0} decimalScale={3} value={plannedQty} onChange={(v) => setPlannedQty(v as number)} />
        <NumberInput label="Hizmet Birim Fiyatı (€/adet)" required min={0} decimalScale={4} value={serviceUnitPrice} onChange={(v) => setServiceUnitPrice(v as number)} />
      </Group>
      <NumberInput label="Satış Fiyatı / VKP (€/adet, opsiyonel)" min={0} decimalScale={4} value={salePrice} onChange={(v) => setSalePrice(v as number)} mb="md" />
      <Group justify="flex-end">
        <Button variant="default" onClick={onClose}>Vazgeç</Button>
        <Button
          loading={create.isPending}
          disabled={!productItemId || !producerSupplierId || !externalWarehouseId || plannedQty === '' || serviceUnitPrice === ''}
          onClick={() => create.mutate()}
        >
          Oluştur
        </Button>
      </Group>
    </Modal>
  )
}
