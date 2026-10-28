import { Alert, Anchor, Badge, Card, Group, Loader, SimpleGrid, Select, Stack, Table, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { Fragment, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  ApiError, apiGet, type List, type PredictionListItem, type PredictionSummary, type PredictionTargetType,
} from '../lib/api'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

const TARGET_LABEL: Record<PredictionTargetType, string> = { production: 'Dış Üretim', cutting: 'Parçalama' }
const TARGET_LINK: Record<PredictionTargetType, string> = { production: '/dis-uretim', cutting: '/parcalama' }
const STAGE_LABEL: Record<string, string> = { draft: 'Taslak', materials_sent: 'Hammadde Sevk Edildi' }
const PART_LABEL: Record<string, string> = {
  but: 'But', kol: 'Kol', sirt: 'Sırt', kaburga: 'Kaburga', gerdan: 'Gerdan', kiyma: 'Kıyma', fire: 'Fire',
}

const ML_REPORT_URL = 'https://github.com/summitman001/avatar-lot-izlenebilirlik/blob/main/docs/ml-report.md'

function SummaryCard({ title, link, entry }: { title: string; link: string; entry: PredictionSummary['production'] | undefined }) {
  return (
    <Card withBorder component={Link} to={link} style={{ textDecoration: 'none' }}>
      <Text fw={500} mb="sm">{title}</Text>
      <SimpleGrid cols={4}>
        <div>
          <Text size="xs" c="dimmed">Kayıt</Text>
          <Text fw={700} size="xl">{entry?.count ?? '—'}</Text>
        </div>
        <div>
          <Text size="xs" c="dimmed">Gerçeği Girilmiş</Text>
          <Text fw={700} size="xl">{entry?.count_with_actual ?? '—'}</Text>
        </div>
        <div>
          <Text size="xs" c="dimmed">MAE</Text>
          <Text fw={700} size="xl">{entry?.mae ?? '—'}</Text>
        </div>
        <div>
          <Text size="xs" c="dimmed">MAPE</Text>
          <Text fw={700} size="xl">{entry?.mape != null ? `${entry.mape}%` : '—'}</Text>
        </div>
      </SimpleGrid>
    </Card>
  )
}

function errorBadgeColor(pct: string | null): string | undefined {
  if (pct == null) return undefined
  const n = Number(pct)
  if (n >= 15) return 'red'
  if (n >= 5) return 'orange'
  return 'green'
}

function PartsBreakdown({ parts }: { parts: PredictionListItem['parts'] }) {
  if (!parts || parts.length === 0) return <Text size="sm" c="dimmed">Fiili ağırlık henüz girilmemiş.</Text>
  return (
    <Table withColumnBorders>
      <Table.Thead>
        <Table.Tr>
          <Table.Th>Parça</Table.Th>
          <Table.Th ta="right">Beklenen (kg)</Table.Th>
          <Table.Th ta="right">Gerçek (kg)</Table.Th>
          <Table.Th ta="right">Fark (kg)</Table.Th>
          <Table.Th ta="right">Fark (%)</Table.Th>
        </Table.Tr>
      </Table.Thead>
      <Table.Tbody>
        {parts.map((p) => (
          <Table.Tr key={p.part_key}>
            <Table.Td>{PART_LABEL[p.part_key] ?? p.part_key}</Table.Td>
            <Table.Td ta="right">{p.predicted_kg ?? '—'}</Table.Td>
            <Table.Td ta="right">{p.actual_kg ?? '—'}</Table.Td>
            <Table.Td ta="right">{p.diff_kg ?? '—'}</Table.Td>
            <Table.Td ta="right">
              {p.diff_pct != null ? (
                <Badge color={errorBadgeColor(p.diff_pct)} variant="light">{p.diff_pct}%</Badge>
              ) : '—'}
            </Table.Td>
          </Table.Tr>
        ))}
      </Table.Tbody>
    </Table>
  )
}

export default function Predictions() {
  const [targetType, setTargetType] = useState<string | null>(null)
  const [hasActual, setHasActual] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<string | null>(null)

  const summary = useQuery({
    queryKey: ['predictions-summary'],
    queryFn: () => apiGet<PredictionSummary>('/predictions/summary'),
    refetchInterval: 15_000,
  })
  const list = useQuery({
    queryKey: ['predictions', targetType, hasActual],
    queryFn: () =>
      apiGet<List<PredictionListItem>>('/predictions', {
        target_type: targetType ?? undefined,
        has_actual: hasActual ?? undefined,
        limit: '200',
      }),
  })

  return (
    <Stack>
      <Title order={2}>Tahminler</Title>
      <Text size="sm" c="dimmed">
        Modeller sentetik veri ile eğitilmiştir; ayrıntılı değerlendirme için{' '}
        <Anchor href={ML_REPORT_URL} target="_blank" rel="noreferrer">docs/ml-report.md</Anchor>.
      </Text>

      {summary.error && <Alert color="red">{err(summary.error)}</Alert>}
      {summary.isLoading && <Loader />}
      {summary.data && (
        <SimpleGrid cols={{ base: 1, md: 2 }}>
          <SummaryCard title="Dış Üretim — Teslim Edilen Adet" link="/dis-uretim" entry={summary.data.production} />
          <SummaryCard title="Parçalama — Parça Ağırlığı (kg)" link="/parcalama" entry={summary.data.cutting} />
        </SimpleGrid>
      )}

      <Group>
        <Select
          placeholder="Tür"
          clearable
          data={[{ value: 'production', label: 'Dış Üretim' }, { value: 'cutting', label: 'Parçalama' }]}
          value={targetType}
          onChange={setTargetType}
        />
        <Select
          placeholder="Gerçekleşme"
          clearable
          data={[{ value: 'true', label: 'Sadece gerçeği girilmiş' }, { value: 'false', label: 'Sadece bekleyen' }]}
          value={hasActual}
          onChange={setHasActual}
        />
      </Group>

      {list.error && <Alert color="red">{err(list.error)}</Alert>}
      {list.isLoading && <Loader />}
      {list.data && (
        <Table striped highlightOnHover withTableBorder>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Tarih</Table.Th>
              <Table.Th>Tür</Table.Th>
              <Table.Th>Emir No</Table.Th>
              <Table.Th>Model</Table.Th>
              <Table.Th ta="right">Tahmin</Table.Th>
              <Table.Th ta="right">Gerçek</Table.Th>
              <Table.Th ta="right">Sapma</Table.Th>
              <Table.Th></Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {list.data.items.map((it) => {
              const isOpen = expanded === it.id
              const canExpand = it.target_type === 'cutting' && it.has_actual
              return (
                <Fragment key={it.id}>
                  <Table.Tr
                    style={{ cursor: canExpand ? 'pointer' : undefined }}
                    onClick={() => canExpand && setExpanded(isOpen ? null : it.id)}
                  >
                    <Table.Td>{new Date(it.created_at).toLocaleString('tr-TR')}</Table.Td>
                    <Table.Td>
                      {TARGET_LABEL[it.target_type]}
                      {it.stage && <Text span size="xs" c="dimmed" ml={4}>({STAGE_LABEL[it.stage] ?? it.stage})</Text>}
                    </Table.Td>
                    <Table.Td>
                      {it.ref_no ? <Link to={`${TARGET_LINK[it.target_type]}/${it.ref_id}`}>{it.ref_no}</Link> : '—'}
                    </Table.Td>
                    <Table.Td>
                      {it.model_name}
                      {it.baseline && <Badge ml="xs" size="xs" variant="outline" color="gray">baseline</Badge>}
                    </Table.Td>
                    <Table.Td ta="right">{it.predicted_value ?? '—'} {it.metric_unit}</Table.Td>
                    <Table.Td ta="right">{it.actual_value ?? '—'} {it.has_actual ? it.metric_unit : ''}</Table.Td>
                    <Table.Td ta="right">
                      {it.error_pct != null ? (
                        <Badge color={errorBadgeColor(it.error_pct)} variant="light">
                          {it.error_abs} {it.metric_unit} ({it.error_pct}%)
                        </Badge>
                      ) : (
                        <Text size="xs" c="dimmed">bekliyor</Text>
                      )}
                    </Table.Td>
                    <Table.Td>{canExpand && (isOpen ? '▲' : '▼')}</Table.Td>
                  </Table.Tr>
                  {isOpen && canExpand && (
                    <Table.Tr>
                      <Table.Td colSpan={8} style={{ background: 'var(--mantine-color-gray-0)' }}>
                        <PartsBreakdown parts={it.parts} />
                      </Table.Td>
                    </Table.Tr>
                  )}
                </Fragment>
              )
            })}
          </Table.Tbody>
        </Table>
      )}
    </Stack>
  )
}
