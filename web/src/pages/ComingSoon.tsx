import { Stack, Text, Title } from '@mantine/core'

// Henüz yapılmamış modüller için placeholder.
export default function ComingSoon({ title }: { title: string }) {
  return (
    <Stack>
      <Title order={2}>{title}</Title>
      <Text c="dimmed">Yakında.</Text>
    </Stack>
  )
}
