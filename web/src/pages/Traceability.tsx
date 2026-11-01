import { Alert, Badge, Button, Card, Group, Loader, Modal, SimpleGrid, Stack, Table, Tabs, Text, Textarea, TextInput, Title, Tooltip } from '@mantine/core'
import { useDebouncedValue, useDisclosure } from '@mantine/hooks'
import { IconSearch } from '@tabler/icons-react'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { LOT_STATUS, LotStatusBadge, RecallLink, isLotHeld, splitReason } from '../components/LotStatus'
import { PageHeader } from '../components/Page'
import {
  ApiError, apiGet, apiPost,
  type GenealogyResponse, type List, type Lot,
  type TraceBackwardResponse, type TraceForwardResponse,
} from '../lib/api'
import { qty, unitCost } from '../lib/format'
import { processLabel } from '../lib/messages'
import { DownstreamCell } from '../components/Downstream'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

const STATUS_COLOR: Record<string, string> = { active: 'green', blocked: 'orange', recalled: 'red', consumed: 'gray' }
const STATUS_LABEL: Record<string, string> = { active: 'Aktif', blocked: 'Bloke', recalled: 'Geri çağrıldı', consumed: 'Tükendi' }

// GenealogyTree: react-flow yerine basit bir hiyerarşik görünüm -- düğümler seviyeye (negatif=yukarı
// hammadde, 0=kök, pozitif=aşağı mamul) göre satırlara dizilir; kenarlar altta miktar + işlem tipiyle listelenir.
function GenealogyTree({ data, onSelect, selectedLotId }: { data: GenealogyResponse; onSelect: (id: string) => void; selectedLotId: string }) {
  const byLevel = new Map<number, typeof data.nodes>()
  for (const n of data.nodes) {
    const list = byLevel.get(n.level) ?? []
    list.push(n)
    byLevel.set(n.level, list)
  }
  const levels = [...byLevel.keys()].sort((a, b) => a - b)
  const nodeByID = new Map(data.nodes.map((n) => [n.lot_id, n]))

  return (
    <Card withBorder>
      <Stack gap="sm">
        {levels.map((level) => (
          <Group key={level} gap="xs" wrap="wrap">
            <Text size="xs" c="dimmed" w={110}>
              {level < 0 ? `Yukarı (${level})` : level === 0 ? 'Kök lot' : `Aşağı (+${level})`}
            </Text>
            {byLevel.get(level)!.map((n) => (
              <Tooltip key={n.lot_id} label={n.item_name}>
                <Badge
                  variant={n.lot_id === selectedLotId ? 'filled' : 'light'}
                  color={STATUS_COLOR[n.status] ?? 'gray'}
                  style={{ cursor: 'pointer' }}
                  onClick={() => onSelect(n.lot_id)}
                >
                  {n.lot_no}
                </Badge>
              </Tooltip>
            ))}
          </Group>
        ))}
      </Stack>
      {data.edges.length > 0 && (
        <Table mt="md" fz="xs">
          <Table.Thead>
            <Table.Tr><Table.Th>Kaynak lot</Table.Th><Table.Th>Hedef lot</Table.Th><Table.Th ta="right">Miktar</Table.Th><Table.Th>İşlem</Table.Th></Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {data.edges.map((e, i) => (
              <Table.Tr key={i}>
                <Table.Td>{nodeByID.get(e.from_lot_id)?.lot_no ?? e.from_lot_id}</Table.Td>
                <Table.Td>{nodeByID.get(e.to_lot_id)?.lot_no ?? e.to_lot_id}</Table.Td>
                <Table.Td ta="right">{qty(e.qty, e.uom)}</Table.Td>
                <Table.Td>{processLabel(e.process_type)}</Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
    </Card>
  )
}

function ForwardPanel({ data }: { data: TraceForwardResponse }) {
  return (
    <Stack>
      <Card withBorder>
        <Text fw={600} mb="sm">Etkilenen Lotlar ({data.summary.affected_lot_count})</Text>
        <Table striped>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Lot</Table.Th><Table.Th>Ürün</Table.Th><Table.Th ta="right">Derinlik</Table.Th>
              <Table.Th ta="right">Stokta kalan</Table.Th><Table.Th ta="right">İade edilen</Table.Th><Table.Th>Durum</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {data.nodes.map((n) => (
              <Table.Tr key={n.lot_id}>
                <Table.Td>{n.lot_no}</Table.Td>
                <Table.Td>{n.item_name}</Table.Td>
                <Table.Td ta="right">{n.depth}</Table.Td>
                <Table.Td ta="right">{qty(n.qty_on_hand_total, n.uom)}</Table.Td>
                <Table.Td ta="right">{qty(n.returned_qty, n.uom)}</Table.Td>
                <Table.Td><Badge color={STATUS_COLOR[n.status]} variant="light">{STATUS_LABEL[n.status] ?? n.status}</Badge></Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Card>

      <Card withBorder>
        <Text fw={600} mb="sm">Müşteri Dağılımı</Text>
        <Table>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Müşteri</Table.Th><Table.Th ta="right">Sevk edilen</Table.Th>
              <Table.Th ta="right">İade edilen</Table.Th><Table.Th>İkinci kademe dağıtım</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {data.summary.customers.map((c) => (
              <Table.Tr key={c.customer_id}>
                <Table.Td>{c.customer_name}</Table.Td>
                <Table.Td ta="right">{qty(c.qty_shipped)}</Table.Td>
                <Table.Td ta="right">{qty(c.qty_returned)}</Table.Td>
                <Table.Td>
                  <DownstreamCell known={c.downstream_known} note={c.downstream_note} entries={c.downstream ?? []} />
                </Table.Td>
              </Table.Tr>
            ))}
            {data.summary.customers.length === 0 && (
              <Table.Tr><Table.Td colSpan={4}><Text c="dimmed" size="sm">Henüz satış/sevkiyat kaydı yok</Text></Table.Td></Table.Tr>
            )}
          </Table.Tbody>
        </Table>
      </Card>

      {data.nodes.filter((n) => n.sales.length > 0).map((n) => (
        <Card withBorder key={n.lot_id}>
          <Text fw={600} mb="sm">{n.lot_no} — Satışlar</Text>
          <Table fz="sm">
            <Table.Thead>
              <Table.Tr><Table.Th>Sipariş</Table.Th><Table.Th>Müşteri</Table.Th><Table.Th ta="right">Miktar</Table.Th><Table.Th>Tarih</Table.Th><Table.Th></Table.Th></Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {n.sales.map((s, i) => (
                <Table.Tr key={i}>
                  <Table.Td>{s.order_no}</Table.Td>
                  <Table.Td>{s.customer_name}</Table.Td>
                  <Table.Td ta="right">{qty(s.qty)}</Table.Td>
                  <Table.Td>{new Date(s.sold_at).toLocaleDateString('tr-TR')}</Table.Td>
                  <Table.Td>{s.recall_violation && <Badge color="red" variant="filled">Bloke lot satışı</Badge>}</Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Card>
      ))}
    </Stack>
  )
}

function BackwardPanel({ data }: { data: TraceBackwardResponse }) {
  return (
    <Card withBorder>
      <Text fw={600} mb="sm">
        Hammadde Lotları ({data.summary.raw_lot_count}) — {data.summary.supplier_count} tedarikçi
      </Text>
      <Table striped>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Lot</Table.Th><Table.Th>Ürün</Table.Th><Table.Th>Tedarikçi</Table.Th><Table.Th>İletişim</Table.Th>
            <Table.Th ta="right">Kullanılan miktar</Table.Th><Table.Th>Parti tarihi</Table.Th><Table.Th>İşlem / Sipariş</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {data.nodes.map((n) => (
            <Table.Tr key={n.lot_id}>
              <Table.Td>{n.lot_no}</Table.Td>
              <Table.Td>{n.item_name}</Table.Td>
              <Table.Td>{n.supplier_name ?? '—'}</Table.Td>
              <Table.Td><Text size="xs">{[n.supplier_phone, n.supplier_email].filter(Boolean).join(' · ') || '—'}</Text></Table.Td>
              <Table.Td ta="right">{qty(n.qty_used, n.uom)}</Table.Td>
              <Table.Td>{n.received_or_produced_at}</Table.Td>
              <Table.Td>{processLabel(n.process_type)}{n.order_no ? ` (${n.order_no})` : ''}</Table.Td>
            </Table.Tr>
          ))}
          {data.nodes.length === 0 && (
            <Table.Tr><Table.Td colSpan={7}><Text c="dimmed" size="sm">Hammadde kaydı yok</Text></Table.Td></Table.Tr>
          )}
        </Table.Tbody>
      </Table>
    </Card>
  )
}

// Açılışta en yeni lotlar listelenir (sunucu sırası); yazdıkça 300 ms gecikmeyle lot no / ürün adına göre
// süzülür, buton gerekmez. Satıra tıklayınca o lotun izlemesi açılır.
function LotPicker({ initialQuery, onSelect }: { initialQuery: string; onSelect: (id: string) => void }) {
  const [q, setQ] = useState(initialQuery)
  const [debouncedQ] = useDebouncedValue(q.trim(), 300)
  const { data, isLoading, isFetching, error } = useQuery({
    queryKey: ['lot-suggest', debouncedQ, 'trace'],
    queryFn: () => apiGet<List<Lot>>('/lots', { q: debouncedQ, limit: '30' }),
    placeholderData: keepPreviousData,
  })
  return (
    <Stack gap="sm">
      <div>
        <TextInput radius="xl" size="md" leftSection={<IconSearch size={16} />} rightSection={isFetching ? <Loader size="xs" /> : null}
          placeholder="Lot no veya ürün adı yazın" aria-label="Lot ara" value={q} onChange={(e) => setQ(e.currentTarget.value)} />
        <Text size="xs" c="dimmed" mt={4} ml="md">Örnek: ET-2026-001</Text>
      </div>
      {error && <Alert color="red">{err(error)}</Alert>}
      {isLoading && <Loader size="sm" />}
      {data && (
        <Table striped highlightOnHover withTableBorder>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Lot no</Table.Th><Table.Th>Ürün</Table.Th><Table.Th>Durum</Table.Th><Table.Th ta="right">Kalan miktar</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {data.items.map((lot) => (
              <Table.Tr key={lot.id} onClick={() => onSelect(lot.id)} style={{ cursor: 'pointer' }}>
                <Table.Td fw={600}>{lot.lot_no}</Table.Td>
                <Table.Td>{lot.item_name}</Table.Td>
                <Table.Td><LotStatusBadge status={lot.status} /></Table.Td>
                <Table.Td ta="right" style={{ whiteSpace: 'nowrap' }}>{qty(lot.qty_on_hand ?? '0', lot.uom)}</Table.Td>
              </Table.Tr>
            ))}
            {data.items.length === 0 && (
              <Table.Tr><Table.Td colSpan={4}><Text c="dimmed" size="sm">Bu aramayla eşleşen lot yok</Text></Table.Td></Table.Tr>
            )}
          </Table.Tbody>
        </Table>
      )}
    </Stack>
  )
}

// Seçili lotun özeti (lot detayı): durum, miktar, maliyet; bloke/geri çağrılmışsa sebebin tam metni ve
// ilgili geri çağırma kaydına link.
function LotInfoCard({ lotId }: { lotId: string }) {
  const { data: lot } = useQuery({ queryKey: ['lot', lotId], queryFn: () => apiGet<Lot>(`/lots/${lotId}`) })
  if (!lot) return null
  const reason = lot.blocked_reason ? splitReason(lot.blocked_reason) : null
  return (
    <Card withBorder>
      <SimpleGrid cols={{ base: 2, md: 5 }}>
        <div><Text size="xs" c="dimmed">Ürün</Text><Text fw={600}>{lot.item_name}</Text></div>
        <div><Text size="xs" c="dimmed">Durum</Text><LotStatusBadge status={lot.status} /></div>
        <div><Text size="xs" c="dimmed">İlk miktar</Text><Text fw={600}>{qty(lot.initial_qty, lot.uom)}</Text></div>
        <div><Text size="xs" c="dimmed">Eldeki miktar</Text><Text fw={600}>{qty(lot.qty_on_hand ?? '0', lot.uom)}</Text></div>
        <div><Text size="xs" c="dimmed">Birim maliyet</Text><Text fw={600}>{unitCost(lot.unit_cost)}</Text></div>
      </SimpleGrid>
      {reason && isLotHeld(lot.status) && (
        <Alert color="red" variant="light" mt="md" title={`${LOT_STATUS[lot.status].label}: satışa ve sevke kapalı`}>
          {reason.recallNo && <Text size="sm" mb={4}>Geri çağırma kaydı: <RecallLink recallNo={reason.recallNo} /></Text>}
          <Text size="sm">Sebep: {reason.text}</Text>
        </Alert>
      )}
    </Card>
  )
}

export default function Traceability() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const selectedLotId = params.get('lot_id') ?? ''
  const [recallOpened, { open: openRecall, close: closeRecall }] = useDisclosure(false)
  const [reason, setReason] = useState('')
  const [recallError, setRecallError] = useState<string | null>(null)
  const qc = useQueryClient()

  const genealogyQuery = useQuery({
    queryKey: ['genealogy', selectedLotId],
    queryFn: () => apiGet<GenealogyResponse>(`/lots/${selectedLotId}/genealogy`),
    enabled: !!selectedLotId,
  })
  const forwardQuery = useQuery({
    queryKey: ['trace-forward', selectedLotId],
    queryFn: () => apiGet<TraceForwardResponse>(`/lots/${selectedLotId}/trace-forward`),
    enabled: !!selectedLotId,
  })
  const backwardQuery = useQuery({
    queryKey: ['trace-backward', selectedLotId],
    queryFn: () => apiGet<TraceBackwardResponse>(`/lots/${selectedLotId}/trace-backward`),
    enabled: !!selectedLotId,
  })

  const select = (lotId: string) => setParams({ lot_id: lotId })

  const createRecall = useMutation({
    mutationFn: () => apiPost<{ id: string }>('/recalls', { root_lot_id: selectedLotId, reason }),
    onSuccess: (rc) => {
      closeRecall()
      setReason('')
      qc.invalidateQueries({ queryKey: ['recalls'] })
      navigate(`/geri-cagirma/${rc.id}`)
    },
    onError: (e) => setRecallError(err(e)),
  })

  return (
    <Stack>
      <PageHeader mb="xs" eyebrow="Bir lotun nereden geldiğini ve nereye gittiğini izle" title="İzlenebilirlik" />
      {!selectedLotId && <LotPicker initialQuery={params.get('q') ?? ''} onSelect={select} />}

      {selectedLotId && (
        <>
          <Group justify="space-between">
            <Title order={4}>{genealogyQuery.data?.root_lot_no ?? '…'}</Title>
            <Group>
              <Button variant="default" onClick={() => setParams({})}>Aramaya dön</Button>
              <Button color="red" onClick={openRecall}>Bu lottan geri çağırma başlat</Button>
            </Group>
          </Group>

          <LotInfoCard lotId={selectedLotId} />

          {genealogyQuery.error && <Alert color="red">{genealogyQuery.error.message}</Alert>}
          {genealogyQuery.isLoading && <Loader />}
          {genealogyQuery.data && <GenealogyTree data={genealogyQuery.data} onSelect={select} selectedLotId={selectedLotId} />}

          <Tabs defaultValue="forward">
            <Tabs.List>
              <Tabs.Tab value="forward">İleri İzleme</Tabs.Tab>
              <Tabs.Tab value="backward">Geri İzleme</Tabs.Tab>
            </Tabs.List>
            <Tabs.Panel value="forward" pt="md">
              {forwardQuery.isLoading && <Loader size="sm" />}
              {forwardQuery.data && <ForwardPanel data={forwardQuery.data} />}
            </Tabs.Panel>
            <Tabs.Panel value="backward" pt="md">
              {backwardQuery.isLoading && <Loader size="sm" />}
              {backwardQuery.data && <BackwardPanel data={backwardQuery.data} />}
            </Tabs.Panel>
          </Tabs>
        </>
      )}

      <Modal opened={recallOpened} onClose={closeRecall} title="Geri Çağırma Başlat">
        {recallError && <Alert color="red" mb="sm">{recallError}</Alert>}
        <Text size="sm" c="dimmed" mb="sm">Kök lot: {genealogyQuery.data?.root_lot_no}</Text>
        <Textarea label="Sebep" required value={reason} onChange={(e) => setReason(e.currentTarget.value)} mb="md" />
        <Group justify="flex-end">
          <Button variant="default" onClick={closeRecall}>Vazgeç</Button>
          <Button color="red" loading={createRecall.isPending} disabled={!reason} onClick={() => createRecall.mutate()}>
            Başlat
          </Button>
        </Group>
      </Modal>
    </Stack>
  )
}
