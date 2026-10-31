import { Alert, Button, Modal, Select, Stack, TextInput } from '@mantine/core'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { ApiError, apiPost, type Item } from '../lib/api'

const TYPE_OPTIONS: { value: Item['item_type']; label: string }[] = [
  { value: 'raw', label: 'Hammadde' },
  { value: 'finished', label: 'Mamul' },
  { value: 'cut_part', label: 'Parça' },
  { value: 'by_product', label: 'Yan ürün' },
  { value: 'waste', label: 'Fire' },
]
const CATEGORY_OPTIONS = [
  { value: 'meat', label: 'Et' },
  { value: 'spice', label: 'Baharat' },
  { value: 'casing', label: 'Bağırsak' },
  { value: 'other', label: 'Diğer' },
]

// Yeni ürün her formdan açılabilir. Hammadde kategorisi dış üretim maliyetinde kullanıldığı için
// hammaddede sorulur; kategorisiz hammadde üreticiye sevk edilemez.
export function NewItemModal({ opened, onClose, onCreated, initialName }: {
  opened: boolean
  onClose: () => void
  onCreated?: (item: Item) => void
  initialName?: string
}) {
  return (
    <Modal opened={opened} onClose={onClose} title="Yeni Ürün">
      <ItemForm initialName={initialName} onCreated={(it) => { onCreated?.(it); onClose() }} />
    </Modal>
  )
}

function ItemForm({ initialName, onCreated }: { initialName?: string; onCreated: (item: Item) => void }) {
  const qc = useQueryClient()
  const [code, setCode] = useState('')
  const [name, setName] = useState(initialName ?? '')
  const [itemType, setItemType] = useState<Item['item_type']>('raw')
  const [baseUom, setBaseUom] = useState<Item['base_uom']>('kg')
  const [category, setCategory] = useState<string>('meat')

  const create = useMutation({
    mutationFn: () => apiPost<Item>('/items', {
      code, name, item_type: itemType, base_uom: baseUom,
      material_category: itemType === 'raw' ? category : undefined,
    }),
    onSuccess: (it) => {
      qc.invalidateQueries({ queryKey: ['items'] })
      onCreated(it)
    },
  })

  return (
    <Stack>
      {create.error && <Alert color="red">{create.error instanceof ApiError ? create.error.message : 'Bilinmeyen hata'}</Alert>}
      <TextInput label="Kod" description="Benzersiz kısa kod, ör. KUZU-BUTUN" required value={code} onChange={(e) => setCode(e.currentTarget.value)} />
      <TextInput label="Ad" required value={name} onChange={(e) => setName(e.currentTarget.value)} />
      <Select label="Tür" allowDeselect={false} data={TYPE_OPTIONS} value={itemType} onChange={(v) => v && setItemType(v as Item['item_type'])} />
      <Select label="Birim" description="Alış ve stok miktarları bu birimle girilir" allowDeselect={false}
        data={[{ value: 'kg', label: 'kg' }, { value: 'adet', label: 'adet' }]} value={baseUom}
        onChange={(v) => v && setBaseUom(v as Item['base_uom'])} />
      {itemType === 'raw' && (
        <Select label="Hammadde kategorisi" allowDeselect={false} data={CATEGORY_OPTIONS} value={category} onChange={(v) => v && setCategory(v)} />
      )}
      <Button disabled={!code || !name} loading={create.isPending} onClick={() => create.mutate()}>Oluştur</Button>
    </Stack>
  )
}
