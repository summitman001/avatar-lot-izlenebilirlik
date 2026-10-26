import { Alert, Badge, Button, Card, Group, Loader, Modal, NumberInput, Stack, Table, Text, Textarea, Title } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ApiError, apiGet, apiPost, type SalesOrder } from '../lib/api'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

export default function SalesOrderDetail() {
  const { id = '' } = useParams()
  const qc = useQueryClient()
  const orderQuery = useQuery({
    queryKey: ['sales-order', id],
    queryFn: () => apiGet<SalesOrder>(`/sales-orders/${id}`),
  })
  const [returnOpened, { open: openReturn, close: closeReturn }] = useDisclosure(false)
  const [returnLine, setReturnLine] = useState<{ id: string; label: string; customerId: string } | null>(null)

  if (orderQuery.isLoading) return <Loader />
  if (orderQuery.error) return <Alert color="red">{orderQuery.error.message}</Alert>
  const order = orderQuery.data!

  return (
    <Stack>
      <Group justify="space-between">
        <div>
          <Title order={2}>{order.order_no}</Title>
          <Text c="dimmed">{order.customer_name} — {new Date(order.sold_at).toLocaleString('tr-TR')}</Text>
        </div>
        <Group>
          <Badge color="teal" variant="light">Onaylandı</Badge>
          {order.has_violation && <Badge color="red" variant="filled">Geri çağırma ihlali</Badge>}
        </Group>
      </Group>

      <Card withBorder>
        <Text fw={500} mb="sm">Satış Satırları</Text>
        <Table>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Lot</Table.Th><Table.Th>Ürün</Table.Th>
              <Table.Th ta="right">Miktar</Table.Th><Table.Th ta="right">Birim Fiyat</Table.Th>
              <Table.Th ta="right">Tutar</Table.Th><Table.Th>Stok Hareketi</Table.Th><Table.Th></Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {order.lines?.map((l) => (
              <Table.Tr key={l.id}>
                <Table.Td>
                  <Link to={`/lotlar?q=${l.lot_no}`}>{l.lot_no}</Link>
                  {l.recall_violation && <Badge color="red" variant="filled" ml="xs">Bloke lot satışı</Badge>}
                </Table.Td>
                <Table.Td>{l.item_name}</Table.Td>
                <Table.Td ta="right">{l.qty}</Table.Td>
                <Table.Td ta="right">{l.unit_price} €</Table.Td>
                <Table.Td ta="right">{l.line_total} €</Table.Td>
                <Table.Td><Text size="xs" c="dimmed">{l.stock_movement_id ?? '—'}</Text></Table.Td>
                <Table.Td>
                  <Button size="xs" variant="subtle" onClick={() => { setReturnLine({ id: l.id, label: `${l.lot_no} (${l.qty})`, customerId: order.customer_id }); openReturn() }}>
                    İade Al
                  </Button>
                </Table.Td>
              </Table.Tr>
            ))}
            <Table.Tr>
              <Table.Td colSpan={4} fw={700}>Toplam</Table.Td>
              <Table.Td ta="right" fw={700}>{order.total_amount} €</Table.Td>
              <Table.Td /><Table.Td />
            </Table.Tr>
          </Table.Tbody>
        </Table>
      </Card>

      {order.shipments && order.shipments.length > 0 && (
        <Card withBorder>
          <Text fw={500} mb="sm">Sevkiyatlar</Text>
          <Table>
            <Table.Thead>
              <Table.Tr><Table.Th>Sevkiyat No</Table.Th><Table.Th>Tarih</Table.Th><Table.Th>Lot / Miktar</Table.Th></Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {order.shipments.map((sh) => (
                <Table.Tr key={sh.id}>
                  <Table.Td>{sh.shipment_no}</Table.Td>
                  <Table.Td>{new Date(sh.shipped_at).toLocaleString('tr-TR')}</Table.Td>
                  <Table.Td>
                    {sh.lines?.map((l) => (
                      <Text key={l.id} size="sm">{l.lot_no}: {l.qty}</Text>
                    ))}
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Card>
      )}

      <ReturnModal
        opened={returnOpened}
        onClose={closeReturn}
        salesLine={returnLine}
        onDone={() => qc.invalidateQueries({ queryKey: ['sales-order', id] })}
      />
    </Stack>
  )
}

function ReturnModal({ opened, onClose, salesLine, onDone }: {
  opened: boolean
  onClose: () => void
  salesLine: { id: string; label: string; customerId: string } | null
  onDone: () => void
}) {
  const [qty, setQty] = useState<number | ''>('')
  const [reason, setReason] = useState('')
  const [formError, setFormError] = useState<string | null>(null)

  const create = useMutation({
    mutationFn: () => {
      if (!salesLine) throw new Error('satis satiri secilmedi')
      return apiPost(`/customer-returns`, {
        customer_id: salesLine.customerId,
        sales_line_id: salesLine.id,
        qty: String(qty),
        reason,
      })
    },
    onSuccess: () => {
      setQty('')
      setReason('')
      onDone()
      onClose()
    },
    onError: (e) => setFormError(err(e)),
  })

  return (
    <Modal opened={opened} onClose={onClose} title="Müşteri İadesi">
      {formError && <Alert color="red" mb="sm">{formError}</Alert>}
      <Text size="sm" c="dimmed" mb="sm">Satış satırı: {salesLine?.label}</Text>
      <NumberInput label="İade Miktarı" required min={0} decimalScale={3} value={qty} onChange={(v) => setQty(v as number)} mb="sm" />
      <Textarea label="Sebep" required value={reason} onChange={(e) => setReason(e.currentTarget.value)} mb="md" />
      <Group justify="flex-end">
        <Button variant="default" onClick={onClose}>Vazgeç</Button>
        <Button loading={create.isPending} disabled={qty === '' || !reason} onClick={() => create.mutate()}>İadeyi Kaydet</Button>
      </Group>
    </Modal>
  )
}
