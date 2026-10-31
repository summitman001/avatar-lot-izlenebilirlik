// Özet pano: mevcut uç noktalardan beslenen kartlar. Yeni iş hesabı yoktur; API'den geleni gösterir,
// yalnızca satış tutarlarını güne göre toplar. Açık anomali özeti docs/anomaly-integration.md; diğerleri
// ilgili sayfanın kendi uç noktasıdır (recalls/sales-orders/lots/production-orders/anomaly-alerts).
import {
  ActionIcon, Alert, Badge, Box, Button, Card, Grid, Group, Loader, Progress, SimpleGrid, Stack, Table, Text,
  ThemeIcon, Title,
} from '@mantine/core'
import {
  IconArrowBackUp, IconArrowUpRight, IconCash, IconLock, IconRoute, IconTruckDelivery, type Icon as TablerIcon,
} from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { useState, type MouseEvent, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { SEVERITY_COLOR, SeverityBadge } from '../components/AnomalyAlerts'
import { eur } from '../components/Page'
import {
  ApiError, apiGet, type AnomalyAlert, type AnomalyAlertSummary, type AnomalySeverity, type List,
  type Lot, type ProductionOrder, type Recall, type SalesOrder,
} from '../lib/api'
import { STATUS as PRODUCTION_STATUS } from './ProductionOrders'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

const SEVERITIES: AnomalySeverity[] = ['critical', 'warning', 'info']
const SALES_DAYS = 30
const SALES_LIMIT = 1000 // backend MaxLimit

// UTC gün (YYYY-MM-DD); backend'in from/to filtresiyle aynı gün sınırı.
function utcDay(offsetDays = 0): string {
  const d = new Date()
  d.setUTCDate(d.getUTCDate() + offsetDays)
  return d.toISOString().slice(0, 10)
}

const shortDate = (day: string) =>
  new Date(day).toLocaleDateString('tr-TR', { day: 'numeric', month: 'short', timeZone: 'UTC' })

// Noktalar arası yumuşak eğri; kontrol noktaları yatay olduğu için tepe/çukur aşımı yapmaz.
function smoothPath(values: number[], w: number, h: number, max: number): string {
  const x = (i: number) => (values.length > 1 ? (i / (values.length - 1)) * w : 0)
  const y = (v: number) => h - (v / max) * h
  let d = `M${x(0)},${y(values[0])}`
  for (let i = 1; i < values.length; i++) {
    const mx = (x(i - 1) + x(i)) / 2
    d += ` C${mx},${y(values[i - 1])} ${mx},${y(values[i])} ${x(i)},${y(values[i])}`
  }
  return d
}

function Sparkline({ values }: { values: number[] }) {
  const max = Math.max(...values, 1)
  return (
    <svg width={84} height={28} viewBox="0 0 84 28" aria-hidden style={{ overflow: 'visible' }}>
      <path d={smoothPath(values, 84, 26, max)} fill="none" stroke="var(--mantine-primary-color-filled)" strokeWidth={2} />
    </svg>
  )
}

const CHART_H = 220

// Son 30 günün günlük satış tutarı: tek seri alan grafiği, üzerine gelince gün/tutar gösterir.
function SalesChart({ days, totals }: { days: string[]; totals: number[] }) {
  const [hover, setHover] = useState<number | null>(null)
  const max = Math.max(...totals) * 1.1
  const line = smoothPath(totals, 1000, CHART_H, max)
  const pct = (i: number) => (i / (totals.length - 1)) * 100
  const onMove = (e: MouseEvent<HTMLDivElement>) => {
    const r = e.currentTarget.getBoundingClientRect()
    const i = Math.round(((e.clientX - r.left) / r.width) * (totals.length - 1))
    setHover(Math.min(totals.length - 1, Math.max(0, i)))
  }
  const ticks = [0, 7, 14, 21, days.length - 1]
  return (
    <Group align="stretch" gap="xs" wrap="nowrap">
      <Stack justify="space-between" w={72} h={CHART_H} style={{ flexShrink: 0 }}>
        {[max, max / 2, 0].map((v) => (
          <Text key={v} size="xs" c="dimmed" ta="right" lh={1}>{eur(v)}</Text>
        ))}
      </Stack>
      <Box style={{ flex: 1, minWidth: 0 }}>
        <Box
          pos="relative"
          h={CHART_H}
          onMouseMove={onMove}
          onMouseLeave={() => setHover(null)}
          role="img"
          aria-label={`Son ${days.length} gün satış; toplam ${eur(totals.reduce((a, b) => a + b, 0))}`}
        >
          <svg width="100%" height={CHART_H} viewBox={`0 0 1000 ${CHART_H}`} preserveAspectRatio="none" style={{ display: 'block' }}>
            <defs>
              <linearGradient id="sales-fill" x1="0" x2="0" y1="0" y2="1">
                <stop offset="0%" stopColor="var(--mantine-primary-color-filled)" stopOpacity={0.22} />
                <stop offset="100%" stopColor="var(--mantine-primary-color-filled)" stopOpacity={0} />
              </linearGradient>
            </defs>
            {[0, CHART_H / 2, CHART_H].map((y) => (
              <line key={y} x1={0} x2={1000} y1={y} y2={y} stroke="var(--app-border)" vectorEffect="non-scaling-stroke" />
            ))}
            <path d={`${line} L1000,${CHART_H} L0,${CHART_H} Z`} fill="url(#sales-fill)" />
            <path d={line} fill="none" stroke="var(--mantine-primary-color-filled)" strokeWidth={2} vectorEffect="non-scaling-stroke" />
            {hover != null && (
              <line x1={pct(hover) * 10} x2={pct(hover) * 10} y1={0} y2={CHART_H} stroke="var(--mantine-color-dimmed)" strokeDasharray="3 3" vectorEffect="non-scaling-stroke" />
            )}
          </svg>
          {hover != null && (
            <>
              <Box
                pos="absolute"
                w={10}
                h={10}
                style={{
                  left: `${pct(hover)}%`, top: CHART_H - (totals[hover] / max) * CHART_H,
                  transform: 'translate(-50%, -50%)', borderRadius: 99,
                  background: 'var(--mantine-primary-color-filled)', border: '2px solid var(--mantine-color-body)',
                }}
              />
              <Card
                withBorder
                shadow="md"
                padding="xs"
                radius="md"
                pos="absolute"
                style={{
                  top: 8, left: `${pct(hover)}%`, pointerEvents: 'none', whiteSpace: 'nowrap',
                  transform: `translateX(${pct(hover) > 70 ? 'calc(-100% - 12px)' : '12px'})`,
                }}
              >
                <Text size="xs" c="dimmed">{shortDate(days[hover])}</Text>
                <Text size="sm" fw={700}>{eur(totals[hover])}</Text>
              </Card>
            </>
          )}
        </Box>
        <Box pos="relative" h={20} mt={6}>
          {ticks.map((i) => (
            <Text
              key={i}
              size="xs"
              c="dimmed"
              pos="absolute"
              style={{ left: `${pct(i)}%`, whiteSpace: 'nowrap', transform: i === 0 ? undefined : i === days.length - 1 ? 'translateX(-100%)' : 'translateX(-50%)' }}
            >
              {shortDate(days[i])}
            </Text>
          ))}
        </Box>
      </Box>
    </Group>
  )
}

function Kpi({ to, label, value, sub, icon: Icon, color, valueColor, children }: {
  to: string; label: string; value: ReactNode; sub: ReactNode; icon: TablerIcon; color: string; valueColor?: string; children?: ReactNode
}) {
  return (
    <Card withBorder component={Link} to={to} h="100%">
      <Group justify="space-between" align="flex-start" wrap="nowrap">
        <Text size="sm" fw={500} c="dimmed">{label}</Text>
        <ThemeIcon variant="light" color={color} radius="xl" size={36}><Icon size={18} /></ThemeIcon>
      </Group>
      <Text fz={28} fw={700} lh={1.2} c={valueColor}>{value}</Text>
      <Group justify="space-between" align="flex-end" mt="md" wrap="nowrap">
        <Text size="xs" c="dimmed">{sub}</Text>
        {children}
      </Group>
    </Card>
  )
}

function CardTitle({ title, sub, to }: { title: string; sub?: string; to: string }) {
  return (
    <Group justify="space-between" align="flex-start" mb="md" wrap="nowrap">
      <div>
        <Text fw={600}>{title}</Text>
        {sub && <Text size="xs" c="dimmed">{sub}</Text>}
      </div>
      <ActionIcon component={Link} to={to} variant="default" radius="xl" size={32} aria-label={`${title}: tümü`}>
        <IconArrowUpRight size={16} />
      </ActionIcon>
    </Group>
  )
}

export default function Dashboard() {
  const alertSummary = useQuery({
    queryKey: ['anomaly-alert-summary'],
    queryFn: () => apiGet<AnomalyAlertSummary>('/anomaly-alerts/summary'),
    refetchInterval: 15_000,
  })
  const recalls = useQuery({
    queryKey: ['recalls', 'dashboard'],
    queryFn: () => apiGet<List<Recall>>('/recalls', { limit: '1000' }),
  })
  const sales = useQuery({
    queryKey: ['sales-orders', 'dashboard', SALES_DAYS],
    queryFn: () => apiGet<List<SalesOrder>>('/sales-orders', {
      from: utcDay(1 - SALES_DAYS), to: utcDay(), limit: String(SALES_LIMIT),
    }),
  })
  const blockedLots = useQuery({
    queryKey: ['lots', 'dashboard', 'blocked'],
    queryFn: () => apiGet<List<Lot>>('/lots', { status: 'blocked', limit: '1000' }),
  })
  const recalledLots = useQuery({
    queryKey: ['lots', 'dashboard', 'recalled'],
    queryFn: () => apiGet<List<Lot>>('/lots', { status: 'recalled', limit: '1000' }),
  })
  const lastProduction = useQuery({
    queryKey: ['production-orders', 'dashboard'],
    queryFn: () => apiGet<List<ProductionOrder>>('/production-orders', { limit: '5' }),
  })
  const lastAlerts = useQuery({
    queryKey: ['anomaly-alerts', 'dashboard'],
    queryFn: () => apiGet<List<AnomalyAlert>>('/anomaly-alerts', { limit: '5' }),
  })

  const openRecallCount = recalls.data?.items.filter((r) => r.status === 'open').length
  // Geri çağrılan lotlar da satışa kapalıdır (Android'in blocked_lot_ids listesiyle aynı kapsam).
  const recalledCount = recalledLots.data?.items.length
  const blockedCount = blockedLots.data && recalledLots.data
    ? blockedLots.data.items.length + recalledLots.data.items.length
    : undefined
  const blockedLoading = { isLoading: blockedLots.isLoading || recalledLots.isLoading }

  const days = Array.from({ length: SALES_DAYS }, (_, i) => utcDay(i + 1 - SALES_DAYS))
  const byDay = new Map(days.map((d) => [d, 0]))
  let todaysOrders = 0
  for (const o of sales.data?.items ?? []) {
    const d = new Date(o.sold_at).toISOString().slice(0, 10)
    if (byDay.has(d)) byDay.set(d, byDay.get(d)! + Number(o.total_amount))
    if (d === days[days.length - 1]) todaysOrders++
  }
  const totals = days.map((d) => byDay.get(d)!)
  const todaysTotal = totals[totals.length - 1]
  // Liste en yeni siparişten başlar; limit dolduysa yalnız en eski günler eksik kalır.
  const truncated = sales.data?.items.length === SALES_LIMIT

  const dash = (q: { isLoading: boolean }, v: ReactNode) => (q.isLoading ? '…' : v ?? '—')

  return (
    <Stack gap="lg">
      <Grid gutter="lg" align="stretch">
        <Grid.Col span={{ base: 12, lg: 3 }}>
          <Stack gap="xs" justify="center" h="100%">
            <Badge
              variant="default"
              size="lg"
              radius="xl"
              tt="none"
              fw={500}
              w="fit-content"
              leftSection={<Box w={7} h={7} bg="var(--mantine-primary-color-filled)" style={{ borderRadius: 99 }} />}
            >
              {new Date().toLocaleDateString('tr-TR', { weekday: 'long', day: 'numeric', month: 'long' })}
            </Badge>
            <Title order={1} fz={32} lh={1.15}>Hoş geldiniz!</Title>
            <Text size="sm" c="dimmed">
              Bugün <b>{dash(sales, todaysOrders)} satış</b> yapıldı; <b>{dash(blockedLoading, blockedCount)} bloke lot</b>{' '}
              ve <b>{dash(alertSummary, alertSummary.data?.total)} açık uyarı</b> var.
            </Text>
            <Group gap="xs" mt={4}>
              <Button component={Link} to="/alislar" size="sm" leftSection={<IconTruckDelivery size={16} />}>Alış girişi</Button>
              <Button component={Link} to="/izlenebilirlik" size="sm" variant="default" leftSection={<IconRoute size={16} />}>Lot izle</Button>
            </Group>
          </Stack>
        </Grid.Col>
        <Grid.Col span={{ base: 12, sm: 4, lg: 3 }}>
          <Kpi
            to="/satis-sevkiyat"
            label="Bugünkü Satış Tutarı"
            value={sales.isLoading ? '…' : sales.data ? eur(todaysTotal) : '—'}
            sub={sales.data ? `${todaysOrders} sipariş · son 7 gün` : '—'}
            icon={IconCash}
            color="brand"
          >
            {sales.data && <Sparkline values={totals.slice(-7)} />}
          </Kpi>
        </Grid.Col>
        <Grid.Col span={{ base: 12, sm: 4, lg: 3 }}>
          <Kpi
            to={recalledCount ? '/lotlar?status=recalled' : '/lotlar?status=blocked'}
            label="Bloke Lot"
            value={dash(blockedLoading, blockedCount)}
            valueColor={blockedCount ? 'orange' : undefined}
            sub={recalledCount ? `${recalledCount} geri çağrılan · satışa kapalı` : 'Satışa ve sevke kapalı'}
            icon={IconLock}
            color="orange"
          />
        </Grid.Col>
        <Grid.Col span={{ base: 12, sm: 4, lg: 3 }}>
          <Kpi
            to="/geri-cagirma"
            label="Aktif Geri Çağırma"
            value={dash(recalls, openRecallCount)}
            valueColor={openRecallCount ? 'red' : undefined}
            sub="Açık geri çağırma kaydı"
            icon={IconArrowBackUp}
            color="red"
          />
        </Grid.Col>
      </Grid>
      {(sales.error || recalls.error || blockedLots.error || recalledLots.error) && (
        <Alert color="red">{err(sales.error ?? recalls.error ?? blockedLots.error ?? recalledLots.error)}</Alert>
      )}

      <Grid gutter="lg" align="stretch">
        <Grid.Col span={{ base: 12, lg: 8 }}>
          <Card withBorder h="100%">
            <CardTitle
              title="Satış Tutarı"
              sub={`Son ${SALES_DAYS} gün, günlük${truncated ? ` · son ${SALES_LIMIT} siparişe göre` : ''}`}
              to="/satis-sevkiyat"
            />
            {sales.isLoading && <Loader size="sm" />}
            {sales.data && (totals.some((t) => t > 0)
              ? <SalesChart days={days} totals={totals} />
              : <Text size="sm" c="dimmed">Son {SALES_DAYS} günde satış yok.</Text>)}
          </Card>
        </Grid.Col>
        <Grid.Col span={{ base: 12, lg: 4 }}>
          <Card withBorder h="100%">
            <CardTitle title="Açık Anomali Uyarıları" sub="Önem derecesine göre" to="/uyarilar" />
            {alertSummary.error && <Alert color="red">{err(alertSummary.error)}</Alert>}
            {alertSummary.isLoading && <Loader size="sm" />}
            {alertSummary.data && (
              <>
                <Text fz={40} fw={700} lh={1}>{alertSummary.data.total}</Text>
                <Text size="xs" c="dimmed" mb="lg">açık uyarı</Text>
                <Stack gap="md">
                  {SEVERITIES.map((s) => (
                    <div key={s}>
                      <Group justify="space-between" mb={6}>
                        <SeverityBadge severity={s} />
                        <Text size="sm" fw={600}>{alertSummary.data[s]}</Text>
                      </Group>
                      <Progress
                        value={alertSummary.data.total ? (alertSummary.data[s] / alertSummary.data.total) * 100 : 0}
                        color={SEVERITY_COLOR[s]}
                        size="sm"
                        radius="xl"
                      />
                    </div>
                  ))}
                </Stack>
              </>
            )}
          </Card>
        </Grid.Col>
      </Grid>

      <SimpleGrid cols={{ base: 1, md: 2 }} spacing="lg">
        <Card withBorder>
          <CardTitle title="Son Üretim Emirleri" to="/dis-uretim" />
          {lastProduction.error && <Alert color="red">{err(lastProduction.error)}</Alert>}
          {lastProduction.isLoading && <Loader size="sm" />}
          {lastProduction.data && (
            <Table highlightOnHover>
              <Table.Tbody>
                {lastProduction.data.items.map((o) => (
                  <Table.Tr key={o.id}>
                    <Table.Td><Link to={`/dis-uretim/${o.id}`}>{o.order_no}</Link></Table.Td>
                    <Table.Td>{o.product_item_name}</Table.Td>
                    <Table.Td>
                      <Badge variant="light" color={PRODUCTION_STATUS[o.status].color}>{PRODUCTION_STATUS[o.status].label}</Badge>
                    </Table.Td>
                  </Table.Tr>
                ))}
                {lastProduction.data.items.length === 0 && (
                  <Table.Tr><Table.Td c="dimmed">Henüz üretim emri yok.</Table.Td></Table.Tr>
                )}
              </Table.Tbody>
            </Table>
          )}
        </Card>

        <Card withBorder>
          <CardTitle title="Son Uyarılar" to="/uyarilar" />
          {lastAlerts.error && <Alert color="red">{err(lastAlerts.error)}</Alert>}
          {lastAlerts.isLoading && <Loader size="sm" />}
          {lastAlerts.data && (
            <Table highlightOnHover>
              <Table.Tbody>
                {lastAlerts.data.items.map((a) => (
                  <Table.Tr key={a.id}>
                    <Table.Td><SeverityBadge severity={a.severity} /></Table.Td>
                    <Table.Td>{a.alert_type}</Table.Td>
                    <Table.Td><Text size="sm" lineClamp={1}>{a.message}</Text></Table.Td>
                  </Table.Tr>
                ))}
                {lastAlerts.data.items.length === 0 && (
                  <Table.Tr><Table.Td c="dimmed">Henüz uyarı yok.</Table.Td></Table.Tr>
                )}
              </Table.Tbody>
            </Table>
          )}
        </Card>
      </SimpleGrid>
    </Stack>
  )
}
