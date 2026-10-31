import { Alert, Badge, Button, Group, Loader, Modal, NumberInput, Select, Table, Text, Textarea, TextInput } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { IconCircleCheck, IconMeat, IconPencil, IconPlus, IconScale } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { FilterChips, PageHeader, StatCard, StatGrid, SUMMARY_LIMIT, countBy } from '../components/Page'
import {
  ApiError, apiGet, apiPost, type CuttingOrder, type CuttingRecipe, type List, type Lot,
} from '../lib/api'

const STATUS: Record<CuttingOrder['status'], { label: string; color: string }> = {
  draft: { label: 'Taslak', color: 'gray' },
  closed: { label: 'Kapandı', color: 'green' },
}
const METHOD_LABEL: Record<CuttingOrder['allocation_method'], string> = { quantity: 'Miktar (kg)', value: 'Değer (€)' }

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

export default function CuttingOrders() {
  const navigate = useNavigate()
  const { data, isLoading, error } = useQuery({
    queryKey: ['cutting-orders'],
    queryFn: () => apiGet<List<CuttingOrder>>('/cutting-orders', { limit: String(SUMMARY_LIMIT) }),
    refetchInterval: 10_000,
  })
  const [opened, { open, close }] = useDisclosure(false)
  const [status, setStatus] = useState<string | null>(null)
  const byStatus = countBy(data?.items, (o) => o.status)
  const unbalanced = data?.items.filter((o) => o.status === 'draft' && !o.closable).length
  const rows = data?.items.filter((o) => !status || o.status === status) ?? []
  const n = (v?: number) => (isLoading ? '…' : v ?? 0)
  const totalKg = data?.items.reduce((sum, o) => sum + Number(o.input_weight_kg), 0)

  return (
    <>
      <PageHeader eyebrow="Karkası parçalara ayır, maliyeti parçalara dağıt" title="Parçalama"
        actions={<Button leftSection={<IconPlus size={16} />} onClick={open}>Yeni Parçalama Emri</Button>} />
      <StatGrid capped={data?.items.length === SUMMARY_LIMIT}>
        <StatCard label="Toplam emir" value={n(data?.items.length)} icon={IconMeat} />
        <StatCard label="Taslak" value={n(byStatus.draft)} icon={IconPencil} color="gray"
          hint={unbalanced ? `${unbalanced} dengesiz` : undefined} hintColor="orange" />
        <StatCard label="Kapandı" value={n(byStatus.closed)} icon={IconCircleCheck} color="green" />
        <StatCard label="Toplam giriş" icon={IconScale} color="orange"
          value={totalKg == null ? '…' : `${totalKg.toLocaleString('tr-TR', { maximumFractionDigits: 3 })} kg`} />
      </StatGrid>
      <Group mb="md">
        <FilterChips value={status} onChange={setStatus} allCount={data?.items.length}
          options={(Object.keys(STATUS) as CuttingOrder['status'][]).map((s) => (
            { value: s, label: STATUS[s].label, count: byStatus[s] ?? 0, color: STATUS[s].color }))} />
      </Group>
      {error && <Alert color="red">{err(error)}</Alert>}
      {isLoading && <Loader />}
      {data && (
        <Table striped highlightOnHover withTableBorder>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Emir No</Table.Th>
              <Table.Th>Giriş Lotu</Table.Th>
              <Table.Th>Hayvan</Table.Th>
              <Table.Th ta="right">Giriş (kg)</Table.Th>
              <Table.Th>Durum</Table.Th>
              <Table.Th>Dağıtım Yöntemi</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {rows.map((o) => (
              <Table.Tr key={o.id} onClick={() => navigate(`/parcalama/${o.id}`)} style={{ cursor: 'pointer' }}>
                <Table.Td>{o.order_no}</Table.Td>
                <Table.Td>{o.input_lot_no}</Table.Td>
                <Table.Td>{o.input_item_name}</Table.Td>
                <Table.Td ta="right">{o.input_weight_kg}</Table.Td>
                <Table.Td>
                  <Badge color={STATUS[o.status].color} variant="light">{STATUS[o.status].label}</Badge>
                  {o.status === 'draft' && !o.closable && (
                    <Badge color="orange" variant="light" ml="xs">denge tutmuyor</Badge>
                  )}
                </Table.Td>
                <Table.Td>{METHOD_LABEL[o.allocation_method]}</Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
      <NewOrderModal opened={opened} onClose={close} />
    </>
  )
}

function NewOrderModal({ opened, onClose }: { opened: boolean; onClose: () => void }) {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [recipeId, setRecipeId] = useState<string | null>(null)
  const [inputLotId, setInputLotId] = useState<string | null>(null)
  const [orderNo, setOrderNo] = useState('')
  const [inputWeightKg, setInputWeightKg] = useState<number | ''>('')
  const [allocationMethod, setAllocationMethod] = useState<string | null>('value')
  const [costBasis, setCostBasis] = useState<string | null>(null)
  const [resolutionNote, setResolutionNote] = useState('')
  const [formError, setFormError] = useState<string | null>(null)

  const recipes = useQuery({ queryKey: ['cutting-recipes'], queryFn: () => apiGet<List<CuttingRecipe>>('/cutting-recipes') })
  const lots = useQuery({ queryKey: ['lots', 'active'], queryFn: () => apiGet<List<Lot>>('/lots', { status: 'active' }) })

  const create = useMutation({
    mutationFn: () =>
      apiPost<CuttingOrder>('/cutting-orders', {
        order_no: orderNo || undefined,
        recipe_id: recipeId,
        input_lot_id: inputLotId,
        input_weight_kg: String(inputWeightKg),
        allocation_method: allocationMethod,
        cut_at: new Date().toISOString(),
        cost_basis: costBasis || undefined,
        resolution_note: resolutionNote || undefined,
      }),
    onSuccess: (order) => {
      qc.invalidateQueries({ queryKey: ['cutting-orders'] })
      onClose()
      navigate(`/parcalama/${order.id}`)
    },
    onError: (e) => setFormError(err(e)),
  })

  const lotOptions = (lots.data?.items ?? []).map((l) => ({
    value: l.id, label: `${l.lot_no} — ${l.item_name} (${l.qty_on_hand ?? '0'} kg mevcut)`,
  }))

  return (
    <Modal opened={opened} onClose={onClose} title="Yeni Parçalama Emri" size="md">
      {formError && <Alert color="red" mb="sm">{formError}</Alert>}
      <Select
        label="Reçete"
        placeholder="Parçalama reçetesi seçin"
        required
        data={recipes.data?.items.map((r) => ({ value: r.id, label: `${r.name} (${r.code})` })) ?? []}
        value={recipeId}
        onChange={setRecipeId}
        mb="sm"
      />
      <Select
        label="Giriş Lotu"
        placeholder="Aktif lot seçin"
        required
        data={lotOptions}
        value={inputLotId}
        onChange={setInputLotId}
        searchable
        mb="sm"
      />
      <TextInput label="Emir No (opsiyonel)" placeholder="CUT-2026-NNN" value={orderNo} onChange={(e) => setOrderNo(e.currentTarget.value)} mb="sm" />
      <Group grow mb="sm">
        <NumberInput label="Giriş Ağırlığı (kg)" required min={0} decimalScale={3} value={inputWeightKg} onChange={(v) => setInputWeightKg(v as number)} />
        <Select
          label="Dağıtım Yöntemi"
          data={[{ value: 'value', label: 'Değer (€)' }, { value: 'quantity', label: 'Miktar (kg)' }]}
          value={allocationMethod}
          onChange={setAllocationMethod}
        />
      </Group>
      <Text size="xs" c="dimmed" mb={4}>
        Giriş lotunun alış belgesinde tutar uyuşmazlığı varsa (ör. hesaplanan ≠ beyan edilen), hangi
        esasın kullanılacağını ve nedenini aşağıda belirtin — aksi halde emir kapatılamaz.
      </Text>
      <Group grow mb="sm">
        <Select
          label="Maliyet Esası (uyuşmazlık varsa zorunlu)"
          placeholder="Belirtilmedi"
          clearable
          data={[{ value: 'computed', label: 'Hesaplanan (kg × birim fiyat)' }, { value: 'declared', label: 'Beyan edilen (belge toplamı)' }]}
          value={costBasis}
          onChange={setCostBasis}
        />
      </Group>
      <Textarea label="Not (uyuşmazlık varsa zorunlu)" value={resolutionNote} onChange={(e) => setResolutionNote(e.currentTarget.value)} mb="md" minRows={2} />
      <Group justify="flex-end">
        <Button variant="default" onClick={onClose}>Vazgeç</Button>
        <Button
          loading={create.isPending}
          disabled={!recipeId || !inputLotId || inputWeightKg === ''}
          onClick={() => create.mutate()}
        >
          Oluştur
        </Button>
      </Group>
    </Modal>
  )
}
