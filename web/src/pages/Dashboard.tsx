// Basit özet panosu. Şimdilik tek içerik: açık anomali uyarıları özeti (bkz.
// docs/anomaly-integration.md). Hesap yapılmaz, API'den gelen sayılar doğrudan gösterilir.
import { Alert, Card, Loader, SimpleGrid, Stack, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { SEVERITY_COLOR, SEVERITY_LABEL } from '../components/AnomalyAlerts'
import { ApiError, apiGet, type AnomalyAlertSummary, type AnomalySeverity } from '../lib/api'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

const SEVERITIES: AnomalySeverity[] = ['critical', 'warning', 'info']

export default function Dashboard() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['anomaly-alert-summary'],
    queryFn: () => apiGet<AnomalyAlertSummary>('/anomaly-alerts/summary'),
    refetchInterval: 15_000,
  })

  return (
    <Stack>
      <Title order={2}>Dashboard</Title>
      {error && <Alert color="red">{err(error)}</Alert>}
      {isLoading && <Loader />}
      {data && (
        <Card withBorder component={Link} to="/uyarilar" style={{ textDecoration: 'none' }}>
          <Text fw={500} mb="sm">Açık Anomali Uyarıları</Text>
          <SimpleGrid cols={{ base: 1, sm: 3 }}>
            {SEVERITIES.map((s) => (
              <div key={s}>
                <Text size="xs" c="dimmed">{SEVERITY_LABEL[s]}</Text>
                <Text fw={700} size="xl" c={SEVERITY_COLOR[s]}>{data[s]}</Text>
              </div>
            ))}
          </SimpleGrid>
          <Text size="xs" c="dimmed" mt="sm">Toplam açık uyarı: {data.total} — detaylar için tıklayın</Text>
        </Card>
      )}
    </Stack>
  )
}
