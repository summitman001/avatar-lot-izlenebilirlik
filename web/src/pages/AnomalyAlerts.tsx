// "Uyarılar" sayfası: anomaly_alert listesi (bkz. docs/anomaly-integration.md). SADECE
// BİLGİLENDİRMEDİR: burada hesap yapılmaz, API'den gelen severity/mesaj/kanıt doğrudan gösterilir.
import { Alert, Badge, Button, Group, Loader, Select, Stack, Table, Text, Textarea, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { SeverityBadge } from '../components/AnomalyAlerts'
import { ApiError, apiGet, apiPatch, type AnomalyAlert, type List } from '../lib/api'

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
    <Stack>
      <Group justify="space-between">
        <Title order={2}>Uyarılar</Title>
        <Group>
          <Select
            placeholder="Önem"
            clearable
            value={severity}
            onChange={setSeverity}
            data={[{ value: 'critical', label: 'Kritik' }, { value: 'warning', label: 'Uyarı' }, { value: 'info', label: 'Bilgi' }]}
          />
          <Select
            placeholder="Kaynak"
            clearable
            value={refType}
            onChange={setRefType}
            data={Object.entries(REF_TYPE_LABEL).map(([value, label]) => ({ value, label }))}
          />
          <Select
            placeholder="Durum"
            clearable
            value={acknowledged}
            onChange={setAcknowledged}
            data={[{ value: 'false', label: 'Açık' }, { value: 'true', label: 'Okundu' }]}
          />
        </Group>
      </Group>

      {error && <Alert color="red">{err(error)}</Alert>}
      {actionError && <Alert color="red" withCloseButton onClose={() => setActionError(null)}>{actionError}</Alert>}
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
                  <Table.Td style={{ whiteSpace: 'nowrap' }}><Badge variant="outline" color="gray">{a.alert_type}</Badge></Table.Td>
                  <Table.Td style={{ whiteSpace: 'nowrap' }}>
                    {REF_TYPE_LABEL[a.ref_type] ?? a.ref_type}
                    {link && a.ref_no ? <> — <Link to={link}>{a.ref_no}</Link></> : a.ref_no ? <> — {a.ref_no}</> : null}
                  </Table.Td>
                  <Table.Td maw={300}><Text size="sm">{a.message}</Text>{a.note && <Text size="xs" c="dimmed">Not: {a.note}</Text>}</Table.Td>
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
        <Stack gap="xs" p="md" style={{ border: '1px solid var(--mantine-color-gray-4)', borderRadius: 8 }}>
          <Text fw={500}>Okundu işaretle: {noteTarget.alert_type}</Text>
          <Textarea placeholder="Not (opsiyonel)" value={note} onChange={(e) => setNote(e.currentTarget.value)} autosize minRows={1} />
          <Group justify="flex-end" gap="xs">
            <Button variant="default" size="xs" onClick={() => setNoteTarget(null)}>Vazgeç</Button>
            <Button size="xs" loading={ack.isPending} onClick={() => ack.mutate({ id: noteTarget.id, note: note || undefined })}>
              Onayla
            </Button>
          </Group>
        </Stack>
      )}
    </Stack>
  )
}
