// "Uyarılar" sayfası: anomaly_alert listesi (bkz. docs/anomaly-integration.md). SADECE
// BİLGİLENDİRMEDİR: burada hesap yapılmaz, API'den gelen severity/mesaj/kanıt doğrudan gösterilir.
import { Alert, Badge, Button, Group, Loader, Select, Stack, Table, Text, Textarea } from '@mantine/core'
import { IconAlertOctagon, IconAlertTriangle, IconBell, IconInfoCircle } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { SEVERITY_COLOR, SEVERITY_LABEL, SeverityBadge } from '../components/AnomalyAlerts'
import { FilterChips, PageHeader, StatCard, StatGrid } from '../components/Page'
import { ApiError, apiGet, apiPatch, type AnomalyAlert, type AnomalyAlertSummary, type AnomalySeverity, type List } from '../lib/api'
import { alertTypeLabel, humanizeMessage } from '../lib/messages'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

const REF_TYPE_LABEL: Record<string, string> = {
  production_order: 'Dış Üretim', cutting_order: 'Parçalama', purchase_receipt: 'Alış', sales_line: 'Satış',
}

const REF_LINK: Record<string, (refID: string) => string> = {
  production_order: (id) => `/dis-uretim/${id}`,
  cutting_order: (id) => `/parcalama/${id}`,
}

export default function AnomalyAlerts() {
  const qc = useQueryClient()
  const [severity, setSeverity] = useState<string | null>(null)
  const [refType, setRefType] = useState<string | null>(null)
  const [acknowledged, setAcknowledged] = useState<string | null>('false')
  const [noteTarget, setNoteTarget] = useState<AnomalyAlert | null>(null)
  const [note, setNote] = useState('')
  const [actionError, setActionError] = useState<string | null>(null)

  const { data, isLoading, error } = useQuery({
    queryKey: ['anomaly-alerts', severity, refType, acknowledged],
    queryFn: () =>
      apiGet<List<AnomalyAlert>>('/anomaly-alerts', {
        severity: severity ?? undefined, ref_type: refType ?? undefined, acknowledged: acknowledged ?? undefined,
      }),
  })

  // Kartlar açık uyarıların özetidir (üst bardaki zil ile aynı sorgu), filtreden bağımsız.
  const summary = useQuery({
    queryKey: ['anomaly-alert-summary'],
    queryFn: () => apiGet<AnomalyAlertSummary>('/anomaly-alerts/summary'),
    refetchInterval: 15_000,
  })
  const sum = (k: keyof AnomalyAlertSummary) => (summary.data ? summary.data[k] : '…')

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['anomaly-alerts'] })
    qc.invalidateQueries({ queryKey: ['anomaly-alert-summary'] })
  }

  const ack = useMutation({
    mutationFn: (payload: { id: string; note?: string }) => apiPatch<AnomalyAlert>(`/anomaly-alerts/${payload.id}`, { acknowledged: true, note: payload.note }),
    onSuccess: () => { invalidate(); setNoteTarget(null); setNote('') },
    onError: (e) => setActionError(err(e)),
  })

  return (
    <>
      <PageHeader eyebrow="Üretim ve parçalamadaki sapmalar; yalnızca bilgilendirir, işlemi durdurmaz" title="Uyarılar" />
      <StatGrid>
        <StatCard label="Açık uyarı" value={sum('total')} icon={IconBell} />
        <StatCard label="Kritik" value={sum('critical')} icon={IconAlertOctagon} color={SEVERITY_COLOR.critical} />
        <StatCard label="Uyarı" value={sum('warning')} icon={IconAlertTriangle} color={SEVERITY_COLOR.warning} />
        <StatCard label="Bilgi" value={sum('info')} icon={IconInfoCircle} color={SEVERITY_COLOR.info} />
      </StatGrid>
      <Group justify="space-between" mb="md">
        <FilterChips value={severity} onChange={setSeverity}
          options={(['critical', 'warning', 'info'] as AnomalySeverity[]).map((s) => ({ value: s, label: SEVERITY_LABEL[s], color: SEVERITY_COLOR[s] }))} />
        <Group gap="xs">
          <Select radius="xl" w={170} placeholder="Kaynak" clearable value={refType} onChange={setRefType}
            data={Object.entries(REF_TYPE_LABEL).map(([value, label]) => ({ value, label }))} />
          <Select radius="xl" w={140} placeholder="Durum" clearable value={acknowledged} onChange={setAcknowledged}
            data={[{ value: 'false', label: 'Açık' }, { value: 'true', label: 'Okundu' }]} />
        </Group>
      </Group>

      {error && <Alert color="red" mb="md">{err(error)}</Alert>}
      {actionError && <Alert color="red" mb="md" withCloseButton onClose={() => setActionError(null)}>{actionError}</Alert>}
      {isLoading && <Loader />}

      {data && (
        <Table striped highlightOnHover withTableBorder verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Önem</Table.Th><Table.Th>Tür</Table.Th><Table.Th>Kaynak</Table.Th>
              <Table.Th>Mesaj</Table.Th><Table.Th>Tarih</Table.Th><Table.Th>Durum</Table.Th><Table.Th></Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {data.items.map((a) => {
              const link = REF_LINK[a.ref_type]?.(a.ref_id)
              return (
                <Table.Tr key={a.id}>
                  <Table.Td style={{ whiteSpace: 'nowrap' }}><SeverityBadge severity={a.severity} /></Table.Td>
                  <Table.Td style={{ whiteSpace: 'nowrap' }}><Badge variant="outline" color="gray" tt="none">{alertTypeLabel(a.alert_type)}</Badge></Table.Td>
                  <Table.Td style={{ whiteSpace: 'nowrap' }}>
                    {REF_TYPE_LABEL[a.ref_type] ?? a.ref_type}
                    {link && a.ref_no ? <> — <Link to={link}>{a.ref_no}</Link></> : a.ref_no ? <> — {a.ref_no}</> : null}
                  </Table.Td>
                  <Table.Td maw={300}><Text size="sm">{humanizeMessage(a.message)}</Text>{a.note && <Text size="xs" c="dimmed">Not: {a.note}</Text>}</Table.Td>
                  <Table.Td style={{ whiteSpace: 'nowrap' }}><Text size="xs">{new Date(a.created_at).toLocaleString('tr-TR')}</Text></Table.Td>
                  <Table.Td style={{ whiteSpace: 'nowrap' }}>
                    {a.acknowledged ? <Badge color="green" variant="light">Okundu</Badge> : <Badge color="red" variant="light">Açık</Badge>}
                  </Table.Td>
                  <Table.Td style={{ whiteSpace: 'nowrap' }}>
                    {!a.acknowledged && (
                      <Button size="xs" variant="light" onClick={() => { setNoteTarget(a); setNote('') }}>Okundu İşaretle</Button>
                    )}
                  </Table.Td>
                </Table.Tr>
              )
            })}
            {data.items.length === 0 && (
              <Table.Tr><Table.Td colSpan={7}><Text c="dimmed" size="sm">Uyarı yok</Text></Table.Td></Table.Tr>
            )}
          </Table.Tbody>
        </Table>
      )}

      {noteTarget && (
        <Stack gap="xs" p="md" mt="md" style={{ border: '1px solid var(--app-border)', borderRadius: 'var(--mantine-radius-lg)' }}>
          <Text fw={500}>Okundu işaretle: {alertTypeLabel(noteTarget.alert_type)}</Text>
          <Textarea placeholder="Not (opsiyonel)" value={note} onChange={(e) => setNote(e.currentTarget.value)} autosize minRows={1} />
          <Group justify="flex-end" gap="xs">
            <Button variant="default" size="xs" onClick={() => setNoteTarget(null)}>Vazgeç</Button>
            <Button size="xs" loading={ack.isPending} onClick={() => ack.mutate({ id: noteTarget.id, note: note || undefined })}>
              Onayla
            </Button>
          </Group>
        </Stack>
      )}
    </>
  )
}
