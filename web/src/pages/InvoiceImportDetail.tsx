import {
  ActionIcon, Alert, Badge, Button, Card, Grid, Group, Image, Loader, Modal, NumberInput,
  Select, Stack, Table, Text, TextInput, Title, Tooltip,
} from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import {
  ApiError, OCR_CONFIDENCE_HIGH, OCR_CONFIDENCE_MEDIUM, apiFileURL, apiGet, apiPost,
  type CreatePurchaseReceiptRequest, type Item, type InvoiceImportDetail as InvoiceImportDetailT,
  type List, type MatchCandidate, type OcrField, type Supplier, type Warehouse,
} from '../lib/api'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

function confidenceColor(c: number): string {
  if (c >= OCR_CONFIDENCE_HIGH) return 'teal'
  if (c >= OCR_CONFIDENCE_MEDIUM) return 'yellow'
  return 'red'
}

// ConfidenceBadge: her alanin yaninda "oneri" etiketi + guven gostergesi (yuksek/orta/dusuk renk
// kodu, bkz. docs/invoice-ocr.md). Hicbir alan "dogru" diye sunulmaz.
function ConfidenceBadge({ field }: { field: OcrField }) {
  return (
    <Tooltip label={field.source_text ? `Okunan: "${field.source_text}"` : 'Okunamadı'} multiline w={260}>
      <Badge size="xs" color={confidenceColor(field.confidence)} variant="light">
        öneri %{Math.round(field.confidence * 100)}
      </Badge>
    </Tooltip>
  )
}

// lowConfidenceStyle: dusuk guvenli alanlar sari cerceveli, kullanicinin dikkatini ceker.
function lowConfidenceStyle(field: OcrField | undefined): React.CSSProperties | undefined {
  if (!field || field.confidence >= OCR_CONFIDENCE_MEDIUM) return undefined
  return { borderColor: 'var(--mantine-color-yellow-6)', borderWidth: 2 }
}

interface LineForm {
  key: number
  lotNo: string
  itemId: string | null
  warehouseId: string | null
  qty: string
  unitPrice: string
  declaredLineTotal: string
  expiryDate: string
  ocrQty?: OcrField
  ocrPrice?: OcrField
  ocrTotal?: OcrField
  ocrLot?: OcrField
  ocrName?: OcrField
  candidates: MatchCandidate[]
  warnings: string[]
}

let nextKey = 1

export default function InvoiceImportDetailPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const qc = useQueryClient()

  const { data, isLoading, error } = useQuery({
    queryKey: ['invoice-import', id],
    queryFn: () => apiGet<InvoiceImportDetailT>(`/invoice-imports/${id}`),
  })

  const suppliers = useQuery({ queryKey: ['suppliers'], queryFn: () => apiGet<List<Supplier>>('/suppliers') })
  const items = useQuery({ queryKey: ['items', 'active'], queryFn: () => apiGet<List<Item>>('/items', { is_active: 'true' }) })
  const warehouses = useQuery({ queryKey: ['warehouses'], queryFn: () => apiGet<List<Warehouse>>('/warehouses') })

  const [supplierId, setSupplierId] = useState<string | null>(null)
  const [invoiceNo, setInvoiceNo] = useState('')
  const [receivedAt, setReceivedAt] = useState('')
  const [lines, setLines] = useState<LineForm[]>([])
  const [formError, setFormError] = useState<string | null>(null)
  const [rawOpened, { toggle: toggleRaw }] = useDisclosure(false)
  const [newSupplierOpened, { open: openNewSupplier, close: closeNewSupplier }] = useDisclosure(false)
  const [newItemForLine, setNewItemForLine] = useState<number | null>(null)

  // OCR onerisi yuklenince formu BASLANGIC degeri olarak doldurur; kullanici sonra her alani
  // duzeltebilir. hicbir alan otomatik "onaylanmis" sayilmaz.
  useEffect(() => {
    if (!data) return
    setInvoiceNo(data.ocr.document_no.value ?? '')
    setReceivedAt(data.ocr.document_date.value ?? new Date().toISOString().slice(0, 10))
    setLines(
      data.ocr.lines.map((l, i) => ({
        key: nextKey++,
        lotNo: l.lot_no.value ?? '',
        itemId: null,
        warehouseId: null,
        qty: l.quantity.value ?? '',
        unitPrice: l.unit_price.value ?? '',
        declaredLineTotal: l.line_total.value ?? '',
        expiryDate: l.expiry_date.value ?? '',
        ocrQty: l.quantity, ocrPrice: l.unit_price, ocrTotal: l.line_total, ocrLot: l.lot_no, ocrName: l.product_name,
        candidates: data.line_item_candidates[i] ?? [],
        warnings: l.warnings,
      })),
    )
  }, [data])

  const confirm = useMutation({
    mutationFn: () => {
      const body: CreatePurchaseReceiptRequest = {
        supplier_id: supplierId!,
        invoice_no: invoiceNo || undefined,
        received_at: receivedAt,
        lines: lines.map((l) => ({
          lot_no: l.lotNo,
          item_id: l.itemId!,
          warehouse_id: l.warehouseId!,
          qty: l.qty,
          unit_price: l.unitPrice,
          declared_line_total: l.declaredLineTotal === '' ? undefined : l.declaredLineTotal,
          expiry_date: l.expiryDate || undefined,
        })),
      }
      return apiPost<{ purchase_receipt_id: string }>(`/invoice-imports/${id}/confirm`, body)
    },
    onSuccess: (updated) => {
      qc.invalidateQueries({ queryKey: ['invoice-imports'] })
      navigate(`/alislar/${updated.purchase_receipt_id}`)
    },
    onError: (e) => setFormError(err(e)),
  })

  const discard = useMutation({
    mutationFn: () => apiPost(`/invoice-imports/${id}/discard`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['invoice-imports'] })
      navigate('/fatura-okuma')
    },
  })

  const createSupplier = useMutation({
    mutationFn: (body: { code: string; name: string }) =>
      apiPost<Supplier>('/suppliers', { ...body, supplier_type: 'material' }),
    onSuccess: (s) => {
      qc.invalidateQueries({ queryKey: ['suppliers'] })
      setSupplierId(s.id)
      closeNewSupplier()
    },
  })

  const createItem = useMutation({
    mutationFn: (body: { code: string; name: string; base_uom: 'kg' | 'adet' }) =>
      apiPost<Item>('/items', { ...body, item_type: 'raw' }),
    onSuccess: (it) => {
      qc.invalidateQueries({ queryKey: ['items', 'active'] })
      if (newItemForLine !== null) updateLine(newItemForLine, { itemId: it.id })
      setNewItemForLine(null)
    },
  })

  const updateLine = (key: number, patch: Partial<LineForm>) =>
    setLines((ls) => ls.map((l) => (l.key === key ? { ...l, ...patch } : l)))

  if (isLoading) return <Loader />
  if (error) return <Alert color="red">{err(error)}</Alert>
  const r = data!

  const supplierOptions = suppliers.data?.items.map((s) => ({ value: s.id, label: `${s.name} (${s.code})` })) ?? []
  const itemOptions = items.data?.items.map((i) => ({ value: i.id, label: `${i.name} (${i.code})` })) ?? []
  const warehouseOptions = warehouses.data?.items.map((w) => ({ value: w.id, label: `${w.name} (${w.code})` })) ?? []

  const linesValid = lines.length > 0 && lines.every((l) => l.lotNo && l.itemId && l.warehouseId && l.qty && l.unitPrice)
  const canConfirm = r.status === 'extracted' && !!supplierId && !!receivedAt && linesValid
  const readOnly = r.status !== 'extracted'

  return (
    <Stack>
      <Alert color="blue" variant="filled">
        Bunlar OCR önerileridir. Onaylamadan hiçbir kayıt oluşturulmaz.
      </Alert>
      <Group justify="space-between">
        <div>
          <Title order={3}>{r.file_name}</Title>
          <Text c="dimmed" size="sm">
            Genel güven:{' '}
            <Badge color={confidenceColor(r.overall_confidence)} variant="light">%{Math.round(r.overall_confidence * 100)}</Badge>
            {'  '}
            <Badge
              ml="xs"
              color={r.status === 'confirmed' ? 'green' : r.status === 'discarded' ? 'gray' : 'yellow'}
              variant="light"
            >
              {r.status === 'confirmed' ? 'Onaylandı' : r.status === 'discarded' ? 'İptal edildi' : 'Öneri bekliyor'}
            </Badge>
          </Text>
        </div>
      </Group>

      {formError && <Alert color="red">{formError}</Alert>}
      {r.ocr.warnings.length > 0 && (
        <Alert color="yellow" title="Belge düzeyinde uyarılar">
          {r.ocr.warnings.join(' · ')}
        </Alert>
      )}

      <Grid>
        <Grid.Col span={{ base: 12, md: 5 }}>
          <Card withBorder p={0} style={{ overflow: 'hidden' }}>
            {r.content_type === 'application/pdf' ? (
              <Text p="md" c="dimmed">PDF önizlemesi desteklenmiyor — dosyayı indirerek kontrol edin.</Text>
            ) : (
              <Image src={apiFileURL(`/invoice-imports/${id}/file`)} fit="contain" fallbackSrc="" />
            )}
          </Card>
        </Grid.Col>

        <Grid.Col span={{ base: 12, md: 7 }}>
          <Card withBorder mb="sm">
            <Group grow mb="sm" align="flex-start">
              <div>
                <Group gap="xs" mb={4}>
                  <Text size="sm" fw={500}>Tedarikçi</Text>
                  <ConfidenceBadge field={r.ocr.supplier_name} />
                </Group>
                <Select
                  placeholder={r.ocr.supplier_name.value ?? 'Tedarikçi seçin'}
                  searchable clearable disabled={readOnly}
                  data={supplierOptions}
                  value={supplierId}
                  onChange={setSupplierId}
                  styles={{ input: lowConfidenceStyle(r.ocr.supplier_name) }}
                />
                {r.supplier_candidates.length > 0 && (
                  <Group gap={4} mt={4}>
                    {r.supplier_candidates.map((c) => (
                      <Badge key={c.id} variant="outline" style={{ cursor: 'pointer' }} onClick={() => setSupplierId(c.id)}>
                        {c.name} (%{Math.round(c.score * 100)})
                      </Badge>
                    ))}
                  </Group>
                )}
                {!readOnly && (
                  <Button variant="subtle" size="compact-xs" mt={4} onClick={openNewSupplier}>+ Yeni tedarikçi</Button>
                )}
              </div>
              <TextInput
                label="Fatura No" disabled={readOnly}
                value={invoiceNo} onChange={(e) => setInvoiceNo(e.currentTarget.value)}
                rightSection={<ConfidenceBadge field={r.ocr.document_no} />} rightSectionWidth={90}
                styles={{ input: lowConfidenceStyle(r.ocr.document_no) }}
              />
            </Group>
            <TextInput
              type="date" label="Tarih" required disabled={readOnly} w={220}
              value={receivedAt} onChange={(e) => setReceivedAt(e.currentTarget.value)}
              styles={{ input: lowConfidenceStyle(r.ocr.document_date) }}
            />
            <Text size="xs" c="dimmed" mt={4}>
              Para birimi (öneri): {r.ocr.currency.value ?? '—'} <ConfidenceBadge field={r.ocr.currency} />
              {' '}— sistem tüm tutarları € olarak kaydeder.
            </Text>
          </Card>

          <Card withBorder mb="sm">
            <Text fw={500} mb="xs">Satırlar</Text>
            <Table>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Lot No</Table.Th>
                  <Table.Th>Ürün</Table.Th>
                  <Table.Th>Depo</Table.Th>
                  <Table.Th ta="right">Miktar</Table.Th>
                  <Table.Th ta="right">Birim Fiyat</Table.Th>
                  <Table.Th ta="right">Beyan Edilen Tutar</Table.Th>
                  <Table.Th>SKT</Table.Th>
                  {!readOnly && <Table.Th></Table.Th>}
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {lines.map((l) => {
                  const computed = Number(l.qty) * Number(l.unitPrice)
                  const declared = l.declaredLineTotal === '' ? null : Number(l.declaredLineTotal)
                  const mismatch = declared !== null && Number.isFinite(computed) && Math.abs(computed - declared) > 0.02
                  return (
                    <Table.Tr key={l.key} bg={mismatch ? 'red.0' : undefined}>
                      <Table.Td>
                        <Group gap={4}>
                          <TextInput disabled={readOnly} value={l.lotNo} onChange={(e) => updateLine(l.key, { lotNo: e.currentTarget.value })}
                            w={120} styles={{ input: lowConfidenceStyle(l.ocrLot) }} />
                          {l.ocrLot && <ConfidenceBadge field={l.ocrLot} />}
                        </Group>
                      </Table.Td>
                      <Table.Td>
                        <Select
                          placeholder={l.ocrName?.value ?? 'Ürün'} searchable disabled={readOnly}
                          data={itemOptions} value={l.itemId} onChange={(v) => updateLine(l.key, { itemId: v })}
                          w={180} styles={{ input: lowConfidenceStyle(l.ocrName) }}
                        />
                        {l.candidates.length > 0 && (
                          <Group gap={4} mt={2}>
                            {l.candidates.map((c) => (
                              <Badge key={c.id} size="xs" variant="outline" style={{ cursor: 'pointer' }}
                                onClick={() => updateLine(l.key, { itemId: c.id })}>
                                {c.name}
                              </Badge>
                            ))}
                          </Group>
                        )}
                        {!readOnly && (
                          <Button variant="subtle" size="compact-xs" onClick={() => setNewItemForLine(l.key)}>+ Yeni ürün</Button>
                        )}
                      </Table.Td>
                      <Table.Td>
                        <Select placeholder="Depo" disabled={readOnly} data={warehouseOptions}
                          value={l.warehouseId} onChange={(v) => updateLine(l.key, { warehouseId: v })} w={140} />
                      </Table.Td>
                      <Table.Td>
                        <NumberInput disabled={readOnly} min={0} decimalScale={3} value={l.qty}
                          onChange={(v) => updateLine(l.key, { qty: String(v) })} w={100} ml="auto"
                          styles={{ input: lowConfidenceStyle(l.ocrQty) }} />
                      </Table.Td>
                      <Table.Td>
                        <NumberInput disabled={readOnly} min={0} decimalScale={4} value={l.unitPrice}
                          onChange={(v) => updateLine(l.key, { unitPrice: String(v) })} w={110} ml="auto"
                          styles={{ input: lowConfidenceStyle(l.ocrPrice) }} />
                      </Table.Td>
                      <Table.Td>
                        <NumberInput disabled={readOnly} min={0} decimalScale={4} value={l.declaredLineTotal}
                          onChange={(v) => updateLine(l.key, { declaredLineTotal: String(v) })} w={120} ml="auto"
                          styles={{ input: mismatch ? { borderColor: 'var(--mantine-color-red-6)', borderWidth: 2 } : lowConfidenceStyle(l.ocrTotal) }} />
                        {mismatch && <Text size="xs" c="red">miktar×fiyat ≠ tutar</Text>}
                      </Table.Td>
                      <Table.Td>
                        <TextInput type="date" disabled={readOnly} value={l.expiryDate}
                          onChange={(e) => updateLine(l.key, { expiryDate: e.currentTarget.value })} w={140} />
                      </Table.Td>
                      {!readOnly && (
                        <Table.Td>
                          <ActionIcon color="red" variant="subtle" disabled={lines.length === 1}
                            onClick={() => setLines((ls) => ls.filter((x) => x.key !== l.key))}>✕</ActionIcon>
                        </Table.Td>
                      )}
                    </Table.Tr>
                  )
                })}
              </Table.Tbody>
            </Table>
            {!readOnly && (
              <Button variant="light" size="xs" mt="sm" onClick={() => setLines((ls) => [...ls, {
                key: nextKey++, lotNo: '', itemId: null, warehouseId: null, qty: '', unitPrice: '', declaredLineTotal: '',
                expiryDate: '', candidates: [], warnings: [],
              }])}>
                + Satır Ekle
              </Button>
            )}
          </Card>

          <Card withBorder mb="sm">
            <Button variant="subtle" size="xs" onClick={toggleRaw}>
              {rawOpened ? 'Ham OCR metnini gizle' : 'Ham OCR metnini göster'}
            </Button>
            {rawOpened && (
              <Text component="pre" size="xs" mt="sm" style={{ whiteSpace: 'pre-wrap' }}>
                {r.ocr.raw_text || '(metin okunamadı)'}
              </Text>
            )}
          </Card>

          {!readOnly && (
            <Group justify="flex-end">
              <Button variant="default" loading={discard.isPending} onClick={() => discard.mutate()}>Vazgeç</Button>
              <Button loading={confirm.isPending} disabled={!canConfirm} onClick={() => confirm.mutate()}>
                Onayla ve alış fişi oluştur
              </Button>
            </Group>
          )}
          {r.status === 'confirmed' && r.purchase_receipt_id && (
            <Group justify="flex-end">
              <Button onClick={() => navigate(`/alislar/${r.purchase_receipt_id}`)}>Alış Fişini Gör</Button>
            </Group>
          )}
        </Grid.Col>
      </Grid>

      <Modal opened={newSupplierOpened} onClose={closeNewSupplier} title="Yeni Tedarikçi">
        <NewSupplierForm onSubmit={(b) => createSupplier.mutate(b)} loading={createSupplier.isPending} />
      </Modal>
      <Modal opened={newItemForLine !== null} onClose={() => setNewItemForLine(null)} title="Yeni Ürün">
        <NewItemForm onSubmit={(b) => createItem.mutate(b)} loading={createItem.isPending} />
      </Modal>
    </Stack>
  )
}

function NewSupplierForm({ onSubmit, loading }: { onSubmit: (b: { code: string; name: string }) => void; loading: boolean }) {
  const [code, setCode] = useState('')
  const [name, setName] = useState('')
  return (
    <Stack>
      <TextInput label="Kod" value={code} onChange={(e) => setCode(e.currentTarget.value)} required />
      <TextInput label="Ad" value={name} onChange={(e) => setName(e.currentTarget.value)} required />
      <Button disabled={!code || !name} loading={loading} onClick={() => onSubmit({ code, name })}>Oluştur</Button>
    </Stack>
  )
}

function NewItemForm({ onSubmit, loading }: { onSubmit: (b: { code: string; name: string; base_uom: 'kg' | 'adet' }) => void; loading: boolean }) {
  const [code, setCode] = useState('')
  const [name, setName] = useState('')
  const [baseUom, setBaseUom] = useState<'kg' | 'adet'>('kg')
  return (
    <Stack>
      <TextInput label="Kod" value={code} onChange={(e) => setCode(e.currentTarget.value)} required />
      <TextInput label="Ad" value={name} onChange={(e) => setName(e.currentTarget.value)} required />
      <Select label="Birim" data={[{ value: 'kg', label: 'kg' }, { value: 'adet', label: 'adet' }]} value={baseUom}
        onChange={(v) => setBaseUom((v as 'kg' | 'adet') ?? 'kg')} />
      <Button disabled={!code || !name} loading={loading} onClick={() => onSubmit({ code, name, base_uom: baseUom })}>Oluştur</Button>
    </Stack>
  )
}
