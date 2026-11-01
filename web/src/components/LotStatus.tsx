// Lot durum rozeti ve bloke/geri çağırma sebebi. Geri çağrılan ve bloke lotlar kırmızı (dolu) rozetle
// öne çıkar; sebep üzerine gelmeden okunur. Geri çağırma sebebi sunucuda "RCL-2026-001: <sebep>"
// biçiminde yazılır (repo/traceability.go), baştaki numara ilgili geri çağırma kaydına link olur.
import { Anchor, Badge, Group, Text } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { apiGet, type List, type Lot, type Recall } from '../lib/api'

export const LOT_STATUS: Record<Lot['status'], { label: string; color: string }> = {
  active: { label: 'Aktif', color: 'green' },
  blocked: { label: 'Bloke', color: 'orange' },
  recalled: { label: 'Geri çağrıldı', color: 'red' },
  consumed: { label: 'Tükendi', color: 'gray' },
}

const isHeld = (status: string) => status === 'recalled' || status === 'blocked'

export function LotStatusBadge({ status, size }: { status: Lot['status']; size?: string }) {
  const s = LOT_STATUS[status] ?? { label: status, color: 'gray' }
  return isHeld(status)
    ? <Badge color="red" variant="filled" size={size}>{s.label}</Badge>
    : <Badge color={s.color} variant="light" size={size}>{s.label}</Badge>
}

// "RCL-2026-001: sebep" → { recallNo, text }. Elle bloklamada numara yoktur.
export function splitReason(reason: string): { recallNo: string | null; text: string } {
  const m = /^(RCL-[\w-]+):\s*(.*)$/s.exec(reason)
  return m ? { recallNo: m[1], text: m[2] } : { recallNo: null, text: reason }
}

// Geri çağırma numarasından kayıt id'si (link için); liste React Query önbelleğinde paylaşılır.
function useRecallId(recallNo: string | null): string | undefined {
  const { data } = useQuery({
    queryKey: ['recalls', 'index'],
    queryFn: () => apiGet<List<Recall>>('/recalls', { limit: '1000' }),
    enabled: !!recallNo,
    staleTime: 30_000,
  })
  return data?.items.find((r) => r.recall_no === recallNo)?.id
}

const NO_WRAP = { whiteSpace: 'nowrap', flexShrink: 0 } as const

export function RecallLink({ recallNo }: { recallNo: string }) {
  const id = useRecallId(recallNo)
  return id
    ? <Anchor component={Link} to={`/geri-cagirma/${id}`} size="sm" fw={600} c="red.7" style={NO_WRAP} onClick={(e) => e.stopPropagation()}>{recallNo}</Anchor>
    : <Text span size="sm" fw={600} c="red.7" style={NO_WRAP}>{recallNo}</Text>
}

// Satır içi sebep: kısaltılmış tek satır (tam metin title'da ve lot detayında).
export function LotReason({ reason, maw = 260 }: { reason: string; maw?: number }) {
  const { recallNo, text } = splitReason(reason)
  return (
    <Group gap={6} wrap="nowrap" mt={4} maw={maw}>
      {recallNo && <RecallLink recallNo={recallNo} />}
      <Text size="sm" c="red.8" truncate="end" title={reason} style={{ minWidth: 0 }}>{text}</Text>
    </Group>
  )
}

export { isHeld as isLotHeld }
