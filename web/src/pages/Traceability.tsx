import { Alert, Badge, Button, Card, Group, Loader, Modal, Stack, Table, Tabs, Text, TextInput, Textarea, Title, Tooltip } from '@mantine/core'
import { useDebouncedValue, useDisclosure } from '@mantine/hooks'
import { IconSearch } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { PageHeader } from '../components/Page'
import {
  ApiError, apiGet, apiPost,
  type GenealogyResponse, type List, type Lot,
  type TraceBackwardResponse, type TraceForwardResponse,
} from '../lib/api'

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
                <Table.Td ta="right">{e.qty} {e.uom}</Table.Td>
                <Table.Td>{e.process_type}</Table.Td>
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
                <Table.Td ta="right">{n.qty_on_hand_total} {n.uom}</Table.Td>
                <Table.Td ta="right">{n.returned_qty} {n.uom}</Table.Td>
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
                <Table.Td ta="right">{c.qty_shipped}</Table.Td>
                <Table.Td ta="right">{c.qty_returned}</Table.Td>
                <Table.Td>
                  {c.downstream_known ? (
                    <Badge color="green" variant="light">Kayıtlı</Badge>
                  ) : (
                    <Tooltip label={c.downstream_note}>
                      <Badge color="gray" variant="outline">Bilinmiyor</Badge>
                    </Tooltip>
                  )}
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
                  <Table.Td ta="right">{s.qty}</Table.Td>
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
              <Table.Td ta="right">{n.qty_used ?? '—'} {n.uom}</Table.Td>
              <Table.Td>{n.received_or_produced_at}</Table.Td>
              <Table.Td>{n.process_type}{n.order_no ? ` (${n.order_no})` : ''}</Table.Td>
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

export default function Traceability() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const [q, setQ] = useState(params.get('q') ?? '')
  const [debouncedQ] = useDebouncedValue(q, 300)
  const selectedLotId = params.get('lot_id') ?? ''
  const [recallOpened, { open: openRecall, close: closeRecall }] = useDisclosure(false)
  const [reason, setReason] = useState('')
  const [recallError, setRecallError] = useState<string | null>(null)
  const qc = useQueryClient()

  const searchQuery = useQuery({
    queryKey: ['lots-search', debouncedQ],
    queryFn: () => apiGet<List<Lot>>('/lots', { q: debouncedQ }),
    enabled: debouncedQ.length > 0,
  })

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
      <TextInput
        radius="xl"
        size="md"
        leftSection={<IconSearch size={18} />}
        placeholder="Lot no veya ürün adı ara"
        value={q}
        onChange={(e) => setQ(e.currentTarget.value)}
      />
      {debouncedQ && !selectedLotId && (
        <Card withBorder>
          {searchQuery.isLoading && <Loader size="sm" />}
          {searchQuery.data && searchQuery.data.items.length === 0 && <Text c="dimmed" size="sm">Sonuç yok</Text>}
          {searchQuery.data && searchQuery.data.items.length > 0 && (
            <Table highlightOnHover>
              <Table.Tbody>
                {searchQuery.data.items.map((lot) => (
                  <Table.Tr key={lot.id} style={{ cursor: 'pointer' }} onClick={() => select(lot.id)}>
                    <Table.Td>{lot.lot_no}</Table.Td>
                    <Table.Td>{lot.item_name} <Text span size="xs" c="dimmed">({lot.item_code})</Text></Table.Td>
                    <Table.Td><Badge color={STATUS_COLOR[lot.status]} variant="light">{STATUS_LABEL[lot.status]}</Badge></Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Card>
      )}

      {selectedLotId && (
        <>
          <Group justify="space-between">
            <Title order={4}>{genealogyQuery.data?.root_lot_no ?? '…'}</Title>
            <Group>
              <Button variant="default" onClick={() => setParams({})}>Aramaya dön</Button>
              <Button color="red" onClick={openRecall}>Bu lottan geri çağırma başlat</Button>
            </Group>
          </Group>

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
