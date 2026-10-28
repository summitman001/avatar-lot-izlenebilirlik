// Özet pano: mevcut uç noktalardan beslenen kartlar (hiçbiri yeni hesap içermez, API'den geleni
// doğrudan gösterir/toplar). Açık anomali özeti docs/anomaly-integration.md; diğerleri ilgili sayfanın
// kendi uç noktasıdır (recalls/sales-orders/lots/production-orders/anomaly-alerts).
import { Alert, Badge, Card, Loader, SimpleGrid, Stack, Table, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { SEVERITY_COLOR, SEVERITY_LABEL, SeverityBadge } from '../components/AnomalyAlerts'
import {
  ApiError, apiGet, type AnomalyAlert, type AnomalyAlertSummary, type AnomalySeverity, type List,
  type Lot, type ProductionOrder, type Recall, type SalesOrder,
} from '../lib/api'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

const SEVERITIES: AnomalySeverity[] = ['critical', 'warning', 'info']

function todayStr(): string {
  return new Date().toISOString().slice(0, 10)
}

function Tile({ label, value, color }: { label: string; value: React.ReactNode; color?: string }) {
  return (
    <div>
      <Text size="xs" c="dimmed">{label}</Text>
      <Text fw={700} size="xl" c={color}>{value}</Text>
    </div>
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
  const todaysSales = useQuery({
    queryKey: ['sales-orders', 'dashboard', 'today'],
    queryFn: () => apiGet<List<SalesOrder>>('/sales-orders', { from: todayStr(), to: todayStr(), limit: '1000' }),
  })
  const blockedLots = useQuery({
    queryKey: ['lots', 'dashboard', 'blocked'],
    queryFn: () => apiGet<List<Lot>>('/lots', { status: 'blocked', limit: '1000' }),
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
  const todaysSalesTotal = todaysSales.data?.items.reduce((sum, o) => sum + Number(o.total_amount), 0)

  return (
    <Stack>
      <Title order={2}>Dashboard</Title>

      <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }}>
        <Card withBorder component={Link} to="/geri-cagirma" style={{ textDecoration: 'none' }}>
          <Tile label="Aktif Geri Çağırma" value={recalls.isLoading ? '…' : openRecallCount ?? '—'} color={openRecallCount ? 'red' : undefined} />
        </Card>
        <Card withBorder component={Link} to="/satis-sevkiyat" style={{ textDecoration: 'none' }}>
          <Tile
            label="Bugünkü Satış Tutarı"
            value={todaysSales.isLoading ? '…' : todaysSalesTotal != null ? `${todaysSalesTotal.toFixed(2)} €` : '—'}
          />
        </Card>
        <Card withBorder component={Link} to="/lotlar?status=blocked" style={{ textDecoration: 'none' }}>
          <Tile
            label="Bloke Lot"
            value={blockedLots.isLoading ? '…' : blockedLots.data?.items.length ?? '—'}
            color={blockedLots.data && blockedLots.data.items.length > 0 ? 'orange' : undefined}
          />
        </Card>
        <Card withBorder component={Link} to="/uyarilar" style={{ textDecoration: 'none' }}>
          {alertSummary.error && <Text size="xs" c="red">{err(alertSummary.error)}</Text>}
          {alertSummary.isLoading && <Loader size="xs" />}
          {alertSummary.data && (
            <>
              <Text fw={500} mb="xs">Açık Anomali Uyarıları</Text>
              <SimpleGrid cols={3}>
                {SEVERITIES.map((s) => (
                  <div key={s}>
                    <Text size="xs" c="dimmed">{SEVERITY_LABEL[s]}</Text>
                    <Text fw={700} c={SEVERITY_COLOR[s]}>{alertSummary.data[s]}</Text>
                  </div>
                ))}
              </SimpleGrid>
            </>
          )}
        </Card>
      </SimpleGrid>

      <SimpleGrid cols={{ base: 1, md: 2 }}>
        <Card withBorder>
          <Text fw={500} mb="sm">Son 5 Üretim Emri</Text>
          {lastProduction.error && <Alert color="red">{err(lastProduction.error)}</Alert>}
          {lastProduction.isLoading && <Loader size="sm" />}
          {lastProduction.data && (
            <Table>
              <Table.Tbody>
                {lastProduction.data.items.map((o) => (
                  <Table.Tr key={o.id}>
                    <Table.Td><Link to={`/dis-uretim/${o.id}`}>{o.order_no}</Link></Table.Td>
                    <Table.Td>{o.product_item_name}</Table.Td>
                    <Table.Td><Badge variant="light">{o.status}</Badge></Table.Td>
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
          <Text fw={500} mb="sm">Son 5 Uyarı</Text>
          {lastAlerts.error && <Alert color="red">{err(lastAlerts.error)}</Alert>}
          {lastAlerts.isLoading && <Loader size="sm" />}
          {lastAlerts.data && (
            <Table>
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
