import { ActionIcon, Alert, Badge, Button, Group, Modal, NumberInput, Stack, Text, TextInput, Tooltip } from '@mantine/core'
import { IconPlus } from '@tabler/icons-react'
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { ApiError, apiPost, type DownstreamDistribution } from '../lib/api'
import { uuidv7 } from '../lib/uuid7'

// İkinci kademe dağıtım (Görev 3): YALNIZCA kaydedilmişse kesin olarak gösterilir. Kayıt yoksa
// "Bilinmiyor"; kayıtlı kısım alıcı ve miktarla listelenir, kalan için sunucunun notu gösterilir.
export function DownstreamCell({ known, note, entries, onAdd }: {
  known: boolean
  note?: string
  entries: DownstreamDistribution[]
  onAdd?: () => void
}) {
  return (
    <Group gap={6} wrap="nowrap" align="flex-start">
      {known ? (
        <Stack gap={2}>
          {entries.map((d) => (
            <Text key={d.id} size="xs">
              {d.recipient_name}: {d.qty} <Text span c="dimmed" size="xs">({d.distributed_at})</Text>
            </Text>
          ))}
          {note && <Text size="xs" c="orange">{note}</Text>}
        </Stack>
      ) : (
        <Tooltip label={note}>
          <Badge color="gray" variant="outline">Bilinmiyor</Badge>
        </Tooltip>
      )}
      {onAdd && (
        <Tooltip label="İkinci kademe dağıtım kaydet">
          <ActionIcon size="sm" variant="light" aria-label="İkinci kademe dağıtım kaydet" onClick={onAdd}>
            <IconPlus size={14} />
          </ActionIcon>
        </Tooltip>
      )}
    </Group>
  )
}

export interface DownstreamTarget {
  customerId: string
  customerName: string
  lotId: string
  lotNo: string
}

export function DownstreamModal({ target, onClose, onSaved }: {
  target: DownstreamTarget
  onClose: () => void
  onSaved: () => void
}) {
  const [recipient, setRecipient] = useState('')
  const [phone, setPhone] = useState('')
  const [qty, setQty] = useState<number | ''>('')
  const [date, setDate] = useState(() => new Date().toISOString().slice(0, 10))
  const [id] = useState(uuidv7) // idempotency anahtarı
  const save = useMutation({
    mutationFn: () => apiPost<DownstreamDistribution>('/downstream-distributions', {
      id, customer_id: target.customerId, lot_id: target.lotId, recipient_name: recipient,
      recipient_phone: phone || undefined, qty: String(qty), distributed_at: date,
    }),
    onSuccess: onSaved,
  })
  return (
    <Modal opened onClose={onClose} title={`İkinci kademe dağıtım: ${target.customerName} · ${target.lotNo}`}>
      {save.error && <Alert color="red" mb="sm">{save.error instanceof ApiError ? save.error.message : 'Kaydedilemedi'}</Alert>}
      <Text size="sm" c="dimmed" mb="sm">Müşterinin bu lottan kendi alıcısına dağıttığı miktar. Toplam, müşteriye giden net miktarı aşamaz.</Text>
      <TextInput label="Alıcı" required mb="sm" value={recipient} onChange={(e) => setRecipient(e.currentTarget.value)} />
      <TextInput label="Telefon" mb="sm" value={phone} onChange={(e) => setPhone(e.currentTarget.value)} />
      <NumberInput label="Miktar" required mb="sm" min={0} decimalScale={3} value={qty} onChange={(v) => setQty(typeof v === 'number' ? v : '')} />
      <TextInput label="Dağıtım tarihi" type="date" required mb="md" value={date} onChange={(e) => setDate(e.currentTarget.value)} />
      <Group justify="flex-end">
        <Button variant="default" onClick={onClose}>Vazgeç</Button>
        <Button loading={save.isPending} disabled={!recipient || qty === '' || qty <= 0 || !date} onClick={() => save.mutate()}>Kaydet</Button>
      </Group>
    </Modal>
  )
}
