import { ActionIcon, Alert, Button, Group, Loader, Modal, NumberInput, Select, Table, Text, TextInput, Title } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { IconPlus, IconTrash } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, apiGet, apiPost, type Item, type List, type Lot, type ReprocessOrder, type Warehouse } from '../lib/api'
import { uuidv7 } from '../lib/uuid7'

// Yeniden işleme (Görev 3: "yeniden işleme … hareketleri lot bazında kaydedilir"). Girdi lotları
// depodan tüketilir, yeni bir lot üretilir; maliyet aktarılır ve izleme bağlantısı kurulur.

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

export default function Reprocess() {
  const [opened, { open, close }] = useDisclosure(false)
  const { data, isLoading, error } = useQuery({
    queryKey: ['reprocess-orders'],
    queryFn: () => apiGet<List<ReprocessOrder>>('/reprocess-orders'),
  })
  return (
    <>
      <Group justify="space-between" mb="md">
        <Title order={2}>Yeniden İşleme</Title>
        <Button leftSection={<IconPlus size={16} />} onClick={open}>Yeni yeniden işleme</Button>
      </Group>
      {error && <Alert color="red">{error.message}</Alert>}
      {isLoading && <Loader />}
      {data && (
        <Table striped withTableBorder>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Emir</Table.Th><Table.Th>Tarih</Table.Th><Table.Th>Depo</Table.Th><Table.Th>Girdi lotları</Table.Th>
              <Table.Th>Çıktı lotu</Table.Th><Table.Th ta="right">Miktar</Table.Th><Table.Th ta="right">Toplam maliyet</Table.Th>
              <Table.Th ta="right">Birim maliyet</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {data.items.map((o) => (
              <Table.Tr key={o.id}>
                <Table.Td>{o.order_no}</Table.Td>
                <Table.Td>{new Date(o.created_at).toLocaleDateString('tr-TR')}</Table.Td>
                <Table.Td>{o.warehouse_code}</Table.Td>
                <Table.Td>
                  {o.inputs.map((i) => <Text key={i.lot_id} size="xs">{i.lot_no}: {i.qty} {i.uom}</Text>)}
                </Table.Td>
                <Table.Td>
                  {o.output_lot_id && <Link to={`/izlenebilirlik?lot_id=${o.output_lot_id}`}>{o.output_lot_no}</Link>}
                  <Text size="xs" c="dimmed">{o.output_item_name}</Text>
                </Table.Td>
                <Table.Td ta="right">{o.output_qty} {o.output_uom}</Table.Td>
                <Table.Td ta="right">{o.total_cost}</Table.Td>
                <Table.Td ta="right">{o.output_unit_cost}</Table.Td>
              </Table.Tr>
            ))}
            {data.items.length === 0 && (
              <Table.Tr><Table.Td colSpan={8}><Text c="dimmed" size="sm">Henüz yeniden işleme kaydı yok</Text></Table.Td></Table.Tr>
            )}
          </Table.Tbody>
        </Table>
      )}
      {opened && <NewReprocessModal onClose={close} />}
    </>
  )
}

interface InputLine { key: number; lotId: string | null; qty: number | '' }
let nextKey = 1

function NewReprocessModal({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient()
  const [warehouseId, setWarehouseId] = useState<string | null>(null)
  const [inputs, setInputs] = useState<InputLine[]>([{ key: nextKey++, lotId: null, qty: '' }])
  const [itemId, setItemId] = useState<string | null>(null)
  const [outQty, setOutQty] = useState<number | ''>('')
  const [lotNo, setLotNo] = useState('')
  const [note, setNote] = useState('')
  const [id] = useState(uuidv7) // idempotency anahtarı

  const warehouses = useQuery({ queryKey: ['warehouses'], queryFn: () => apiGet<List<Warehouse>>('/warehouses') })
  const items = useQuery({ queryKey: ['items', 'active'], queryFn: () => apiGet<List<Item>>('/items', { is_active: 'true' }) })
  const lots = useQuery({ queryKey: ['lots', 'active'], queryFn: () => apiGet<List<Lot>>('/lots', { status: 'active' }) })

  const create = useMutation({
    mutationFn: () => apiPost<ReprocessOrder>('/reprocess-orders', {
      id, warehouse_id: warehouseId, note: note || undefined,
      inputs: inputs.map((i) => ({ lot_id: i.lotId, qty: String(i.qty) })),
      output: { item_id: itemId, qty: String(outQty), lot_no: lotNo || undefined },
    }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['reprocess-orders'] }); qc.invalidateQueries({ queryKey: ['lots'] }); onClose() },
  })

  const update = (key: number, patch: Partial<InputLine>) => setInputs((ls) => ls.map((l) => (l.key === key ? { ...l, ...patch } : l)))
  const valid = !!warehouseId && !!itemId && outQty !== '' && outQty > 0 &&
    inputs.length > 0 && inputs.every((i) => i.lotId && i.qty !== '' && i.qty > 0)

  return (
    <Modal opened onClose={onClose} title="Yeni yeniden işleme" size="lg">
      {create.error && <Alert color="red" mb="sm">{err(create.error)}</Alert>}
      <Select label="Depo" required mb="sm" value={warehouseId} onChange={setWarehouseId}
        description="Girdiler bu depodan tüketilir, çıktı lotu bu depoya girer"
        data={warehouses.data?.items.filter((w) => w.warehouse_type !== 'external_producer')
          .map((w) => ({ value: w.id, label: `${w.name} (${w.code})` })) ?? []} />
      <Text fw={500} size="sm" mb="xs">Girdi lotları</Text>
      {inputs.map((l) => (
        <Group key={l.key} mb="xs" align="flex-end">
          <Select flex={1} searchable placeholder="Lot" value={l.lotId} onChange={(v) => update(l.key, { lotId: v })}
            data={lots.data?.items.map((x) => ({ value: x.id, label: `${x.lot_no} · ${x.item_name} (${x.qty_on_hand ?? '0'} ${x.uom})` })) ?? []} />
          <NumberInput w={140} placeholder="Miktar" min={0} decimalScale={3} value={l.qty}
            onChange={(v) => update(l.key, { qty: typeof v === 'number' ? v : '' })} />
          <ActionIcon variant="subtle" color="red" aria-label="Satırı sil" disabled={inputs.length === 1}
            onClick={() => setInputs((ls) => ls.filter((x) => x.key !== l.key))}>
            <IconTrash size={16} />
          </ActionIcon>
        </Group>
      ))}
      <Button variant="subtle" size="xs" mb="md" leftSection={<IconPlus size={14} />}
        onClick={() => setInputs((ls) => [...ls, { key: nextKey++, lotId: null, qty: '' }])}>
        Girdi ekle
      </Button>
      <Text fw={500} size="sm" mb="xs">Çıktı</Text>
      <Group grow mb="sm">
        <Select label="Ürün" required searchable value={itemId} onChange={setItemId}
          data={items.data?.items.map((i) => ({ value: i.id, label: `${i.name} (${i.code})` })) ?? []} />
        <NumberInput label="Miktar" required min={0} decimalScale={3} value={outQty}
          onChange={(v) => setOutQty(typeof v === 'number' ? v : '')} />
      </Group>
      <Group grow mb="md">
        <TextInput label="Çıktı lot no (opsiyonel)" placeholder="YIL-2026-NNN" value={lotNo} onChange={(e) => setLotNo(e.currentTarget.value)} />
        <TextInput label="Not" value={note} onChange={(e) => setNote(e.currentTarget.value)} />
      </Group>
      <Group justify="flex-end">
        <Button variant="default" onClick={onClose}>Vazgeç</Button>
        <Button loading={create.isPending} disabled={!valid} onClick={() => create.mutate()}>Kaydet</Button>
      </Group>
    </Modal>
  )
}
