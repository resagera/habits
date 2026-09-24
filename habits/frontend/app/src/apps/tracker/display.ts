import type { Category, CategoryColor, MarkInfo } from './types'

export interface CellView {
  marked: boolean
  /** цвет заливки (square/circle/counter) */
  color: string | null
  /** эмодзи-контент (style=emoji) */
  emoji: string | null
  count: number
}

/**
 * Как отобразить день трекера. Для одноцветных/одноэмодзи трекеров
 * сохранённые в отметке цвет/эмодзи игнорируются — смена цвета категории
 * перекрашивает и старые отметки.
 */
export function cellView(cat: Category, info: MarkInfo | undefined): CellView {
  if (!info) return { marked: false, color: null, emoji: null, count: 0 }
  if (cat.kind === 'counter') {
    return { marked: info.count > 0, color: cat.color, emoji: null, count: info.count }
  }
  if (cat.style === 'emoji') {
    const emoji = cat.multi ? (info.emoji ?? cat.emoji) : cat.emoji
    return { marked: true, color: null, emoji, count: info.count }
  }
  const color = cat.multi ? (info.color ?? cat.color) : cat.color
  return { marked: true, color, emoji: null, count: info.count }
}

/**
 * Обработчики «клик +1 / долгое нажатие −1» для ячеек счётчика.
 * Использовать через v-on="pressHandlers(...)".
 * Состояние живёт в WeakMap по DOM-элементу: обработчики пересоздаются
 * при каждом рендере, а «хвостовой» click после долгого нажатия должен
 * игнорироваться и после перерисовки.
 */
interface PressState {
  timer?: ReturnType<typeof setTimeout>
  firedLong: boolean
}

const pressState = new WeakMap<EventTarget, PressState>()

export function pressHandlers(onClick: () => void, onLongPress: () => void) {
  return {
    pointerdown(e: Event) {
      const el = e.currentTarget
      if (!el) return
      const s: PressState = { firedLong: false }
      s.timer = setTimeout(() => {
        s.firedLong = true
        onLongPress()
      }, 500)
      pressState.set(el, s)
    },
    pointerup(e: Event) {
      const s = e.currentTarget && pressState.get(e.currentTarget)
      if (s) clearTimeout(s.timer)
    },
    pointerleave(e: Event) {
      const s = e.currentTarget && pressState.get(e.currentTarget)
      if (s) clearTimeout(s.timer)
    },
    click(e: Event) {
      const s = e.currentTarget && pressState.get(e.currentTarget)
      if (s?.firedLong) {
        s.firedLong = false
        return
      }
      onClick()
    },
    contextmenu(e: Event) {
      e.preventDefault()
    },
  }
}

// Значение цвета отметки: '#rrggbb' или пара '#rrggbb,#rrggbb' (градиент).
// Пара хранится прямо в отметке, а не ссылкой на набор: цвет, которым день
// был отмечен, не должен меняться от правок набора задним числом.
const COLOR_VALUE_RE = /^#[0-9a-fA-F]{6}(,#[0-9a-fA-F]{6})?$/

/** Значение для хранения: цвет набора → строка цвета отметки. */
export function colorValue(c: CategoryColor): string {
  return c.color2 ? `${c.color},${c.color2}` : c.color
}

/**
 * CSS-фон по значению цвета. Проверка регуляркой обязательна: значение
 * приходит из базы и уезжает в style, а не проверенная строка там —
 * это чужой CSS в нашей вёрстке.
 */
export function colorCss(value: string | null | undefined): string {
  if (!value || !COLOR_VALUE_RE.test(value)) return ''
  const [from, to] = value.split(',')
  return to ? `linear-gradient(135deg, ${from} 0%, ${to} 100%)` : from
}

/** Есть ли у трекера набор именованных цветов (тогда кружки вместо палитры). */
export function hasPalette(cat: Category): boolean {
  return cat.kind === 'marks' && cat.style !== 'emoji' && cat.palette.length > 0
}

/** Палитра пресетов для пикера цвета. */
export const PRESET_COLORS = [
  '#4caf50', '#8bc34a', '#cddc39', '#ffc107',
  '#ff9800', '#ff5722', '#f44336', '#e91e63',
  '#9c27b0', '#673ab7', '#3f51b5', '#2196f3',
  '#03a9f4', '#00bcd4', '#009688', '#795548',
]

/** Набор эмодзи для пикера. */
export const PRESET_EMOJI = [
  '✅', '❌', '⭐', '🔥', '💪', '🏃', '🚴', '🏋️',
  '🧘', '🚶', '💧', '🥗', '🍎', '☕', '🚭', '💤',
  '📚', '✍️', '🎯', '🎸', '🎨', '🧹', '💰', '💊',
  '🌞', '🌙', '❤️', '😊', '😐', '😞', '😡', '🥳',
  '🏆', '🎉', '👍', '👎', '🧠', '🗣️', '📝', '📵',
]
