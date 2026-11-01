import { Alert, Anchor, Badge, Button, Card, Group, Loader, Modal, NumberInput, Stack, Table, Text, Textarea } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { PageHeader } from '../components/Page'
import { ApiError, apiGet, apiPost, type SalesOrder } from '../lib/api'
import { money, qty, unitCost } from '../lib/format'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

// Miktar 3 ondalıklı (DECIMAL(14,3)): binde birlik tamsayıda kayan nokta hatası olmadan çıkarılır.
const milli = (v: string) => Math.round(Number(v) * 1000)

const REVIEW_BADGE = {
  open: { color: 'orange', label: 'İncelemede' },
  reviewed: { color: 'teal', label: 'İncelendi' },
  returned: { color: 'gray', label: 'İade edildi' },
} as const

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
  const recallViolation = order.lines?.some((l) => l.recall_violation)
  const negativeStock = order.lines?.some((l) => l.negative_stock_violation)

  return (
    <Stack>
      <PageHeader mb="xs" title={order.order_no}
        eyebrow={<><Anchor component={Link} to="/satis-sevkiyat" size="sm">Satış / Sevkiyat</Anchor> · {order.customer_name} — {new Date(order.sold_at).toLocaleString('tr-TR')}</>}
        badges={<>
          <Badge color="teal" variant="light">Onaylandı</Badge>
          {recallViolation && <Badge color="red" variant="filled">Geri çağırma ihlali</Badge>}
          {negativeStock && <Badge color="red" variant="filled">Eksi stok (çevrimdışı)</Badge>}
          {order.review_status && (
            <Badge color={REVIEW_BADGE[order.review_status].color} variant="light">{REVIEW_BADGE[order.review_status].label}</Badge>
          )}
        </>} />

      <Card withBorder>
        <Text fw={600} mb="sm">Satış Satırları</Text>
        <Table>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Lot</Table.Th><Table.Th>Ürün</Table.Th>
              <Table.Th ta="right">Miktar</Table.Th><Table.Th ta="right">Birim Fiyat</Table.Th>
              <Table.Th ta="right">Tutar</Table.Th><Table.Th>Stok Hareketi</Table.Th><Table.Th></Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {order.lines?.map((l) => {
              const left = milli(l.qty) - milli(l.returned_qty) // iade edilebilir, binde bir
              return (
                <Table.Tr key={l.id}>
                  <Table.Td>
                    <Link to={`/lotlar?q=${l.lot_no}`}>{l.lot_no}</Link>
                    {l.recall_violation && <Badge color="red" variant="filled" ml="xs">Bloke lot satışı</Badge>}
                    {l.negative_stock_violation && <Badge color="red" variant="filled" ml="xs">Stok yetersizken satıldı</Badge>}
                    {milli(l.returned_qty) > 0 && <Badge color="gray" variant="light" ml="xs">İade: {qty(l.returned_qty)}</Badge>}
                  </Table.Td>
                  <Table.Td>{l.item_name}</Table.Td>
                  <Table.Td ta="right">{qty(l.qty)}</Table.Td>
                  <Table.Td ta="right">{unitCost(l.unit_price)}</Table.Td>
                  <Table.Td ta="right">{money(l.line_total)}</Table.Td>
                  <Table.Td><Text size="xs" c="dimmed">{l.stock_movement_id ?? '—'}</Text></Table.Td>
                  <Table.Td>
                    {left > 0 && (
                      <Button size="xs" variant="subtle" onClick={() => { setReturnLine({ id: l.id, label: `${l.lot_no} (satılan ${qty(l.qty)}, iade edilebilir ${qty((left / 1000).toFixed(3))})`, customerId: order.customer_id }); openReturn() }}>
                        İade Al
                      </Button>
                    )}
                  </Table.Td>
                </Table.Tr>
              )
            })}
            <Table.Tr>
              <Table.Td colSpan={4} fw={700}>Toplam</Table.Td>
              <Table.Td ta="right" fw={700}>{money(order.total_amount)}</Table.Td>
              <Table.Td /><Table.Td />
            </Table.Tr>
          </Table.Tbody>
        </Table>
      </Card>

      {order.shipments && order.shipments.length > 0 && (
        <Card withBorder>
          <Text fw={600} mb="sm">Sevkiyatlar</Text>
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
                      <Text key={l.id} size="sm">{l.lot_no}: {qty(l.qty)}</Text>
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
