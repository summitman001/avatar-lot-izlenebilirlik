import {
  ActionIcon, AppShell, Box, Group, Indicator, Kbd, Select, Stack, Text, ThemeIcon, Tooltip, UnstyledButton,
  useComputedColorScheme, useMantineColorScheme, type ComboboxItem, type OptionsFilter,
} from '@mantine/core'
import { useHotkeys, useLocalStorage } from '@mantine/hooks'
import {
  IconAlertTriangle, IconArrowBackUp, IconBell, IconBox, IconBuildingFactory2, IconChartLine, IconFileInvoice,
  IconLayoutDashboard, IconLayoutSidebarLeftCollapse, IconLayoutSidebarLeftExpand, IconMeat, IconMoon, IconRoute, IconSearch, IconServer, IconShoppingCart, IconStack2,
  IconSun, IconTruckDelivery,
} from '@tabler/icons-react'
import { useQuery } from '@tanstack/react-query'
import { useRef, useState, type MouseEvent } from 'react'
import { flushSync } from 'react-dom'
import { NavLink as RouterLink, Route, Routes, useNavigate } from 'react-router-dom'
import { API_URL, apiGet, type AnomalyAlertSummary } from './lib/api'
import AnomalyAlertsPage from './pages/AnomalyAlerts'
import ComingSoon from './pages/ComingSoon'
import CuttingOrderDetail from './pages/CuttingOrderDetail'
import CuttingOrders from './pages/CuttingOrders'
import Dashboard from './pages/Dashboard'
import InvoiceImportDetail from './pages/InvoiceImportDetail'
import InvoiceImports from './pages/InvoiceImports'
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

// Sol menü (ikon rayı). `ready: false` olanlar "Yakında" placeholder'ıdır (docs/HANDOFF.md sıradaki işler).
const MENU = [
  { path: '/', label: 'Dashboard', icon: IconLayoutDashboard, ready: true },
  { path: '/urunler', label: 'Ürünler', icon: IconBox, ready: true },
  { path: '/lotlar', label: 'Lotlar', icon: IconStack2, ready: true },
  { path: '/alislar', label: 'Alışlar', icon: IconTruckDelivery, ready: true },
  { path: '/fatura-okuma', label: 'Fatura Okuma', icon: IconFileInvoice, ready: true },
  { path: '/dis-uretim', label: 'Dış Üretim', icon: IconBuildingFactory2, ready: true },
  { path: '/parcalama', label: 'Parçalama', icon: IconMeat, ready: true },
  { path: '/satis-sevkiyat', label: 'Satış/Sevkiyat', icon: IconShoppingCart, ready: true },
  { path: '/izlenebilirlik', label: 'İzlenebilirlik', icon: IconRoute, ready: true },
  { path: '/geri-cagirma', label: 'Geri Çağırma', icon: IconArrowBackUp, ready: true },
  { path: '/tahminler', label: 'Tahminler', icon: IconChartLine, ready: true },
  { path: '/uyarilar', label: 'Uyarılar', icon: IconAlertTriangle, ready: true },
]

function ApiStatus() {
  const { isSuccess, isError, isLoading } = useQuery({
    queryKey: ['health'],
    queryFn: () => apiGet<{ status: string }>('/health'),
    refetchInterval: 15_000,
  })
  const label = isLoading ? '…' : isSuccess ? 'Bağlı' : 'Erişilemiyor'
  return (
    <Group gap={8} wrap="nowrap" title={`API: ${API_URL}`}>
      <ThemeIcon size={36} radius="xl" variant="light" color={isSuccess ? 'green' : isError ? 'red' : 'gray'} aria-label={`API ${label}`}>
        <IconServer size={18} />
      </ThemeIcon>
      <Box visibleFrom="md" pr={6}>
        <Text size="xs" c="dimmed" lh={1.2}>API</Text>
        <Text size="sm" fw={600} lh={1.2}>{label}</Text>
      </Box>
    </Group>
  )
}

// Açık anomali uyarısı özeti (bkz. docs/anomaly-integration.md); zil ve menü rozeti aynı sorguyu paylaşır.
function useAlertSummary() {
  return useQuery({
    queryKey: ['anomaly-alert-summary'],
    queryFn: () => apiGet<AnomalyAlertSummary>('/anomaly-alerts/summary'),
    refetchInterval: 15_000,
  })
}

function AlertsBell() {
  const critical = useAlertSummary().data?.critical ?? 0
  return (
    <Tooltip label={critical ? `${critical} kritik uyarı` : 'Uyarılar'}>
      <Indicator label={critical} size={18} color="red" offset={4} disabled={!critical}>
        <ActionIcon component={RouterLink} to="/uyarilar" variant="default" size={40} radius="xl" aria-label="Uyarılar">
          <IconBell size={18} />
        </ActionIcon>
      </Indicator>
    </Tooltip>
  )
}

// Türkçe büyük/küçük harf: "izl" aramasının "İzlenebilirlik"i bulması için.
const trLower = (s: string) => s.toLocaleLowerCase('tr')
const menuFilter: OptionsFilter = ({ options, search }) =>
  (options as ComboboxItem[]).filter((o) => trLower(o.label).includes(trLower(search.trim())))

// Sayfaya atla (Ctrl/⌘+K). Seçim tutulmaz; seçilen sayfaya gidilir, kutu boşalır.
function PageSearch() {
  const navigate = useNavigate()
  const ref = useRef<HTMLInputElement>(null)
  const [search, setSearch] = useState('')
  useHotkeys([['mod+K', () => ref.current?.focus()]])
  return (
    <Select
      ref={ref}
      visibleFrom="sm"
      w={240}
      radius="xl"
      searchable
      selectFirstOptionOnChange
      value={null}
      searchValue={search}
      onSearchChange={setSearch}
      onChange={(path) => { if (path) { navigate(path); setSearch(''); ref.current?.blur() } }}
      data={MENU.map((m) => ({ value: m.path, label: m.label }))}
      filter={menuFilter}
      nothingFoundMessage="Sayfa bulunamadı"
      placeholder="Sayfa ara"
      aria-label="Sayfa ara"
      leftSection={<IconSearch size={16} />}
      rightSection={<Kbd size="xs">Ctrl K</Kbd>}
      rightSectionWidth={64}
      rightSectionPointerEvents="none"
    />
  )
}

// Tema değişimi, basılan düğmenin merkezinden büyüyen bir daire olarak açılır (View Transitions API).
// Desteklemeyen tarayıcıda veya "azaltılmış hareket" tercihinde anında değişir.
function switchScheme(change: () => void, origin: Element) {
  const apply = () => flushSync(change)
  if (!('startViewTransition' in document) || matchMedia('(prefers-reduced-motion: reduce)').matches) return apply()
  const r = origin.getBoundingClientRect()
  const x = r.left + r.width / 2
  const y = r.top + r.height / 2
  const end = Math.hypot(Math.max(x, innerWidth - x), Math.max(y, innerHeight - y))
  document.startViewTransition(apply).ready.then(() => {
    document.documentElement.animate(
      { clipPath: [`circle(0px at ${x}px ${y}px)`, `circle(${end}px at ${x}px ${y}px)`] },
      { duration: 620, easing: 'cubic-bezier(.65,0,.35,1)', pseudoElement: '::view-transition-new(root)' },
    )
  })
}

const SCHEMES = [
  { value: 'light', label: 'Açık tema', icon: IconSun },
  { value: 'dark', label: 'Koyu tema', icon: IconMoon },
] as const

function ThemeSwitch({ expanded }: { expanded: boolean }) {
  const { setColorScheme } = useMantineColorScheme()
  const current = useComputedColorScheme('light')
  return (
    <Stack gap={6} className="app-rail">
      {SCHEMES.map(({ value, label, icon: Icon }) => (
        <Tooltip key={value} label={label} position="right" disabled={expanded}>
          <UnstyledButton
            className="rail-link theme-btn"
            data-active={current === value || undefined}
            aria-pressed={current === value}
            onClick={(e: MouseEvent<HTMLButtonElement>) => {
              if (current !== value) switchScheme(() => setColorScheme(value), e.currentTarget)
            }}
          >
            <Icon size={20} stroke={1.7} />
            <span>{label}</span>
          </UnstyledButton>
        </Tooltip>
      ))}
    </Stack>
  )
}

// Dar halde etiketler `.rail-link` içinde kırpılır; genişleyince görünür (ad, ekran okuyucuya her zaman açık).
function NavRail({ expanded }: { expanded: boolean }) {
  const critical = useAlertSummary().data?.critical ?? 0
  return (
    <Stack gap="sm">
      <ThemeSwitch expanded={expanded} />
      <Stack gap={6} className="app-rail" component="nav" aria-label="Ana menü">
        {MENU.map(({ path, label, icon: Icon, ready }) => (
          <Tooltip key={path} label={ready ? label : `${label} (yakında)`} position="right" disabled={expanded}>
            <RouterLink to={path} end={path === '/'} className="rail-link">
              <Icon size={20} stroke={1.7} />
              <span>{label}</span>
              {path === '/uyarilar' && critical > 0 && <span className="rail-count">{critical}</span>}
            </RouterLink>
          </Tooltip>
        ))}
      </Stack>
    </Stack>
  )
}

function Brand() {
  return (
    <UnstyledButton component={RouterLink} to="/" aria-label="Dashboard">
      <Group gap="sm" wrap="nowrap">
        <ThemeIcon size={42} radius="xl">
          <IconMeat size={22} />
        </ThemeIcon>
        <Box visibleFrom="xs">
          <Text fw={700} lh={1.15}>Lot İzlenebilirlik</Text>
          <Text size="xs" c="dimmed" lh={1.2}>Üretim · Maliyet · İzlenebilirlik</Text>
        </Box>
      </Group>
    </UnstyledButton>
  )
}

export default function App() {
  // Menü genişliği tarayıcıda hatırlanır; ilk çizimde okunur ki açılışta daralıp genişleme animasyonu olmasın.
  const [navExpanded, setNavExpanded] = useLocalStorage({
    key: 'lot-nav-expanded', defaultValue: false, getInitialValueInEffect: false,
  })
  return (
    // breakpoint 0: dar ikon rayı mobilde de açık kalır (hamburger menü yok).
    // 94 = 44 ikon + 2×8 iç boşluk + 2×1 kenarlık + 2×16 navbar boşluğu.
    <AppShell header={{ height: 80 }} navbar={{ width: navExpanded ? 240 : 94, breakpoint: 0 }} padding="lg" withBorder={false}>
      <AppShell.Header className="app-header" px="md" py={12}>
        <Group className="app-bar" justify="space-between" wrap="nowrap">
          <Group gap={6} wrap="nowrap">
            <Tooltip label={navExpanded ? 'Menüyü daralt' : 'Menüyü genişlet'}>
              <ActionIcon
                variant="subtle"
                color="gray"
                size={40}
                radius="xl"
                aria-label={navExpanded ? 'Menüyü daralt' : 'Menüyü genişlet'}
                aria-expanded={navExpanded}
                onClick={() => setNavExpanded((v) => !v)}
              >
                {navExpanded ? <IconLayoutSidebarLeftCollapse size={20} /> : <IconLayoutSidebarLeftExpand size={20} />}
              </ActionIcon>
            </Tooltip>
            <Brand />
          </Group>
          <Group gap="sm" wrap="nowrap">
            <PageSearch />
            <AlertsBell />
            <ApiStatus />
          </Group>
        </Group>
      </AppShell.Header>
      <AppShell.Navbar className="app-nav" bg="transparent" px="md" pb="md">
        <NavRail expanded={navExpanded} />
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
          <Route path="/fatura-okuma" element={<InvoiceImports />} />
          <Route path="/fatura-okuma/:id" element={<InvoiceImportDetail />} />
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
