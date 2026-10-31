// Anomali uyarıları için paylaşılan görsel parçalar (bkz. docs/anomaly-integration.md).
// SADECE BİLGİLENDİRMEDİR: hiçbir uyarı bir lotu bloke etmez veya bir işlemi reddetmez; bu bileşenler
// de hesap yapmaz, yalnızca API'den geleni gösterir.
import { Badge, Button, Card, Group, Stack, Text, Textarea } from '@mantine/core'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { ApiError, apiPatch, type AnomalyAlert, type AnomalySeverity } from '../lib/api'

export const SEVERITY_COLOR: Record<AnomalySeverity, string> = { critical: 'red', warning: 'orange', info: 'gray' }
export const SEVERITY_LABEL: Record<AnomalySeverity, string> = { critical: 'Kritik', warning: 'Uyarı', info: 'Bilgi' }

export function SeverityBadge({ severity }: { severity: AnomalySeverity }) {
  return <Badge color={SEVERITY_COLOR[severity]} variant="light">{SEVERITY_LABEL[severity]}</Badge>
}

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

// AnomalyAlertsCard: bir siparişin (production/cutting) detay sayfasına gömülen uyarı kartı.
export function AnomalyAlertsCard({ alerts, queryKeysToInvalidate }: { alerts: AnomalyAlert[]; queryKeysToInvalidate: unknown[][] }) {
  if (alerts.length === 0) return null
  return (
    <Card withBorder>
      <Text fw={600} mb="sm">Anomali Uyarıları</Text>
      <Stack gap="xs">
        {alerts.map((a) => <AlertRow key={a.id} alert={a} queryKeysToInvalidate={queryKeysToInvalidate} />)}
      </Stack>
      <Text size="xs" c="dimmed" mt="sm">
        Bu uyarılar sadece bilgilendirmedir; hiçbir lotu bloke etmez veya işlemi reddetmez.
      </Text>
    </Card>
  )
}

export function AlertRow({ alert: a, queryKeysToInvalidate }: { alert: AnomalyAlert; queryKeysToInvalidate: unknown[][] }) {
  const qc = useQueryClient()
  const [note, setNote] = useState(a.note ?? '')
  const [editing, setEditing] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  const ack = useMutation({
    mutationFn: (payload: { acknowledged?: boolean; note?: string }) => apiPatch<AnomalyAlert>(`/anomaly-alerts/${a.id}`, payload),
    onSuccess: () => {
      setEditing(false)
      setFormError(null)
      for (const key of queryKeysToInvalidate) qc.invalidateQueries({ queryKey: key })
    },
    onError: (e) => setFormError(err(e)),
  })

  return (
    <Card withBorder padding="sm" bg={a.acknowledged ? undefined : 'var(--mantine-color-red-light)'}>
      <Group justify="space-between" wrap="nowrap" align="flex-start">
        <Stack gap={4} style={{ flex: 1 }}>
          <Group gap="xs">
            <SeverityBadge severity={a.severity} />
            <Badge variant="outline" color="gray">{a.alert_type}</Badge>
            {a.acknowledged && <Badge variant="outline" color="green">Okundu</Badge>}
          </Group>
          <Text size="sm">{a.message}</Text>
          {a.note && <Text size="xs" c="dimmed">Not: {a.note}</Text>}
          <Text size="xs" c="dimmed">{new Date(a.created_at).toLocaleString('tr-TR')}</Text>
        </Stack>
        {!a.acknowledged && !editing && (
          <Button size="xs" variant="light" onClick={() => setEditing(true)}>Okundu İşaretle</Button>
        )}
      </Group>
      {editing && (
        <Stack gap="xs" mt="sm">
          {formError && <Text size="xs" c="red">{formError}</Text>}
          <Textarea placeholder="Not (opsiyonel)" value={note} onChange={(e) => setNote(e.currentTarget.value)} autosize minRows={1} />
          <Group justify="flex-end" gap="xs">
            <Button size="xs" variant="default" onClick={() => setEditing(false)}>Vazgeç</Button>
            <Button size="xs" loading={ack.isPending} onClick={() => ack.mutate({ acknowledged: true, note: note || undefined })}>
              Onayla
            </Button>
          </Group>
        </Stack>
      )}
    </Card>
  )
}
