import { Alert, Badge, Checkbox, Group, Loader, Select, Table, Text, TextInput, Title, Tooltip } from '@mantine/core'
import { useDebouncedValue } from '@mantine/hooks'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { apiGet, type List, type Lot } from '../lib/api'

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
  const [q, setQ] = useState('')
  const [status, setStatus] = useState<string | null>(null)
  const [onlyMismatch, setOnlyMismatch] = useState(false)
  const [debouncedQ] = useDebouncedValue(q, 300)

  const { data, isLoading, error } = useQuery({
    queryKey: ['lots', debouncedQ, status, onlyMismatch],
    queryFn: () =>
      apiGet<List<Lot>>('/lots', {
        q: debouncedQ,
        status: status ?? undefined,
        has_mismatch: onlyMismatch ? 'true' : undefined,
      }),
  })

  return (
    <>
      <Group justify="space-between" mb="md">
        <Title order={2}>Lotlar</Title>
        <Group>
          <Checkbox label="Sadece tutar uyuşmazlığı" checked={onlyMismatch} onChange={(e) => setOnlyMismatch(e.currentTarget.checked)} />
          <TextInput placeholder="Lot no veya ürün ara" value={q} onChange={(e) => setQ(e.currentTarget.value)} />
          <Select
            placeholder="Durum"
            clearable
            value={status}
            onChange={setStatus}
            data={Object.entries(STATUS).map(([value, s]) => ({ value, label: s.label }))}
          />
        </Group>
      </Group>
      {error && <Alert color="red">{error.message}</Alert>}
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
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
    </>
  )
}
