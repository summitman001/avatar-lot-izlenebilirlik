import { AppShell, Badge, Group, NavLink, ScrollArea, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { NavLink as RouterLink, Route, Routes } from 'react-router-dom'
import { API_URL, apiGet, type AnomalyAlertSummary } from './lib/api'
import AnomalyAlertsPage from './pages/AnomalyAlerts'
import ComingSoon from './pages/ComingSoon'
import CuttingOrderDetail from './pages/CuttingOrderDetail'
import CuttingOrders from './pages/CuttingOrders'
import Dashboard from './pages/Dashboard'
import Lots from './pages/Lots'
import ProductionOrderDetail from './pages/ProductionOrderDetail'
import ProductionOrders from './pages/ProductionOrders'
import Products from './pages/Products'
import Predictions from './pages/Predictions'
import PurchaseReceiptDetail from './pages/PurchaseReceiptDetail'
import Purchases from './pages/Purchases'
import RecallDetail from './pages/RecallDetail'
import Recalls from './pages/Recalls'
import SalesOrderDetail from './pages/SalesOrderDetail'
import SalesOrders from './pages/SalesOrders'
import Traceability from './pages/Traceability'

// Sol menu. `ready: false` olanlar "Yakında" placeholder'ıdır (docs/HANDOFF.md sıradaki işler).
const MENU = [
  { path: '/', label: 'Dashboard', ready: true },
  { path: '/urunler', label: 'Ürünler', ready: true },
  { path: '/lotlar', label: 'Lotlar', ready: true },
  { path: '/alislar', label: 'Alışlar', ready: true },
  { path: '/dis-uretim', label: 'Dış Üretim', ready: true },
  { path: '/parcalama', label: 'Parçalama', ready: true },
  { path: '/satis-sevkiyat', label: 'Satış/Sevkiyat', ready: true },
  { path: '/izlenebilirlik', label: 'İzlenebilirlik', ready: true },
  { path: '/geri-cagirma', label: 'Geri Çağırma', ready: true },
  { path: '/tahminler', label: 'Tahminler', ready: true },
  { path: '/uyarilar', label: 'Uyarılar', ready: true },
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

// Üst bardaki açık kritik anomali uyarısı rozeti (bkz. docs/anomaly-integration.md).
function CriticalAlertsBadge() {
  const { data } = useQuery({
    queryKey: ['anomaly-alert-summary'],
    queryFn: () => apiGet<AnomalyAlertSummary>('/anomaly-alerts/summary'),
    refetchInterval: 15_000,
  })
  if (!data || data.critical === 0) return null
  return (
    <Badge color="red" component={RouterLink} to="/uyarilar" style={{ cursor: 'pointer' }}>
      {data.critical} kritik uyarı
    </Badge>
  )
}

export default function App() {
  return (
    <AppShell header={{ height: 56 }} navbar={{ width: 230, breakpoint: 'sm' }} padding="md">
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between">
          <Title order={4}>Üretim · Maliyet · Lot İzlenebilirliği</Title>
          <Group>
            <CriticalAlertsBadge />
            <ApiStatus />
          </Group>
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
          <Route path="/" element={<Dashboard />} />
          <Route path="/uyarilar" element={<AnomalyAlertsPage />} />
          <Route path="/urunler" element={<Products />} />
          <Route path="/lotlar" element={<Lots />} />
          <Route path="/alislar" element={<Purchases />} />
          <Route path="/alislar/:id" element={<PurchaseReceiptDetail />} />
          <Route path="/tahminler" element={<Predictions />} />
          <Route path="/dis-uretim" element={<ProductionOrders />} />
          <Route path="/dis-uretim/:id" element={<ProductionOrderDetail />} />
          <Route path="/parcalama" element={<CuttingOrders />} />
          <Route path="/parcalama/:id" element={<CuttingOrderDetail />} />
          <Route path="/satis-sevkiyat" element={<SalesOrders />} />
          <Route path="/satis-sevkiyat/:id" element={<SalesOrderDetail />} />
          <Route path="/izlenebilirlik" element={<Traceability />} />
          <Route path="/geri-cagirma" element={<Recalls />} />
          <Route path="/geri-cagirma/:id" element={<RecallDetail />} />
          <Route path="*" element={<ComingSoon title="Sayfa bulunamadı" />} />
        </Routes>
      </AppShell.Main>
    </AppShell>
  )
}
