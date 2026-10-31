import { Alert, Badge, Button, Group, Loader, Modal, Stack, Table, Text, Textarea } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { IconAlertOctagon, IconArrowBackUp, IconCircleCheck, IconPhoneCall } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { LotSearch } from '../components/LotSearch'
import { FilterChips, PageHeader, StatCard, StatGrid, SUMMARY_LIMIT, countBy } from '../components/Page'
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
    queryFn: () => apiGet<List<Recall>>('/recalls', { limit: String(SUMMARY_LIMIT) }),
  })
  const [status, setStatus] = useState<string | null>(null)
  const byStatus = countBy(data?.items, (r) => r.status)
  const rows = data?.items.filter((r) => !status || r.status === status) ?? []
  const n = (v?: number) => (isLoading ? '…' : v ?? 0)

  return (
    <>
      <PageHeader eyebrow="Bir lottan etkilenen tüm lotlar ve müşteriler, iletişim takibiyle" title="Geri Çağırma"
        actions={<Button color="red" leftSection={<IconArrowBackUp size={16} />} onClick={open}>Yeni Geri Çağırma</Button>} />
      <StatGrid capped={data?.items.length === SUMMARY_LIMIT}>
        <StatCard label="Toplam" value={n(data?.items.length)} icon={IconArrowBackUp} />
        <StatCard label="Açık" value={n(byStatus.open)} icon={IconAlertOctagon} color={byStatus.open ? 'red' : 'gray'} />
        <StatCard label="Kapalı" value={n(byStatus.closed)} icon={IconCircleCheck} color="gray" />
        <StatCard label="Aranacak müşteri" icon={IconPhoneCall} color="orange"
          value={n(data?.items.filter((r) => r.status === 'open').reduce((sum, r) => sum + r.pending_customer_count, 0))} />
      </StatGrid>
      <Group mb="md">
        <FilterChips value={status} onChange={setStatus} allCount={data?.items.length}
          options={(['open', 'closed'] as const).map((s) => ({ value: s, label: STATUS_LABEL[s], count: byStatus[s] ?? 0, color: STATUS_COLOR[s] }))} />
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
            {rows.map((r) => (
              <Table.Tr key={r.id} style={{ cursor: 'pointer' }} onClick={() => navigate(`/geri-cagirma/${r.id}`)}>
                <Table.Td>{r.recall_no}</Table.Td>
                <Table.Td>{r.root_lot_no}</Table.Td>
                <Table.Td>{r.reason}</Table.Td>
                <Table.Td><Badge color={STATUS_COLOR[r.status]} variant="light">{STATUS_LABEL[r.status]}</Badge></Table.Td>
                <Table.Td ta="right">{r.affected_lot_count}</Table.Td>
                <Table.Td ta="right">{r.pending_customer_count}</Table.Td>
              </Table.Tr>
            ))}
            {rows.length === 0 && (
              <Table.Tr><Table.Td colSpan={6}><Text c="dimmed" size="sm">{status ? 'Bu durumda geri çağırma yok' : 'Henüz geri çağırma yok'}</Text></Table.Td></Table.Tr>
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
    </>
  )
}

function NewRecallModal({ opened, onClose, onDone }: { opened: boolean; onClose: () => void; onDone: (id: string) => void }) {
  const [selected, setSelected] = useState<Lot | null>(null)
  const [reason, setReason] = useState('')
  const [formError, setFormError] = useState<string | null>(null)

  const reset = () => {
    setSelected(null)
    setReason('')
    setFormError(null)
  }

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
      {!selected && <LotSearch status="active" onSelect={setSelected} mb="sm" />}
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
