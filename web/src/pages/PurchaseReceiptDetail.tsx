import { Alert, Badge, Card, Group, Loader, SimpleGrid, Stack, Table, Text, Title, Tooltip } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { ApiError, apiGet, type PurchaseReceiptDetail } from '../lib/api'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

function Stat({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div>
      <Text size="xs" c="dimmed">{label}</Text>
      <Text fw={600}>{value}</Text>
    </div>
  )
}

export default function PurchaseReceiptDetailPage() {
  const { id = '' } = useParams()
  const { data, isLoading, error } = useQuery({
    queryKey: ['purchase-receipt', id],
    queryFn: () => apiGet<PurchaseReceiptDetail>(`/purchase-receipts/${id}`),
  })

  if (isLoading) return <Loader />
  if (error) return <Alert color="red">{err(error)}</Alert>
  const r = data!

  return (
    <Stack>
      <Group justify="space-between">
        <div>
          <Title order={2}>{r.doc_no}</Title>
          <Text c="dimmed">{r.supplier_name} — {r.received_at}</Text>
        </div>
        {r.has_mismatch && <Badge color="red" variant="filled">Tutar uyuşmazlığı</Badge>}
      </Group>

      <Card withBorder>
        <SimpleGrid cols={{ base: 2, md: 4 }}>
          <Stat label="Tedarikçi" value={r.supplier_name} />
          <Stat label="Fatura No" value={r.invoice_no ?? '—'} />
          <Stat label="Tarih" value={r.received_at} />
          <Stat label="Toplam Tutar" value={`${r.total_amount} €`} />
        </SimpleGrid>
      </Card>

      <Card withBorder>
        <Text fw={500} mb="sm">Satırlar</Text>
        <Table>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Ürün</Table.Th>
              <Table.Th>Lot</Table.Th>
              <Table.Th ta="right">Miktar</Table.Th>
              <Table.Th ta="right">Birim Fiyat</Table.Th>
              <Table.Th ta="right">Hesaplanan Tutar</Table.Th>
              <Table.Th ta="right">Beyan Edilen Tutar</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {r.lines.map((l) => (
              <Table.Tr key={l.id} bg={l.has_mismatch ? 'red.0' : undefined}>
                <Table.Td>{l.item_name} <Text span size="xs" c="dimmed">({l.item_code})</Text></Table.Td>
                <Table.Td><Link to={`/lotlar?q=${l.lot_no}`}>{l.lot_no}</Link></Table.Td>
                <Table.Td ta="right">{l.qty}</Table.Td>
                <Table.Td ta="right">{l.unit_price} €</Table.Td>
                <Table.Td ta="right" fw={l.has_mismatch ? 700 : undefined}>{l.line_total} €</Table.Td>
                <Table.Td ta="right" c={l.has_mismatch ? 'red' : undefined} fw={l.has_mismatch ? 700 : undefined}>
                  {l.declared_line_total ?? '—'} {l.declared_line_total && '€'}
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
        {r.has_mismatch && (
          <Alert color="red" mt="sm">
            Kırmızı işaretli satırlarda hesaplanan tutar (miktar × birim fiyat) belgede beyan edilen
            tutardan farklı. Hangisinin doğru olduğu ayrıca araştırılmalıdır (bkz. docs/assumptions.md).
          </Alert>
        )}
      </Card>

      <Card withBorder>
        <Text fw={500} mb="sm">Oluşan Lotlar</Text>
        <Table>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Lot</Table.Th>
              <Table.Th>Ürün</Table.Th>
              <Table.Th ta="right">İlk Miktar</Table.Th>
              <Table.Th ta="right">Eldeki Miktar</Table.Th>
              <Table.Th>Durum</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {r.lots.map((lot) => (
              <Table.Tr key={lot.id}>
                <Table.Td><Link to={`/lotlar?q=${lot.lot_no}`}>{lot.lot_no}</Link></Table.Td>
                <Table.Td>{lot.item_name}</Table.Td>
                <Table.Td ta="right">{lot.initial_qty} {lot.uom}</Table.Td>
                <Table.Td ta="right">{lot.qty_on_hand ?? '—'} {lot.uom}</Table.Td>
                <Table.Td>
                  <Tooltip label={lot.blocked_reason ?? ''} disabled={!lot.blocked_reason}>
                    <Badge color={lot.status === 'active' ? 'green' : lot.status === 'blocked' ? 'orange' : 'gray'} variant="light">
                      {lot.status}
                    </Badge>
                  </Tooltip>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Card>

      <Card withBorder>
        <Text fw={500} mb="sm">Stok Hareketleri</Text>
        <Table>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Lot</Table.Th>
              <Table.Th ta="right">Miktar</Table.Th>
              <Table.Th ta="right">Birim Maliyet</Table.Th>
              <Table.Th>Tarih</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {r.movements.map((m) => (
              <Table.Tr key={m.id}>
                <Table.Td>{m.lot_no}</Table.Td>
                <Table.Td ta="right">{m.qty}</Table.Td>
                <Table.Td ta="right">{m.unit_cost} €</Table.Td>
                <Table.Td>{new Date(m.created_at).toLocaleString('tr-TR')}</Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Card>
    </Stack>
  )
}
