import { AppShell, Badge, Group, NavLink, ScrollArea, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { NavLink as RouterLink, Route, Routes } from 'react-router-dom'
import { API_URL, apiGet } from './lib/api'
import ComingSoon from './pages/ComingSoon'
import Lots from './pages/Lots'
import Products from './pages/Products'

// Sol menu. `ready: false` olanlar "Yakında" placeholder'ıdır (docs/HANDOFF.md sıradaki işler).
const MENU = [
  { path: '/', label: 'Dashboard', ready: false },
  { path: '/urunler', label: 'Ürünler', ready: true },
  { path: '/lotlar', label: 'Lotlar', ready: true },
  { path: '/alislar', label: 'Alışlar', ready: false },
  { path: '/dis-uretim', label: 'Dış Üretim', ready: false },
  { path: '/parcalama', label: 'Parçalama', ready: false },
  { path: '/satis-sevkiyat', label: 'Satış/Sevkiyat', ready: false },
  { path: '/izlenebilirlik', label: 'İzlenebilirlik', ready: false },
  { path: '/geri-cagirma', label: 'Geri Çağırma', ready: false },
  { path: '/tahminler', label: 'Tahminler', ready: false },
  { path: '/uyarilar', label: 'Uyarılar', ready: false },
]

function ApiStatus() {
  const { isSuccess, isError, isLoading } = useQuery({
    queryKey: ['health'],
    queryFn: () => apiGet<{ status: string }>('/health'),
    refetchInterval: 15_000,
  })
  return (
    <Badge color={isSuccess ? 'green' : isError ? 'red' : 'gray'} variant="light" title={API_URL}>
      API: {isLoading ? '…' : isSuccess ? 'bağlı' : 'erişilemiyor'}
    </Badge>
  )
}

export default function App() {
  return (
    <AppShell header={{ height: 56 }} navbar={{ width: 230, breakpoint: 'sm' }} padding="md">
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between">
          <Title order={4}>Üretim · Maliyet · Lot İzlenebilirliği</Title>
          <ApiStatus />
        </Group>
      </AppShell.Header>
      <AppShell.Navbar p="xs">
        <ScrollArea>
          {MENU.map((m) => (
            <NavLink
              key={m.path}
              component={RouterLink}
              to={m.path}
              label={m.label}
              rightSection={m.ready ? null : <Text size="xs" c="dimmed">yakında</Text>}
            />
          ))}
        </ScrollArea>
      </AppShell.Navbar>
      <AppShell.Main>
        <Routes>
          {MENU.filter((m) => !m.ready).map((m) => (
            <Route key={m.path} path={m.path} element={<ComingSoon title={m.label} />} />
          ))}
          <Route path="/urunler" element={<Products />} />
          <Route path="/lotlar" element={<Lots />} />
          <Route path="*" element={<ComingSoon title="Sayfa bulunamadı" />} />
        </Routes>
      </AppShell.Main>
    </AppShell>
  )
}
