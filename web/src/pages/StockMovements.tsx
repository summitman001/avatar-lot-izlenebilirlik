import { Alert, Badge, Group, Loader, Select, Table, Text } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { PageHeader } from '../components/Page'
import { apiGet, type List, type StockMovement, type Warehouse } from '../lib/api'
import { qty, unitCost } from '../lib/format'
import { refTypeLabel } from '../lib/messages'

// Lot hareketleri raporu (Bölüm 6: "en azından … lot hareketleri … raporları gösterilmelidir";
// Görev 1: "tüm maliyet ve stok hareketleri geçmişe dönük incelenebilir olmalıdır").
// Değiştirilemez defterin (stock_movement) okuma görünümü; miktar ve maliyet string gösterilir.

export const MOVEMENT_LABEL: Record<string, string> = {
  purchase_in: 'Alış girişi',
  send_to_producer: 'Dış üreticiye sevk',
  production_consume: 'Üretimde tüketim',
  production_in: 'Üretim girişi',
  cutting_consume: 'Parçalamada tüketim',
  cutting_in: 'Parçalama girişi',
  transfer_out: 'Transfer çıkışı',
  transfer_in: 'Transfer girişi',
  sale_out: 'Satış',
  return_in: 'İade girişi',
  waste_out: 'Fire',
  reprocess_consume: 'Yeniden işlemede tüketim',
  reprocess_in: 'Yeniden işleme girişi',
  adjustment: 'Düzeltme',
  reversal: 'Ters kayıt',
}

export default function StockMovements() {
  const [params, setParams] = useSearchParams()
  const lotId = params.get('lot_id') ?? undefined
  const movementType = params.get('movement_type')
  const warehouseId = params.get('warehouse_id')
  const setParam = (key: string, value: string | null) => {
    const next = new URLSearchParams(params)
    if (value) next.set(key, value)
    else next.delete(key)
    setParams(next)
  }

  const warehouses = useQuery({ queryKey: ['warehouses'], queryFn: () => apiGet<List<Warehouse>>('/warehouses') })
  const whCode = (id: string) => warehouses.data?.items.find((w) => w.id === id)?.code ?? id.slice(0, 8)
  const { data, isLoading, error } = useQuery({
    queryKey: ['stock-movements', lotId, movementType, warehouseId],
    queryFn: () => apiGet<List<StockMovement>>('/stock-movements', {
      lot_id: lotId, movement_type: movementType ?? undefined, warehouse_id: warehouseId ?? undefined, limit: '500',
    }),
  })

  return (
    <>
      <PageHeader eyebrow="Değiştirilemez stok defteri: her giriş, çıkış ve transfer lot bazında" title="Stok Hareketleri"
        badges={lotId && (
          <Badge size="lg" variant="light" rightSection={
            <Text span size="xs" style={{ cursor: 'pointer' }} onClick={() => setParam('lot_id', null)}>✕</Text>
          }>
            Lot: {data?.items[0]?.lot_no ?? lotId.slice(0, 8)}
          </Badge>
        )} />
      <Group justify="flex-end" mb="md" gap="xs">
        <Select radius="xl" placeholder="Hareket tipi" clearable w={220} value={movementType}
          onChange={(v) => setParam('movement_type', v)}
          data={Object.entries(MOVEMENT_LABEL).map(([value, label]) => ({ value, label }))} />
        <Select radius="xl" placeholder="Depo" clearable w={200} value={warehouseId}
          onChange={(v) => setParam('warehouse_id', v)}
          data={warehouses.data?.items.map((w) => ({ value: w.id, label: `${w.name} (${w.code})` })) ?? []} />
      </Group>
      {error && <Alert color="red">{error.message}</Alert>}
      {isLoading && <Loader />}
      {data && (
        <Table striped highlightOnHover withTableBorder>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Tarih</Table.Th><Table.Th>Lot</Table.Th><Table.Th>Depo</Table.Th><Table.Th>Hareket</Table.Th>
              <Table.Th ta="right">Miktar</Table.Th><Table.Th ta="right">Birim maliyet</Table.Th><Table.Th>Belge</Table.Th><Table.Th>Not</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {data.items.map((m) => (
              <Table.Tr key={m.id}>
                <Table.Td>{new Date(m.created_at).toLocaleString('tr-TR')}</Table.Td>
                <Table.Td>
                  <Text span size="sm" style={{ cursor: 'pointer' }} td="underline" onClick={() => setParam('lot_id', m.lot_id)}>{m.lot_no}</Text>
                </Table.Td>
                <Table.Td>{whCode(m.warehouse_id)}</Table.Td>
                <Table.Td>{MOVEMENT_LABEL[m.movement_type] ?? m.movement_type}</Table.Td>
                <Table.Td ta="right" c={m.qty.startsWith('-') ? 'red' : 'green'} style={{ whiteSpace: 'nowrap' }}>{qty(m.qty)}</Table.Td>
                <Table.Td ta="right" style={{ whiteSpace: 'nowrap' }}>{unitCost(m.unit_cost)}</Table.Td>
                <Table.Td><Text size="xs" c="dimmed">{refTypeLabel(m.ref_type)}</Text></Table.Td>
                <Table.Td><Text size="xs">{m.note ?? ''}</Text></Table.Td>
              </Table.Tr>
            ))}
            {data.items.length === 0 && (
              <Table.Tr><Table.Td colSpan={8}><Text c="dimmed" size="sm">Hareket yok</Text></Table.Td></Table.Tr>
            )}
          </Table.Tbody>
        </Table>
      )}
    </>
  )
}
