import { Alert, Anchor, Badge, Button, Card, Loader, Select, Stack, Table, Text, Tooltip } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { IconDownload } from '@tabler/icons-react'
import { Link, useParams } from 'react-router-dom'
import { PageHeader } from '../components/Page'
import { API_URL, ApiError, apiGet, apiPatch, apiPost, type Recall } from '../lib/api'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

const CONTACT_LABEL: Record<string, string> = { pending: 'Bekliyor', contacted: 'İletişime geçildi', confirmed: 'Onaylandı' }

export default function RecallDetail() {
  const { id = '' } = useParams()
  const qc = useQueryClient()
  const [closeError, setCloseError] = useState<string | null>(null)

  const { data, isLoading, error } = useQuery({
    queryKey: ['recall', id],
    queryFn: () => apiGet<Recall>(`/recalls/${id}`),
  })
  const invalidate = () => qc.invalidateQueries({ queryKey: ['recall', id] })

  const patchCustomer = useMutation({
    mutationFn: (v: { recallCustomerId: string; contact_status: string }) =>
      apiPatch(`/recalls/${id}/customers/${v.recallCustomerId}`, { contact_status: v.contact_status }),
    onSuccess: invalidate,
  })
  const closeRecall = useMutation({
    mutationFn: () => apiPost(`/recalls/${id}/close`),
    onSuccess: invalidate,
    onError: (e) => setCloseError(err(e)),
  })
  const reopenRecall = useMutation({
    mutationFn: () => apiPost(`/recalls/${id}/reopen`),
    onSuccess: invalidate,
  })

  if (isLoading) return <Loader />
  if (error) return <Alert color="red">{error.message}</Alert>
  const recall = data!

  return (
    <Stack>
      <PageHeader mb="xs" title={recall.recall_no}
        eyebrow={<>
          <Anchor component={Link} to="/geri-cagirma" size="sm">Geri Çağırma</Anchor> · Kök lot:{' '}
          <Link to={`/izlenebilirlik?lot_id=${recall.root_lot_id}`}>{recall.root_lot_no}</Link> — {recall.reason}
        </>}
        badges={<Badge color={recall.status === 'open' ? 'red' : 'gray'} variant="filled" size="lg">
          {recall.status === 'open' ? 'Açık' : 'Kapalı'}
        </Badge>}
        actions={<>
          <Button component="a" href={`${API_URL}/recalls/${id}/contact-list?format=csv`} variant="default"
            leftSection={<IconDownload size={16} />}>
            İletişim Listesi (CSV)
          </Button>
          {recall.status === 'open' ? (
            <Button color="teal" loading={closeRecall.isPending} onClick={() => closeRecall.mutate()}>Kapat</Button>
          ) : (
            <Button variant="default" loading={reopenRecall.isPending} onClick={() => reopenRecall.mutate()}>Tekrar Aç</Button>
          )}
        </>} />
      {closeError && <Alert color="red" onClose={() => setCloseError(null)} withCloseButton>{closeError}</Alert>}

      <Card withBorder>
        <Text fw={600} mb="sm">Etkilenen Lotlar ({recall.affected_lot_count})</Text>
        <Table striped>
          <Table.Thead>
            <Table.Tr><Table.Th>Lot</Table.Th><Table.Th>Ürün</Table.Th><Table.Th ta="right">Derinlik</Table.Th><Table.Th>Durum</Table.Th></Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {recall.affected_lots?.map((l) => (
              <Table.Tr key={l.lot_id}>
                <Table.Td><Link to={`/izlenebilirlik?lot_id=${l.lot_id}`}>{l.lot_no}</Link></Table.Td>
                <Table.Td>{l.item_name}</Table.Td>
                <Table.Td ta="right">{l.depth}</Table.Td>
                <Table.Td><Badge color="red" variant="light">Bloke (geri çağrıldı)</Badge></Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Card>

      <Card withBorder>
        <Text fw={600} mb="sm">Müşteri / Market Tablosu ({recall.pending_customer_count} bekliyor)</Text>
        <Table>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Müşteri</Table.Th><Table.Th>Telefon/Mail</Table.Th><Table.Th>Lot</Table.Th>
              <Table.Th ta="right">Sevk</Table.Th><Table.Th ta="right">İade</Table.Th><Table.Th ta="right">Kalan Risk</Table.Th>
              <Table.Th>İkinci Kademe</Table.Th><Table.Th>Durum</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {recall.customers?.map((c) => (
              <Table.Tr key={c.id}>
                <Table.Td>
                  {c.customer_name}
                  {c.has_violation && <Badge color="red" variant="filled" ml="xs" size="xs">offline ihlal</Badge>}
                </Table.Td>
                <Table.Td><Text size="xs">{[c.customer_phone, c.customer_email].filter(Boolean).join(' · ') || '—'}</Text></Table.Td>
                <Table.Td>{c.lot_no}</Table.Td>
                <Table.Td ta="right">{c.qty_shipped}</Table.Td>
                <Table.Td ta="right">{c.qty_returned}</Table.Td>
                <Table.Td ta="right">{c.remaining_risk_qty}</Table.Td>
                <Table.Td>
                  {c.downstream_known ? (
                    <Badge color="green" variant="light">Bilinen</Badge>
                  ) : (
                    <Tooltip label={c.downstream_note}>
                      <Badge color="gray" variant="outline">Bilinmiyor</Badge>
                    </Tooltip>
                  )}
                </Table.Td>
                <Table.Td>
                  <Select
                    size="xs"
                    w={150}
                    value={c.contact_status}
                    data={Object.entries(CONTACT_LABEL).map(([value, label]) => ({ value, label }))}
                    onChange={(v) => v && patchCustomer.mutate({ recallCustomerId: c.id, contact_status: v })}
                  />
                </Table.Td>
              </Table.Tr>
            ))}
            {(recall.customers?.length ?? 0) === 0 && (
              <Table.Tr><Table.Td colSpan={8}><Text c="dimmed" size="sm">Bu lotlar hiçbir müşteriye sevk edilmemiş</Text></Table.Td></Table.Tr>
            )}
          </Table.Tbody>
        </Table>
      </Card>
    </Stack>
  )
}
