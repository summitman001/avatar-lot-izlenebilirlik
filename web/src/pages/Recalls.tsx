import { Alert, Badge, Button, Group, Loader, Modal, Stack, Table, Text, TextInput, Textarea, Title } from '@mantine/core'
import { useDebouncedValue, useDisclosure } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiError, apiGet, apiPost, type List, type Lot, type Recall, type TraceForwardResponse } from '../lib/api'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

const STATUS_COLOR: Record<Recall['status'], string> = { open: 'red', closed: 'gray' }
const STATUS_LABEL: Record<Recall['status'], string> = { open: 'Açık', closed: 'Kapalı' }

export default function Recalls() {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [opened, { open, close }] = useDisclosure(false)

  const { data, isLoading, error } = useQuery({
    queryKey: ['recalls'],
    queryFn: () => apiGet<List<Recall>>('/recalls'),
  })

  return (
    <Stack>
      <Group justify="space-between">
        <Title order={2}>Geri Çağırma</Title>
        <Button color="red" onClick={open}>Yeni Geri Çağırma</Button>
      </Group>
      {error && <Alert color="red">{error.message}</Alert>}
      {isLoading && <Loader />}
      {data && (
        <Table striped highlightOnHover withTableBorder>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Geri Çağırma No</Table.Th><Table.Th>Kök Lot</Table.Th><Table.Th>Sebep</Table.Th>
              <Table.Th>Durum</Table.Th><Table.Th ta="right">Etkilenen Lot</Table.Th><Table.Th ta="right">Bekleyen Müşteri</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {data.items.map((r) => (
              <Table.Tr key={r.id} style={{ cursor: 'pointer' }} onClick={() => navigate(`/geri-cagirma/${r.id}`)}>
                <Table.Td>{r.recall_no}</Table.Td>
                <Table.Td>{r.root_lot_no}</Table.Td>
                <Table.Td>{r.reason}</Table.Td>
                <Table.Td><Badge color={STATUS_COLOR[r.status]} variant="light">{STATUS_LABEL[r.status]}</Badge></Table.Td>
                <Table.Td ta="right">{r.affected_lot_count}</Table.Td>
                <Table.Td ta="right">{r.pending_customer_count}</Table.Td>
              </Table.Tr>
            ))}
            {data.items.length === 0 && (
              <Table.Tr><Table.Td colSpan={6}><Text c="dimmed" size="sm">Henüz geri çağırma yok</Text></Table.Td></Table.Tr>
            )}
          </Table.Tbody>
        </Table>
      )}
      <NewRecallModal
        opened={opened}
        onClose={close}
        onDone={(id) => {
          qc.invalidateQueries({ queryKey: ['recalls'] })
          navigate(`/geri-cagirma/${id}`)
        }}
      />
    </Stack>
  )
}

function NewRecallModal({ opened, onClose, onDone }: { opened: boolean; onClose: () => void; onDone: (id: string) => void }) {
  const [q, setQ] = useState('')
  const [debouncedQ] = useDebouncedValue(q, 300)
  const [selected, setSelected] = useState<Lot | null>(null)
  const [reason, setReason] = useState('')
  const [formError, setFormError] = useState<string | null>(null)

  const reset = () => {
    setQ('')
    setSelected(null)
    setReason('')
    setFormError(null)
  }

  const searchQuery = useQuery({
    queryKey: ['recall-lot-search', debouncedQ],
    queryFn: () => apiGet<List<Lot>>('/lots', { q: debouncedQ, status: 'active' }),
    enabled: debouncedQ.length > 0 && !selected,
  })
  const previewQuery = useQuery({
    queryKey: ['recall-preview', selected?.id],
    queryFn: () => apiGet<TraceForwardResponse>(`/lots/${selected?.id}/trace-forward`),
    enabled: !!selected,
  })

  const create = useMutation({
    mutationFn: () => apiPost<{ id: string }>('/recalls', { root_lot_id: selected?.id, reason }),
    onSuccess: (rc) => {
      reset()
      onDone(rc.id)
    },
    onError: (e) => setFormError(err(e)),
  })

  return (
    <Modal opened={opened} onClose={() => { reset(); onClose() }} title="Yeni Geri Çağırma" size="lg">
      {formError && <Alert color="red" mb="sm">{formError}</Alert>}
      {!selected && (
        <>
          <TextInput placeholder="Lot no veya ürün adı ara" value={q} onChange={(e) => setQ(e.currentTarget.value)} mb="sm" />
          {searchQuery.data && (
            <Table highlightOnHover>
              <Table.Tbody>
                {searchQuery.data.items.map((lot) => (
                  <Table.Tr key={lot.id} style={{ cursor: 'pointer' }} onClick={() => setSelected(lot)}>
                    <Table.Td>{lot.lot_no}</Table.Td>
                    <Table.Td>{lot.item_name}</Table.Td>
                  </Table.Tr>
                ))}
                {searchQuery.data.items.length === 0 && (
                  <Table.Tr><Table.Td colSpan={2}><Text c="dimmed" size="sm">Sonuç yok</Text></Table.Td></Table.Tr>
                )}
              </Table.Tbody>
            </Table>
          )}
        </>
      )}
      {selected && (
        <Stack>
          <Group justify="space-between">
            <Text fw={500}>Kök lot: {selected.lot_no} ({selected.item_name})</Text>
            <Button size="xs" variant="subtle" onClick={() => setSelected(null)}>Değiştir</Button>
          </Group>
          {previewQuery.isLoading && <Loader size="sm" />}
          {previewQuery.data && (
            <Alert color="orange" title="Önizleme">
              <Text size="sm">
                <b>{previewQuery.data.summary.affected_lot_count}</b> lot bloke edilecek; etkilenen marketler:{' '}
                {previewQuery.data.summary.customers.map((c) => c.customer_name).join(', ') || '—'}.
              </Text>
            </Alert>
          )}
          <Textarea label="Sebep" required value={reason} onChange={(e) => setReason(e.currentTarget.value)} />
          <Group justify="flex-end">
            <Button variant="default" onClick={() => { reset(); onClose() }}>Vazgeç</Button>
            <Button color="red" loading={create.isPending} disabled={!reason} onClick={() => create.mutate()}>
              Onayla ve Başlat
            </Button>
          </Group>
        </Stack>
      )}
    </Modal>
  )
}
