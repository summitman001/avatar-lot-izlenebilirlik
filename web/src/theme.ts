import { Badge, Button, Card, createTheme, Modal, Table, Tooltip, type MantineColorsTuple } from '@mantine/core'

// Orman yeşili marka rengi; 7. ton açık temada ana renk.
const brand: MantineColorsTuple = [
  '#e8f5ee', '#d3eadd', '#a7d4bb', '#77bd96', '#50a977',
  '#389c64', '#22804e', '#1b6f43', '#135f38', '#074f2c',
]

// Koyu tema: Mantine'in nötr gri skalası yerine hafif yeşil tonlu skala.
const dark: MantineColorsTuple = [
  '#d5dbd7', '#b3bcb6', '#8e9892', '#68736c', '#3a443e',
  '#2c3530', '#222a25', '#181f1b', '#121814', '#0c110e',
]

const font = "'Plus Jakarta Sans', system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif"

export const theme = createTheme({
  colors: { brand, dark },
  primaryColor: 'brand',
  primaryShade: { light: 7, dark: 5 },
  defaultRadius: 'md',
  fontFamily: font,
  headings: { fontFamily: font, fontWeight: '700' },
  components: {
    Card: Card.extend({ defaultProps: { radius: 'lg', padding: 'lg', shadow: 'xs' } }),
    Badge: Badge.extend({ defaultProps: { tt: 'none', fw: 600 } }),
    Button: Button.extend({ defaultProps: { radius: 'xl' } }),
    Modal: Modal.extend({ defaultProps: { radius: 'lg', transitionProps: { transition: 'pop', duration: 200 } } }),
    Table: Table.extend({ defaultProps: { verticalSpacing: 'sm' } }),
    Tooltip: Tooltip.extend({ defaultProps: { radius: 'md', openDelay: 150 } }),
  },
})
