import { Autocomplete, Badge, Group, Text, type AutocompleteProps } from '@mantine/core'
import { useDebouncedValue } from '@mantine/hooks'
import { IconSearch } from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { apiGet, type List, type Lot } from '../lib/api'

const STATUS_COLOR: Record<Lot['status'], string> = { active: 'green', blocked: 'orange', recalled: 'red', consumed: 'gray' }
const STATUS_LABEL: Record<Lot['status'], string> = { active: 'Aktif', blocked: 'Bloke', recalled: 'Geri çağrıldı', consumed: 'Tükendi' }

// Lot arama: kutuya tıklayınca en yeni lotlar önerilir, yazdıkça lot no, ürün kodu ve adına göre
// daralır. Tam numarayı bilmek gerekmez.
export function LotSearch({ status, initialQuery = '', onSelect, ...rest }: {
  status?: Lot['status']
  initialQuery?: string
  onSelect: (lot: Lot) => void
} & Pick<AutocompleteProps, 'size' | 'radius' | 'mb'>) {
  const [q, setQ] = useState(initialQuery)
  const [debouncedQ] = useDebouncedValue(q, 250)
  const { data, isFetched } = useQuery({
    queryKey: ['lot-suggest', debouncedQ, status],
    queryFn: () => apiGet<List<Lot>>('/lots', { q: debouncedQ, status, limit: '20' }),
  })
  const lots = data?.items ?? []
  const byNo = new Map(lots.map((l) => [l.lot_no, l]))

  return (
    <>
      <Autocomplete
        {...rest}
        leftSection={<IconSearch size={16} />}
        placeholder="Lot no veya ürün adı yazın ya da listeden seçin"
        value={q}
        onChange={setQ}
        data={lots.map((l) => l.lot_no)}
        filter={({ options }) => options} // süzmeyi sunucu yapar
        maxDropdownHeight={340}
        onOptionSubmit={(no) => {
          const lot = byNo.get(no)
          if (lot) onSelect(lot)
        }}
        renderOption={({ option }) => {
          const lot = byNo.get(option.value)
          return (
            <Group justify="space-between" wrap="nowrap" w="100%">
              <div>
                <Text size="sm" fw={500}>{option.value}</Text>
                {lot && <Text size="xs" c="dimmed">{lot.item_name} · {lot.qty_on_hand ?? '0'} {lot.uom}</Text>}
              </div>
              {lot && <Badge size="sm" variant="light" color={STATUS_COLOR[lot.status]}>{STATUS_LABEL[lot.status]}</Badge>}
            </Group>
          )
        }}
      />
      {isFetched && lots.length === 0 && debouncedQ && <Text c="dimmed" size="sm" mt={4}>Bu aramayla eşleşen lot yok</Text>}
    </>
  )
}
