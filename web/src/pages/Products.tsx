import { Alert, Badge, Group, Loader, Table, TextInput } from '@mantine/core'
import { useDebouncedValue } from '@mantine/hooks'
import { IconBox, IconCircleOff, IconPackages, IconPlant2, IconSearch } from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { FilterChips, PageHeader, StatCard, StatGrid, SUMMARY_LIMIT, countBy } from '../components/Page'
import { apiGet, type Item, type List } from '../lib/api'

const TYPE_LABEL: Record<Item['item_type'], string> = {
  raw: 'Hammadde',
  finished: 'Mamul',
  cut_part: 'Parça',
  by_product: 'Yan ürün',
  waste: 'Fire',
}
const TYPE_COLOR: Record<Item['item_type'], string> = {
  raw: 'orange', finished: 'brand', cut_part: 'grape', by_product: 'cyan', waste: 'gray',
}

export default function Products() {
  const [q, setQ] = useState('')
  const [type, setType] = useState<string | null>(null)
  const [debouncedQ] = useDebouncedValue(q, 300)

  const { data, isLoading, error } = useQuery({
    queryKey: ['items', debouncedQ, type],
    queryFn: () => apiGet<List<Item>>('/items', { q: debouncedQ, item_type: type ?? undefined }),
  })
  const all = useQuery({
    queryKey: ['items', 'summary'],
    queryFn: () => apiGet<List<Item>>('/items', { limit: String(SUMMARY_LIMIT) }),
  })
  const items = all.data?.items
  const byType = countBy(items, (i) => i.item_type)
  const n = (v?: number) => (all.isLoading ? '…' : v ?? 0)

  return (
    <>
      <PageHeader eyebrow="Hammadde, mamul ve parçaların ana kaydı" title="Ürünler" />
      <StatGrid capped={items?.length === SUMMARY_LIMIT}>
        <StatCard label="Toplam ürün" value={n(items?.length)} icon={IconPackages} />
        <StatCard label="Hammadde" value={n(byType.raw)} icon={IconPlant2} color="orange" />
        <StatCard label="Mamul" value={n(byType.finished)} icon={IconBox} />
        <StatCard label="Pasif" value={n(items?.filter((i) => !i.is_active).length)} icon={IconCircleOff} color="gray" />
      </StatGrid>
      <Group justify="space-between" mb="md">
        <FilterChips value={type} onChange={setType} allCount={items?.length}
          options={(Object.keys(TYPE_LABEL) as Item['item_type'][]).map((t) => (
            { value: t, label: TYPE_LABEL[t], count: byType[t] ?? 0, color: TYPE_COLOR[t] }))} />
        <TextInput radius="xl" w={260} leftSection={<IconSearch size={16} />} placeholder="Kod veya ad ara"
          value={q} onChange={(e) => setQ(e.currentTarget.value)} />
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
