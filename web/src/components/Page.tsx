// Sayfaların ortak tasarım dili: başlık (üst satır + büyük başlık + düğmeler), özet kartları ve
// filtre çipleri. Görünüm/animasyon app.css'te (.filter-chip, .stagger, .page-anim).
import { Badge, Card, Group, SimpleGrid, Text, ThemeIcon, Title, UnstyledButton, type MantineSpacing } from '@mantine/core'
import type { Icon as TablerIcon } from '@tabler/icons-react'
import type { ReactNode } from 'react'

// Özet kartları filtreden bağımsız, bu limitle çekilen listeden hesaplanır (backend MaxLimit).
export const SUMMARY_LIMIT = 1000

// mb: Stack içindeki sayfalarda Stack'in kendi aralığı eklendiği için küçültülür ("xs").
export function PageHeader({ eyebrow, title, badges, actions, mb = 'lg' }: {
  eyebrow?: ReactNode; title: ReactNode; badges?: ReactNode; actions?: ReactNode; mb?: MantineSpacing
}) {
  return (
    <Group justify="space-between" align="flex-end" mb={mb} gap="md">
      <div>
        {eyebrow && <Text size="sm" c="dimmed" mb={2}>{eyebrow}</Text>}
        <Group gap="sm">
          <Title order={1} fz={30} lh={1.15}>{title}</Title>
          {badges}
        </Group>
      </div>
      {actions && <Group gap="xs">{actions}</Group>}
    </Group>
  )
}

export function StatCard({ label, value, hint, hintColor = 'gray', icon: Icon, color = 'brand' }: {
  label: string; value: ReactNode; hint?: ReactNode; hintColor?: string; icon: TablerIcon; color?: string
}) {
  return (
    <Card withBorder padding="md">
      <Group gap="xs" wrap="nowrap">
        <ThemeIcon variant="light" color={color} radius="md" size={30}><Icon size={16} /></ThemeIcon>
        <Text size="sm" c="dimmed" fw={500}>{label}</Text>
      </Group>
      <Group gap="xs" mt="sm" wrap="nowrap">
        <Text fz={24} fw={700} lh={1}>{value}</Text>
        {hint != null && <Badge variant="light" color={hintColor} size="sm">{hint}</Badge>}
      </Group>
    </Card>
  )
}

// capped: özet listesi SUMMARY_LIMIT'e takıldı, rakamlar son kayıtlarla sınırlı.
export function StatGrid({ children, capped }: { children: ReactNode; capped?: boolean }) {
  return (
    <>
      <SimpleGrid cols={{ base: 2, md: 4 }} spacing="md" mb={capped ? 4 : 'lg'} className="stagger">{children}</SimpleGrid>
      {capped && <Text size="xs" c="dimmed" mb="lg">Özet son {SUMMARY_LIMIT} kayda göre.</Text>}
    </>
  )
}

export interface ChipOption { value: string; label: string; count?: number; color?: string }

// Tek seçimli filtre çipleri; null = "Tümü".
export function FilterChips({ value, onChange, options, allCount }: {
  value: string | null; onChange: (v: string | null) => void; options: ChipOption[]; allCount?: number
}) {
  const chip = (v: string | null, label: string, count?: number, color?: string) => (
    <UnstyledButton key={v ?? ''} className="filter-chip" data-active={value === v || undefined}
      aria-pressed={value === v} onClick={() => onChange(v)}>
      {color && <span className="chip-dot" style={{ background: `var(--mantine-color-${color}-filled)` }} />}
      {label}
      {count != null && <span className="chip-count">{count}</span>}
    </UnstyledButton>
  )
  return (
    <Group gap={6}>
      {chip(null, 'Tümü', allCount)}
      {options.map((o) => chip(o.value, o.label, o.count, o.color))}
    </Group>
  )
}

export const eur = (n: number) => n.toLocaleString('tr-TR', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) + ' €'

export function countBy<T>(items: T[] | undefined, key: (t: T) => string): Record<string, number> {
  const out: Record<string, number> = {}
  for (const it of items ?? []) out[key(it)] = (out[key(it)] ?? 0) + 1
  return out
}
