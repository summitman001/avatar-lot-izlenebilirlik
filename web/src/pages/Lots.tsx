import { Alert, Badge, Button, Group, Loader, Modal, Table, Text, Textarea, TextInput, Tooltip, UnstyledButton } from '@mantine/core'
import { useDebouncedValue, useDisclosure } from '@mantine/hooks'
import { IconAlertTriangle, IconArrowBackUp, IconLock, IconSearch, IconStack2 } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { FilterChips, PageHeader, StatCard, StatGrid, SUMMARY_LIMIT, countBy } from '../components/Page'
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
  const [status, setStatus] = useState<string | null>(params.get('status')) // Dashboard: ?status=blocked
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

  const all = useQuery({
    queryKey: ['lots', 'summary'],
    queryFn: () => apiGet<List<Lot>>('/lots', { limit: String(SUMMARY_LIMIT) }),
  })
  const lots = all.data?.items
  const byStatus = countBy(lots, (l) => l.status)
  const mismatchCount = lots?.filter((l) => l.purchase?.has_mismatch).length
  const n = (v?: number) => (all.isLoading ? '…' : v ?? 0)

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
      <PageHeader eyebrow="Her lotun kaynağı, eldeki miktarı ve durumu" title="Lotlar" />
      <StatGrid capped={lots?.length === SUMMARY_LIMIT}>
        <StatCard label="Toplam lot" value={n(lots?.length)} icon={IconStack2} />
        <StatCard label="Aktif" value={n(byStatus.active)} icon={IconStack2} color="green" />
        <StatCard label="Bloke" value={n(byStatus.blocked)} icon={IconLock} color="orange" />
        <StatCard label="Geri çağrılan" value={n(byStatus.recalled)} icon={IconArrowBackUp} color="red" />
      </StatGrid>
      <Group justify="space-between" mb="md">
        <FilterChips value={status} onChange={setStatus} allCount={lots?.length}
          options={(Object.keys(STATUS) as Lot['status'][]).map((s) => (
            { value: s, label: STATUS[s].label, count: byStatus[s] ?? 0, color: STATUS[s].color }))} />
        <Group gap="xs">
          <UnstyledButton className="filter-chip" data-active={onlyMismatch || undefined} aria-pressed={onlyMismatch}
            onClick={() => setOnlyMismatch((v) => !v)}>
            <IconAlertTriangle size={14} color="var(--mantine-color-red-filled)" />
            Tutar uyuşmazlığı
            {mismatchCount != null && <span className="chip-count">{mismatchCount}</span>}
          </UnstyledButton>
          <TextInput radius="xl" w={240} leftSection={<IconSearch size={16} />} placeholder="Lot no veya ürün ara"
            value={q} onChange={(e) => setQ(e.currentTarget.value)} />
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
