import { Alert, Badge, Group, Loader, Select, Table, TextInput, Title } from '@mantine/core'
import { useDebouncedValue } from '@mantine/hooks'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { apiGet, type Item, type List } from '../lib/api'

const TYPE_LABEL: Record<Item['item_type'], string> = {
  raw: 'Hammadde',
  finished: 'Mamul',
  cut_part: 'Parça',
  by_product: 'Yan ürün',
  waste: 'Fire',
}

export default function Products() {
  const [q, setQ] = useState('')
  const [type, setType] = useState<string | null>(null)
  const [debouncedQ] = useDebouncedValue(q, 300)

  const { data, isLoading, error } = useQuery({
    queryKey: ['items', debouncedQ, type],
    queryFn: () => apiGet<List<Item>>('/items', { q: debouncedQ, item_type: type ?? undefined }),
  })

  return (
    <>
      <Group justify="space-between" mb="md">
        <Title order={2}>Ürünler</Title>
        <Group>
          <TextInput placeholder="Kod veya ad ara" value={q} onChange={(e) => setQ(e.currentTarget.value)} />
          <Select
            placeholder="Tür"
            clearable
            value={type}
            onChange={setType}
            data={Object.entries(TYPE_LABEL).map(([value, label]) => ({ value, label }))}
          />
        </Group>
      </Group>
      {error && <Alert color="red">{error.message}</Alert>}
      {isLoading && <Loader />}
      {data && (
        <Table striped highlightOnHover withTableBorder>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Kod</Table.Th>
              <Table.Th>Ad</Table.Th>
              <Table.Th>Tür</Table.Th>
              <Table.Th>Birim</Table.Th>
              <Table.Th ta="right">Birim ağırlık (kg)</Table.Th>
              <Table.Th ta="right">Varsayılan satış fiyatı</Table.Th>
              <Table.Th ta="right">Raf ömrü (gün)</Table.Th>
              <Table.Th>Durum</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {data.items.map((it) => (
              <Table.Tr key={it.id}>
                <Table.Td>{it.code}</Table.Td>
                <Table.Td>{it.name}</Table.Td>
                <Table.Td>{TYPE_LABEL[it.item_type]}</Table.Td>
                <Table.Td>{it.base_uom}</Table.Td>
                {/* Decimal'ler string olarak gösterilir; Number'a çevrilmez. */}
                <Table.Td ta="right">{it.unit_weight_kg ?? '—'}</Table.Td>
                <Table.Td ta="right">{it.default_sale_price ?? '—'}</Table.Td>
                <Table.Td ta="right">{it.shelf_life_days ?? '—'}</Table.Td>
                <Table.Td>
                  <Badge color={it.is_active ? 'green' : 'gray'} variant="light">
                    {it.is_active ? 'Aktif' : 'Pasif'}
                  </Badge>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
    </>
  )
}
