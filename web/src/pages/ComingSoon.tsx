import { Text } from '@mantine/core'
import { PageHeader } from '../components/Page'

// Henüz yapılmamış modüller için placeholder.
export default function ComingSoon({ title }: { title: string }) {
  return (
    <>
      <PageHeader title={title} />
      <Text c="dimmed">Yakında.</Text>
    </>
  )
}
