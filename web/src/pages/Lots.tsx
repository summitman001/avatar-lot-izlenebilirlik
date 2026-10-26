import { Alert, Badge, Button, Checkbox, Group, Loader, Modal, Select, Table, Text, Textarea, TextInput, Title, Tooltip } from '@mantine/core'
import { useDebouncedValue, useDisclosure } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ApiError, apiGet, apiPost, type List, type Lot } from '../lib/api'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

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
  const [params] = useSearchParams()
  const qc = useQueryClient()
  const [q, setQ] = useState(params.get('q') ?? '')
  const [status, setStatus] = useState<string | null>(null)
  const [onlyMismatch, setOnlyMismatch] = useState(false)
  const [debouncedQ] = useDebouncedValue(q, 300)
  const [blockTarget, setBlockTarget] = useState<Lot | null>(null)
  const [blockReason, setBlockReason] = useState('')
  const [actionError, setActionError] = useState<string | null>(null)
  const [opened, { open, close }] = useDisclosure(false)

  const { data, isLoading, error } = useQuery({
    queryKey: ['lots', debouncedQ, status, onlyMismatch],
    queryFn: () =>
      apiGet<List<Lot>>('/lots', {
        q: debouncedQ,
        status: status ?? undefined,
        has_mismatch: onlyMismatch ? 'true' : undefined,
      }),
  })

  const invalidate = () => qc.invalidateQueries({ queryKey: ['lots'] })

  const block = useMutation({
    mutationFn: (lot: Lot) => apiPost(`/lots/${lot.id}/block`, { reason: blockReason }),
    onSuccess: () => { invalidate(); close(); setBlockReason('') },
    onError: (e) => setActionError(err(e)),
  })
  const unblock = useMutation({
    mutationFn: (lot: Lot) => apiPost(`/lots/${lot.id}/unblock`),
    onSuccess: invalidate,
    onError: (e) => setActionError(err(e)),
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
      {actionError && <Alert color="red" mb="sm" onClose={() => setActionError(null)} withCloseButton>{actionError}</Alert>}
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
              <Table.Th></Table.Th>
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
                  {lot.blocked_reason && (
                    <Tooltip label={lot.blocked_reason}><Text span size="xs" c="dimmed" ml="xs">(sebep)</Text></Tooltip>
                  )}
                </Table.Td>
                <Table.Td>
                  {lot.status === 'blocked' ? (
                    <Button size="xs" variant="light" color="green" loading={unblock.isPending} onClick={() => unblock.mutate(lot)}>
                      Aç
                    </Button>
                  ) : lot.status === 'active' ? (
                    <Button size="xs" variant="light" color="orange" onClick={() => { setBlockTarget(lot); open() }}>
                      Blokla
                    </Button>
                  ) : null}
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
      <Modal opened={opened} onClose={close} title={`Lotu Blokla: ${blockTarget?.lot_no ?? ''}`}>
        <Textarea label="Sebep" required value={blockReason} onChange={(e) => setBlockReason(e.currentTarget.value)} mb="md" />
        <Group justify="flex-end">
          <Button variant="default" onClick={close}>Vazgeç</Button>
          <Button color="orange" loading={block.isPending} disabled={!blockReason}
            onClick={() => blockTarget && block.mutate(blockTarget)}>
            Blokla
          </Button>
        </Group>
      </Modal>
    </>
  )
}
