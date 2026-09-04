// Движок тем: набор токенов вместо пары «светлая/тёмная».
//
// Токены выставляются CSS-переменными на <html>, поэтому тему видно во всём
// приложении сразу и без перезагрузки. theme.css задаёт значения по умолчанию
// (на случай, если оформление ещё не загрузилось с сервера), а здесь —
// конкретные темы и правка отдельных цветов.
//
// Рамки рисуются inset-тенью, а не border: тень не влияет на размеры вообще,
// то есть «забирается у ширины», как и требовалось.

export type ThemeKind = 'light' | 'dark'

export interface ThemeTokens {
  bg: string
  text: string
  text_secondary: string
  accent: string
  /** цвет карточки в формате «R, G, B» — прозрачность задаётся отдельно */
  card_rgb: string
  card_alpha: number
  card_blur: number
  cell_bg: string
  hover_bg: string
  border_card_color: string
  border_card_width: number
  border_btn_color: string
  border_btn_width: number
}

export interface Theme {
  id: string
  name: string
  kind: ThemeKind
  tokens: ThemeTokens
}

const dark = (over: Partial<ThemeTokens>): ThemeTokens => ({
  bg: '#121212',
  text: '#e0e0e0',
  text_secondary: '#aaaaaa',
  accent: '#60a5fa',
  card_rgb: '31, 31, 31',
  card_alpha: 1,
  card_blur: 0,
  cell_bg: '#2b2b2b',
  hover_bg: '#424242',
  border_card_color: '#3a3a3a',
  border_card_width: 0,
  border_btn_color: '#3a3a3a',
  border_btn_width: 0,
  ...over,
})

const light = (over: Partial<ThemeTokens>): ThemeTokens =>
  dark({
    bg: '#ffffff',
    text: '#000000',
    text_secondary: '#666666',
    accent: '#2563eb',
    card_rgb: '249, 249, 249',
    cell_bg: '#e2e2e2',
    hover_bg: '#d4d4d4',
    border_card_color: '#c9c9c9',
    border_btn_color: '#c9c9c9',
    ...over,
  })

/**
 * Встроенные темы. Порядок важен: в списке они показываются как есть.
 * «Ночь» и «День» повторяют прежний вид приложения — обновление ничего
 * не ломает у тех, кто ничего не настраивал.
 */
export const BUILTIN_THEMES: Theme[] = [
  { id: 'night', name: 'Ночь', kind: 'dark', tokens: dark({}) },
  {
    id: 'graphite',
    name: 'Графит',
    kind: 'dark',
    tokens: dark({
      bg: '#1a1715',
      text: '#ece5dd',
      text_secondary: '#a9a09a',
      accent: '#e0a458',
      card_rgb: '38, 33, 30',
      cell_bg: '#3a332e',
      hover_bg: '#4a423b',
    }),
  },
  {
    id: 'indigo',
    name: 'Индиго',
    kind: 'dark',
    tokens: dark({
      bg: '#12142a',
      text: '#e2e4f5',
      text_secondary: '#9ba0c8',
      accent: '#8b7cf6',
      card_rgb: '27, 30, 56',
      cell_bg: '#282c52',
      hover_bg: '#353a66',
    }),
  },
  {
    id: 'contrast-dark',
    name: 'Контраст (тёмная)',
    kind: 'dark',
    tokens: dark({
      bg: '#000000',
      text: '#ffffff',
      text_secondary: '#c8c8c8',
      accent: '#ffd400',
      card_rgb: '20, 20, 20',
      cell_bg: '#262626',
      hover_bg: '#3a3a3a',
      border_card_color: '#4d4d4d',
      border_card_width: 1,
    }),
  },
  { id: 'day', name: 'День', kind: 'light', tokens: light({}) },
  {
    id: 'paper',
    name: 'Бумага',
    kind: 'light',
    tokens: light({
      bg: '#f6f1e7',
      text: '#2a2620',
      text_secondary: '#6d6353',
      accent: '#b4622b',
      card_rgb: '255, 251, 243',
      cell_bg: '#e8dfcd',
      hover_bg: '#ded2ba',
    }),
  },
  {
    id: 'sky',
    name: 'Небо',
    kind: 'light',
    tokens: light({
      bg: '#eef4fb',
      text: '#12263a',
      text_secondary: '#5b7288',
      accent: '#0a7ea4',
      card_rgb: '255, 255, 255',
      cell_bg: '#dbe7f3',
      hover_bg: '#c7d9ec',
    }),
  },
  {
    id: 'contrast-light',
    name: 'Контраст (светлая)',
    kind: 'light',
    tokens: light({
      bg: '#ffffff',
      text: '#000000',
      text_secondary: '#3d3d3d',
      accent: '#0b57d0',
      card_rgb: '255, 255, 255',
      cell_bg: '#e6e6e6',
      hover_bg: '#cccccc',
      border_card_color: '#8c8c8c',
      border_card_width: 1,
    }),
  },
]

export function builtinTheme(id: string): Theme | undefined {
  return BUILTIN_THEMES.find((t) => t.id === id)
}

export function defaultTokens(kind: ThemeKind): ThemeTokens {
  return kind === 'light' ? light({}) : dark({})
}

/** Применение токенов: CSS-переменные на <html> + переключатели рамок. */
export function applyTokens(tokens: ThemeTokens, kind: ThemeKind): void {
  const root = document.documentElement
  const set = (name: string, value: string) => root.style.setProperty(name, value)

  set('--bg-color', tokens.bg)
  set('--text-color', tokens.text)
  set('--text-secondary', tokens.text_secondary)
  set('--accent-color', tokens.accent)
  set('--card-rgb', tokens.card_rgb)
  set('--card-alpha', String(tokens.card_alpha))
  set('--card-blur', `${tokens.card_blur}px`)
  set('--cell-bg-color', tokens.cell_bg)
  set('--hover-bg-color', tokens.hover_bg)
  set('--bg-secondary', tokens.cell_bg)
  set('--border-card-color', tokens.border_card_color)
  set('--border-card-width', `${tokens.border_card_width}px`)
  set('--border-btn-color', tokens.border_btn_color)
  set('--border-btn-width', `${tokens.border_btn_width}px`)

  // data-theme оставляем: часть компонентов и Telegram-виджеты смотрят на него
  root.dataset.theme = kind
  // color-scheme говорит браузеру, какой палитрой рисовать РОДНЫЕ элементы:
  // выпадающий список <select>, полосы прокрутки, автозаполнение. Без него
  // вебвью Telegram на десктопе рисует их светлыми, и по тёмной теме получался
  // белый список со светлым текстом. Считаем по яркости фона, а не по kind:
  // своя тема бывает тёмной, но заведённой как светлая.
  const bg = parseColor(tokens.bg)
  root.style.colorScheme = bg && luminance(bg) < 0.4 ? 'dark' : 'light'
  // «стекло» включаем только когда оно реально нужно — backdrop-filter дорогой
  if (tokens.card_alpha < 1 || tokens.card_blur > 0) root.dataset.cardGlass = ''
  else delete root.dataset.cardGlass
  if (tokens.border_card_width > 0) root.dataset.cardBorder = ''
  else delete root.dataset.cardBorder
  if (tokens.border_btn_width > 0) root.dataset.btnBorder = ''
  else delete root.dataset.btnBorder
}

// --- контраст (WCAG) ---

function channel(v: number): number {
  const s = v / 255
  return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
}

export function parseColor(color: string): [number, number, number] | null {
  const hex = color.trim().replace('#', '')
  if (/^[0-9a-f]{6}$/i.test(hex)) {
    return [parseInt(hex.slice(0, 2), 16), parseInt(hex.slice(2, 4), 16), parseInt(hex.slice(4, 6), 16)]
  }
  if (/^[0-9a-f]{3}$/i.test(hex)) {
    return [
      parseInt(hex[0] + hex[0], 16),
      parseInt(hex[1] + hex[1], 16),
      parseInt(hex[2] + hex[2], 16),
    ]
  }
  const m = color.match(/(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/)
  return m ? [+m[1], +m[2], +m[3]] : null
}

export function luminance(rgb: [number, number, number]): number {
  return 0.2126 * channel(rgb[0]) + 0.7152 * channel(rgb[1]) + 0.0722 * channel(rgb[2])
}

/** Коэффициент контраста двух цветов: 1 — одинаковые, 21 — чёрный/белый. */
export function contrastRatio(a: string, b: string): number | null {
  const ca = parseColor(a)
  const cb = parseColor(b)
  if (!ca || !cb) return null
  const la = luminance(ca)
  const lb = luminance(cb)
  const [hi, lo] = la > lb ? [la, lb] : [lb, la]
  return (hi + 0.05) / (lo + 0.05)
}

/** Хватает ли контраста для основного текста (порог WCAG AA — 4.5). */
export function contrastOk(text: string, bg: string): boolean {
  const r = contrastRatio(text, bg)
  return r === null || r >= 4.5
}

// --- генератор темы из одного цвета ---

function hexToHsl(hex: string): [number, number, number] | null {
  const rgb = parseColor(hex)
  if (!rgb) return null
  const [r, g, b] = rgb.map((v) => v / 255)
  const max = Math.max(r, g, b)
  const min = Math.min(r, g, b)
  const l = (max + min) / 2
  if (max === min) return [0, 0, l]
  const d = max - min
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min)
  let h = 0
  if (max === r) h = ((g - b) / d + (g < b ? 6 : 0)) / 6
  else if (max === g) h = ((b - r) / d + 2) / 6
  else h = ((r - g) / d + 4) / 6
  return [h * 360, s, l]
}

function hsl(h: number, s: number, l: number): string {
  const f = (n: number) => {
    const k = (n + h / 30) % 12
    const a = s * Math.min(l, 1 - l)
    const v = l - a * Math.max(-1, Math.min(k - 3, Math.min(9 - k, 1)))
    return Math.round(255 * v)
  }
  return `#${[f(0), f(8), f(4)].map((v) => v.toString(16).padStart(2, '0')).join('')}`
}

/**
 * Тема из одного акцентного цвета: фон и карточки берут его оттенок, но с
 * низкой насыщенностью — иначе интерфейс выглядит кислотным.
 */
export function generateTheme(accent: string, kind: ThemeKind): ThemeTokens {
  const parsed = hexToHsl(accent)
  const base = defaultTokens(kind)
  if (!parsed) return base
  const [h] = parsed
  if (kind === 'dark') {
    return {
      ...base,
      accent,
      bg: hsl(h, 0.22, 0.07),
      text: hsl(h, 0.08, 0.9),
      text_secondary: hsl(h, 0.1, 0.65),
      card_rgb: parseColor(hsl(h, 0.18, 0.13))!.join(', '),
      cell_bg: hsl(h, 0.18, 0.18),
      hover_bg: hsl(h, 0.18, 0.26),
    }
  }
  return {
    ...base,
    accent,
    bg: hsl(h, 0.35, 0.97),
    text: hsl(h, 0.35, 0.12),
    text_secondary: hsl(h, 0.15, 0.42),
    card_rgb: parseColor(hsl(h, 0.4, 0.995))!.join(', '),
    cell_bg: hsl(h, 0.3, 0.9),
    hover_bg: hsl(h, 0.3, 0.83),
  }
}

/** Подписи токенов для редактора «своей темы». */
export const TOKEN_LABELS: { key: keyof ThemeTokens; label: string; type: 'color' | 'number' }[] = [
  { key: 'bg', label: 'Фон приложения', type: 'color' },
  { key: 'text', label: 'Текст', type: 'color' },
  { key: 'text_secondary', label: 'Второстепенный текст', type: 'color' },
  { key: 'accent', label: 'Кнопки и акценты', type: 'color' },
  { key: 'cell_bg', label: 'Ячейки и поля', type: 'color' },
  { key: 'border_card_color', label: 'Рамка карточек', type: 'color' },
  { key: 'border_btn_color', label: 'Рамка кнопок', type: 'color' },
]
