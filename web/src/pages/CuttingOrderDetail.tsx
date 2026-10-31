import {
  Alert, Anchor, Badge, Button, Card, Group, Loader, NumberInput, Progress, SimpleGrid, Stack, Table,
  Text, Tooltip,
} from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { PageHeader } from '../components/Page'
import { AnomalyAlertsCard } from '../components/AnomalyAlerts'
import {
  ApiError, apiGet, apiPost, apiSend, type CuttingOrder, type CuttingResult, type List, type StockMovement,
} from '../lib/api'
import { warningLabel } from '../lib/messages'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

function trNum(s: string | null | undefined): string {
  if (s == null) return '—'
  return s.replace('.', ',')
}

export default function CuttingOrderDetail() {
  const { id = '' } = useParams()
  const qc = useQueryClient()
  const orderQuery = useQuery({
    queryKey: ['cutting-order', id],
    queryFn: () => apiGet<CuttingOrder>(`/cutting-orders/${id}`),
    refetchInterval: 5_000,
  })
  const movementsQuery = useQuery({
    queryKey: ['cutting-order', id, 'movements'],
    queryFn: () => apiGet<List<StockMovement>>(`/cutting-orders/${id}/movements`),
    enabled: orderQuery.data?.status === 'closed',
  })

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['cutting-order', id] })
    qc.invalidateQueries({ queryKey: ['cutting-orders'] })
  }

  if (orderQuery.isLoading) return <Loader />
  if (orderQuery.error) return <Alert color="red">{err(orderQuery.error)}</Alert>
  const order = orderQuery.data!

  return (
    <Stack>
      <PageHeader mb="xs" title={order.order_no}
        eyebrow={<><Anchor component={Link} to="/parcalama" size="sm">Parçalama</Anchor> · {order.recipe_name} — {order.input_lot_no}</>}
        badges={<>
          <Badge color={order.status === 'closed' ? 'green' : 'gray'} variant="light">
            {order.status === 'closed' ? 'Kapandı' : 'Taslak'}
          </Badge>
          {order.warnings.map((w) => <Badge key={w} color="orange" variant="light">{warningLabel(w)}</Badge>)}
        </>} />

      <InputCard order={order} />

      {order.anomaly_alerts && (
        <AnomalyAlertsCard alerts={order.anomaly_alerts} queryKeysToInvalidate={[['cutting-order', id]]} />
      )}

      {order.outputs && (
        <OutputsForm order={order} onSaved={invalidate} />
      )}

      {order.balance_diff_kg != null && (
        <BalanceBar order={order} />
      )}

      {order.allocation && <AllocationComparison order={order} />}

      {order.status === 'draft' && <CloseCard order={order} onDone={invalidate} />}

      {order.status === 'closed' && order.outputs && <OutputLotsCard order={order} />}

      {movementsQuery.data && <MovementsCard movements={movementsQuery.data.items} />}
    </Stack>
  )
}

function InputCard({ order }: { order: CuttingOrder }) {
  return (
    <Card withBorder>
      <Text fw={600} mb="sm">Giriş</Text>
      <SimpleGrid cols={{ base: 2, md: 4 }}>
        <Stat label="Giriş Lotu" value={<Link to={`/lotlar?q=${order.input_lot_no}`}>{order.input_lot_no}</Link>} />
        <Stat label="Ağırlık" value={`${trNum(order.input_weight_kg)} kg`} />
        <Stat label="Birim Maliyet" value={`${trNum(order.unit_cost_per_kg)} €/kg`} />
        <Stat label="Dağıtım Yöntemi" value={order.allocation_method === 'value' ? 'Değer (€)' : 'Miktar (kg)'} />
        <Stat label="Hesaplanan Toplam" value={`${trNum(order.computed_total_cost)} €`} />
        <Stat label="Beyan Edilen Toplam" value={order.declared_total_cost ? `${trNum(order.declared_total_cost)} €` : '—'} />
        <Stat label="Kullanılan Esas" value={order.cost_basis_used === 'declared' ? 'Beyan edilen' : 'Hesaplanan'} />
        <Stat label="Toplam Maliyet (sabit)" value={`${trNum(order.total_input_cost)} €`} />
      </SimpleGrid>
      {order.resolution_note && (
        <Text size="sm" c="dimmed" mt="sm">Çözüm notu: {order.resolution_note}</Text>
      )}
      {order.warnings.includes('MISMATCH_RESOLUTION_MISSING') && (
        <Alert color="orange" mt="sm">
          Alış tutarı uyuşmazlığı çözümlenmemiş: bu emir <strong>oluşturulurken</strong> maliyet esası
          (cost_basis) ve not (resolution_note) verilmedi. Bu alanlar yalnızca emir oluşturulurken
          belirlenebilir — emri silip aynı lotla yeniden oluşturun.
        </Alert>
      )}
    </Card>
  )
}

function Stat({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div>
      <Text size="xs" c="dimmed">{label}</Text>
      <Text fw={600}>{value}</Text>
    </div>
  )
}

function OutputsForm({ order, onSaved }: { order: CuttingOrder; onSaved: () => void }) {
  const [weights, setWeights] = useState<Record<string, number | ''>>({})
  const [formError, setFormError] = useState<string | null>(null)

  useEffect(() => {
    const next: Record<string, number | ''> = {}
    for (const o of order.outputs ?? []) {
      next[o.part_item_id] = o.actual_weight_kg != null ? Number(o.actual_weight_kg) : ''
    }
    setWeights(next)
    // order.id degisince (farkli emir) sifirla; ayni emirde disaridan gelen guncellemeler kullanicinin
    // yazdigi degerleri ezmesin diye yalniz id bagimliligi kullanilir.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [order.id])

  const save = useMutation({
    mutationFn: () =>
      apiSend(
        'PUT',
        `/cutting-orders/${order.id}/outputs`,
        {
          outputs: (order.outputs ?? [])
            .filter((o) => weights[o.part_item_id] !== '')
            .map((o) => ({ part_item_id: o.part_item_id, actual_weight_kg: String(weights[o.part_item_id]) })),
        },
      ),
    onSuccess: onSaved,
    onError: (e) => setFormError(err(e)),
  })

  const readOnly = order.status !== 'draft'

  return (
    <Card withBorder>
      <Text fw={600} mb="sm">Fiili Ağırlıklar</Text>
      {formError && <Alert color="red" mb="sm">{formError}</Alert>}
      <Table>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Parça</Table.Th>
            <Table.Th ta="right">Beklenen (reçete)</Table.Th>
            <Table.Th ta="right">ML Tahmini</Table.Th>
            <Table.Th ta="right">Fiili</Table.Th>
            <Table.Th ta="right">Fark</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {(order.outputs ?? []).map((o) => {
            const actual = weights[o.part_item_id]
            const expected = Number(o.expected_weight_kg)
            const diff = actual === '' || actual == null ? null : actual - expected
            const diffPct = diff != null && expected > 0 ? (diff / expected) * 100 : null
            return (
              <Table.Tr key={o.part_item_id}>
                <Table.Td>
                  {o.part_item_name}
                  {o.is_waste && <Badge ml="xs" size="xs" color="gray" variant="outline">fire</Badge>}
                  {o.is_by_product && <Badge ml="xs" size="xs" color="blue" variant="outline">yan ürün</Badge>}
                </Table.Td>
                <Table.Td ta="right">{trNum(o.expected_weight_kg)} kg</Table.Td>
                <Table.Td ta="right">{o.predicted_weight_kg ? `${trNum(o.predicted_weight_kg)} kg` : '—'}</Table.Td>
                <Table.Td ta="right">
                  <NumberInput
                    disabled={readOnly}
                    min={0}
                    decimalScale={3}
                    value={actual}
                    onChange={(v) => setWeights((w) => ({ ...w, [o.part_item_id]: v as number }))}
                    w={110}
                    ml="auto"
                  />
                </Table.Td>
                <Table.Td ta="right">
                  {diff != null ? `${diff >= 0 ? '+' : ''}${trNum(diff.toFixed(3))} kg (${diffPct?.toFixed(1)}%)` : '—'}
                </Table.Td>
              </Table.Tr>
            )
          })}
        </Table.Tbody>
      </Table>
      {!readOnly && (
        <Group justify="flex-end" mt="sm">
          <Button loading={save.isPending} onClick={() => save.mutate()}>Kaydet</Button>
        </Group>
      )}
    </Card>
  )
}

function BalanceBar({ order }: { order: CuttingOrder }) {
  const input = Number(order.input_weight_kg)
  const diff = Number(order.balance_diff_kg ?? '0')
  const outputSum = input - diff
  const pct = input > 0 ? Math.min(100, Math.max(0, (outputSum / input) * 100)) : 0
  const ok = Math.abs(diff) <= 0.05
  return (
    <Card withBorder>
      <Group justify="space-between" mb="xs">
        <Text fw={500}>Ağırlık Dengesi</Text>
        <Text size="sm" c={ok ? 'green' : 'red'} fw={600}>
          {trNum(outputSum.toFixed(3))} / {trNum(order.input_weight_kg)} kg (fark {trNum(diff.toFixed(3))} kg)
        </Text>
      </Group>
      <Progress value={pct} color={ok ? 'green' : 'red'} size="lg" />
      {!ok && (
        <Text size="sm" c="red" mt="xs">
          Denge tutmuyor ({'>'}0,05 kg fark), kapatılamaz. Tüm parçaların (fire dahil) fiili ağırlığını girin.
        </Text>
      )}
    </Card>
  )
}

function AllocationTable({ title, result }: { title: string; result: CuttingResult }) {
  return (
    <div>
      <Text fw={500} mb="xs">{title}</Text>
      <Table>
        <Table.Thead>
          <Table.Tr><Table.Th>Parça</Table.Th><Table.Th ta="right">kg</Table.Th><Table.Th ta="right">Dağıtılan</Table.Th><Table.Th ta="right">Birim</Table.Th></Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {result.parts.map((p) => (
            <Table.Tr key={p.name}>
              <Table.Td>{p.name}</Table.Td>
              <Table.Td ta="right">{trNum(p.kg)}</Table.Td>
              <Table.Td ta="right">{trNum(p.allocated_cost)} €</Table.Td>
              <Table.Td ta="right">{trNum(p.unit_cost)} €/kg</Table.Td>
            </Table.Tr>
          ))}
          <Table.Tr>
            <Table.Td fw={700} colSpan={2}>Toplam</Table.Td>
            <Table.Td ta="right" fw={700}>{trNum(result.total_allocated)} €</Table.Td>
            <Table.Td />
          </Table.Tr>
        </Table.Tbody>
      </Table>
    </div>
  )
}

function AllocationComparison({ order }: { order: CuttingOrder }) {
  const alloc = order.allocation!
  return (
    <Card withBorder>
      <Group justify="space-between" mb="sm">
        <Text fw={500}>Dağıtım Önizlemesi — Miktar vs Değer</Text>
        <Text size="sm" c="dimmed">
          Her iki yöntemin toplamı giriş maliyetine ({trNum(order.total_input_cost)} €) birebir eşittir
        </Text>
      </Group>
      <SimpleGrid cols={{ base: 1, md: 2 }}>
        {alloc.quantity && (
          <AllocationTable
            title={`Miktar (kg) ${order.allocation_method === 'quantity' ? '— seçili' : ''}`}
            result={alloc.quantity}
          />
        )}
        {alloc.value && (
          <AllocationTable
            title={`Değer (€) ${order.allocation_method === 'value' ? '— seçili' : ''}`}
            result={alloc.value}
          />
        )}
      </SimpleGrid>
    </Card>
  )
}

function CloseCard({ order, onDone }: { order: CuttingOrder; onDone: () => void }) {
  const [formError, setFormError] = useState<string | null>(null)
  const close = useMutation({
    mutationFn: () => apiPost(`/cutting-orders/${order.id}/close`),
    onSuccess: onDone,
    onError: (e) => setFormError(err(e)),
  })
  return (
    <Card withBorder>
      <Text fw={600} mb="sm">Emri Kapat</Text>
      {formError && <Alert color="red" mb="sm">{formError}</Alert>}
      <Group justify="flex-end">
        <Tooltip label={order.warnings.map(warningLabel).join(', ') || 'Kapatılabilir'} disabled={order.closable}>
          <Button loading={close.isPending} disabled={!order.closable} onClick={() => close.mutate()}>
            Kapat
          </Button>
        </Tooltip>
      </Group>
    </Card>
  )
}

function OutputLotsCard({ order }: { order: CuttingOrder }) {
  return (
    <Card withBorder>
      <Text fw={600} mb="sm">Oluşan Lotlar</Text>
      <Table>
        <Table.Thead>
          <Table.Tr><Table.Th>Parça</Table.Th><Table.Th>Lot</Table.Th><Table.Th ta="right">Ağırlık</Table.Th><Table.Th ta="right">Birim Maliyet</Table.Th></Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {(order.outputs ?? []).filter((o) => o.output_lot_id).map((o) => (
            <Table.Tr key={o.part_item_id}>
              <Table.Td>{o.part_item_name}</Table.Td>
              <Table.Td><Link to={`/lotlar?q=${o.output_lot_no}`}>{o.output_lot_no}</Link></Table.Td>
              <Table.Td ta="right">{trNum(o.actual_weight_kg)} kg</Table.Td>
              <Table.Td ta="right">{trNum(o.unit_cost)} €/kg</Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Card>
  )
}

const MOVEMENT_LABEL: Record<string, string> = { cutting_consume: 'Parçalamada Tüketim', cutting_in: 'Parça Girişi' }

function MovementsCard({ movements }: { movements: StockMovement[] }) {
  return (
    <Card withBorder>
      <Text fw={600} mb="sm">Stok Hareketleri</Text>
      <Table>
        <Table.Thead>
          <Table.Tr><Table.Th>Lot</Table.Th><Table.Th>Tür</Table.Th><Table.Th ta="right">Miktar</Table.Th><Table.Th ta="right">Birim Maliyet</Table.Th></Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {movements.map((m) => (
            <Table.Tr key={m.id}>
              <Table.Td>{m.lot_no}</Table.Td>
              <Table.Td>{MOVEMENT_LABEL[m.movement_type] ?? m.movement_type}</Table.Td>
              <Table.Td ta="right">{trNum(m.qty)}</Table.Td>
              <Table.Td ta="right">{trNum(m.unit_cost)} €</Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Card>
  )
}
