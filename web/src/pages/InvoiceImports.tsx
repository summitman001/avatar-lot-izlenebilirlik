import { ActionIcon, Alert, Badge, Button, FileButton, Group, Loader, Modal, Table, Text, Tooltip } from '@mantine/core'
import { IconCircleCheck, IconClock, IconFileInvoice, IconTrash, IconUpload, IconX } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { FilterChips, PageHeader, StatCard, StatGrid, SUMMARY_LIMIT, countBy } from '../components/Page'
import {
  ApiError, apiDelete, apiGet, apiUpload, type InvoiceImportDetail, type InvoiceImportListItem, type List,
} from '../lib/api'

function err(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Bilinmeyen hata'
}

const STATUS_LABEL: Record<string, { label: string; color: string }> = {
  extracted: { label: 'Öneri bekliyor', color: 'yellow' },
  confirmed: { label: 'Onaylandı', color: 'green' },
  discarded: { label: 'İptal', color: 'gray' },
}

export default function InvoiceImports() {
  const navigate = useNavigate()
  const qc = useQueryClient()

  const [status, setStatus] = useState<string | null>(null)
  const { data, isLoading, error } = useQuery({
    queryKey: ['invoice-imports'],
    queryFn: () => apiGet<List<InvoiceImportListItem>>('/invoice-imports', { limit: String(SUMMARY_LIMIT) }),
    refetchInterval: 10_000,
  })
  const byStatus = countBy(data?.items, (i) => i.status)
  const rows = data?.items.filter((i) => !status || i.status === status) ?? []
  const n = (v?: number) => (isLoading ? '…' : v ?? 0)

  const upload = useMutation({
    mutationFn: (file: File) => apiUpload<InvoiceImportDetail>('/invoice-imports', file),
    onSuccess: (created) => {
      qc.invalidateQueries({ queryKey: ['invoice-imports'] })
      navigate(`/fatura-okuma/${created.id}`)
    },
  })

  const [toDelete, setToDelete] = useState<InvoiceImportListItem | null>(null)
  const remove = useMutation({
    mutationFn: (id: string) => apiDelete(`/invoice-imports/${id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['invoice-imports'] })
      setToDelete(null)
    },
  })

  return (
    <>
      <PageHeader eyebrow="Fatura ve irsaliyeden okunan öneriler; onaylanınca alış fişi olur" title="Fatura Okuma"
        actions={
          <FileButton onChange={(f) => f && upload.mutate(f)} accept="image/png,image/jpeg,application/pdf">
            {(props) => (
              <Button {...props} leftSection={<IconUpload size={16} />} loading={upload.isPending}>
                Fatura/İrsaliye Yükle
              </Button>
            )}
          </FileButton>
        } />
      <StatGrid capped={data?.items.length === SUMMARY_LIMIT}>
        <StatCard label="Yüklenen belge" value={n(data?.items.length)} icon={IconFileInvoice} />
        <StatCard label="Öneri bekliyor" value={n(byStatus.extracted)} icon={IconClock} color="yellow" />
        <StatCard label="Onaylandı" value={n(byStatus.confirmed)} icon={IconCircleCheck} color="green" />
        <StatCard label="İptal" value={n(byStatus.discarded)} icon={IconX} color="gray" />
      </StatGrid>

      <Alert color="blue" mb="md">
        Bunlar OCR önerileridir. Onaylamadan hiçbir kayıt oluşturulmaz.
      </Alert>

      {upload.isError && <Alert color="red" mb="md">{err(upload.error)}</Alert>}
      {error && <Alert color="red">{err(error)}</Alert>}
      <Group mb="md">
        <FilterChips value={status} onChange={setStatus} allCount={data?.items.length}
          options={Object.entries(STATUS_LABEL).map(([value, st]) => ({ value, label: st.label, count: byStatus[value] ?? 0, color: st.color }))} />
      </Group>
      {isLoading && <Loader />}
      {data && (
        <Table striped highlightOnHover withTableBorder>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Dosya Adı</Table.Th>
              <Table.Th>Yükleme Tarihi</Table.Th>
              <Table.Th ta="right">Genel Güven</Table.Th>
              <Table.Th>Durum</Table.Th>
              <Table.Th>Oluşan Fiş</Table.Th>
              <Table.Th></Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {rows.map((it) => {
              const st = STATUS_LABEL[it.status] ?? { label: it.status, color: 'gray' }
              return (
                <Table.Tr key={it.id} onClick={() => navigate(`/fatura-okuma/${it.id}`)} style={{ cursor: 'pointer' }}>
                  <Table.Td>{it.file_name}</Table.Td>
                  <Table.Td>{new Date(it.uploaded_at).toLocaleString('tr-TR')}</Table.Td>
                  <Table.Td ta="right">%{Math.round(it.overall_confidence * 100)}</Table.Td>
                  <Table.Td><Badge color={st.color} variant="light">{st.label}</Badge></Table.Td>
                  <Table.Td>
                    {it.purchase_receipt_doc_no ? (
                      <Text
                        span c="blue" style={{ cursor: 'pointer' }}
                        onClick={(e) => { e.stopPropagation(); navigate(`/alislar/${it.purchase_receipt_id}`) }}
                      >
                        {it.purchase_receipt_doc_no}
                      </Text>
                    ) : '—'}
                  </Table.Td>
                  <Table.Td ta="right" w={48} onClick={(e) => e.stopPropagation()}>
                    <Tooltip label={it.status === 'confirmed' ? 'Onaylanan belge alış fişinin kaynağıdır, silinemez' : 'Belgeyi sil'}>
                      <ActionIcon variant="subtle" color="red" aria-label="Belgeyi sil" data-disabled={it.status === 'confirmed' || undefined}
                        onClick={() => { if (it.status !== 'confirmed') { remove.reset(); setToDelete(it) } }}>
                        <IconTrash size={16} />
                      </ActionIcon>
                    </Tooltip>
                  </Table.Td>
                </Table.Tr>
              )
            })}
            {rows.length === 0 && (
              <Table.Tr><Table.Td colSpan={6}><Text c="dimmed" ta="center">{status ? 'Bu durumda belge yok' : 'Henüz yüklenen fatura yok'}</Text></Table.Td></Table.Tr>
            )}
          </Table.Tbody>
        </Table>
      )}
      <Modal opened={!!toDelete} onClose={() => setToDelete(null)} title="Belgeyi sil">
        {remove.isError && <Alert color="red" mb="sm">{err(remove.error)}</Alert>}
        <Text size="sm" mb="md">
          <b>{toDelete?.file_name}</b> ve okunan öneriler silinecek. Bu işlem geri alınamaz; stok ve alış kayıtları etkilenmez.
        </Text>
        <Group justify="flex-end">
          <Button variant="default" onClick={() => setToDelete(null)}>Vazgeç</Button>
          <Button color="red" loading={remove.isPending} onClick={() => toDelete && remove.mutate(toDelete.id)}>Sil</Button>
        </Group>
      </Modal>
    </>
  )
}
