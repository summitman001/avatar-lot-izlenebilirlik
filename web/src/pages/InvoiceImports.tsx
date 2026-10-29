import { Alert, Badge, Button, FileButton, Group, Loader, Table, Text, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import {
  ApiError, apiGet, apiUpload, type InvoiceImportDetail, type InvoiceImportListItem, type List,
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

  const { data, isLoading, error } = useQuery({
    queryKey: ['invoice-imports'],
    queryFn: () => apiGet<List<InvoiceImportListItem>>('/invoice-imports'),
    refetchInterval: 10_000,
  })

  const upload = useMutation({
    mutationFn: (file: File) => apiUpload<InvoiceImportDetail>('/invoice-imports', file),
    onSuccess: (created) => {
      qc.invalidateQueries({ queryKey: ['invoice-imports'] })
      navigate(`/fatura-okuma/${created.id}`)
    },
  })

  return (
    <>
      <Group justify="space-between" mb="md">
        <Title order={2}>Fatura Okuma</Title>
        <FileButton onChange={(f) => f && upload.mutate(f)} accept="image/png,image/jpeg,application/pdf">
          {(props) => (
            <Button {...props} loading={upload.isPending}>
              Fatura/İrsaliye Yükle
            </Button>
          )}
        </FileButton>
      </Group>

      <Alert color="blue" mb="md">
        Bunlar OCR önerileridir. Onaylamadan hiçbir kayıt oluşturulmaz.
      </Alert>

      {upload.isError && <Alert color="red" mb="md">{err(upload.error)}</Alert>}
      {error && <Alert color="red">{err(error)}</Alert>}
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
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {data.items.map((it) => {
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
                </Table.Tr>
              )
            })}
            {data.items.length === 0 && (
              <Table.Tr><Table.Td colSpan={5}><Text c="dimmed" ta="center">Henüz yüklenen fatura yok</Text></Table.Td></Table.Tr>
            )}
          </Table.Tbody>
        </Table>
      )}
    </>
  )
}
