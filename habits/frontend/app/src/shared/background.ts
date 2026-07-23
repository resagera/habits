// Пользовательский фон приложения. Рисуется в отдельном fixed-слое
// (#app-background), а не через background-attachment: fixed — тот сломан
// в iOS Safari/WebView: фон дёргается и «уезжает» при скролле.
// Поверх — слой #app-background-dim для затемнения/осветления.
import { api } from './api/client'

export type BgPosition = 'cover' | 'repeat' | 'center'

export interface BackgroundImageItem {
  id: number
  url: string
}

export interface BackgroundState {
  kind: 'none' | 'file' | 'url'
  url: string
  position: BgPosition
  blur: number // px, 0-30
  dim: number // -70 (темнее) .. 70 (светлее)
  /** свой цвет текста ('' — цвет темы по умолчанию) */
  text_dark: string
  text_light: string
  /** карточки-«стекло»: непрозрачность 20-100 (100 — сплошной) и размытие 0-30 */
  card_opacity: number
  card_blur: number
  images: BackgroundImageItem[]
}

const CACHE_KEY = 'bg_cache_v1'

/** Серверные пути относительные (uploads/...) — префиксуем базой приложения. */
export function resolveBgUrl(url: string): string {
  if (!url) return ''
  return /^https?:/i.test(url) ? url : import.meta.env.BASE_URL + url
}

export function applyBackground(url: string, position: BgPosition, blur = 0, dim = 0): void {
  const el = document.getElementById('app-background')
  const dimEl = document.getElementById('app-background-dim')
  if (!el || !dimEl) return

  if (!url) {
    el.style.backgroundImage = ''
    el.style.filter = ''
    el.style.inset = '0'
    dimEl.style.background = ''
    return
  }

  el.style.backgroundImage = `url("${resolveBgUrl(url)}")`
  if (position === 'repeat') {
    el.style.backgroundRepeat = 'repeat'
    el.style.backgroundSize = 'auto'
    el.style.backgroundPosition = 'top left'
  } else if (position === 'center') {
    el.style.backgroundRepeat = 'no-repeat'
    el.style.backgroundSize = 'auto'
    el.style.backgroundPosition = 'center'
  } else {
    el.style.backgroundRepeat = 'no-repeat'
    el.style.backgroundSize = 'cover'
    el.style.backgroundPosition = 'center'
  }

  // Размытие: расширяем слой, чтобы не просвечивали прозрачные края блюра
  el.style.filter = blur > 0 ? `blur(${blur}px)` : ''
  el.style.inset = blur > 0 ? `-${blur * 2}px` : '0'

  // Затемнение (dim < 0) или осветление (dim > 0)
  if (dim < 0) {
    dimEl.style.background = `rgba(0, 0, 0, ${Math.min(-dim, 70) / 100})`
  } else if (dim > 0) {
    dimEl.style.background = `rgba(255, 255, 255, ${Math.min(dim, 70) / 100})`
  } else {
    dimEl.style.background = ''
  }
}

/**
 * Полупрозрачные карточки с размытием фона под ними. Непрозрачность 100 и
 * размытие 0 = прежнее поведение (сплошные карточки, слой backdrop-filter не
 * создаётся). Реализация — через переменные --card-alpha/--card-blur, из
 * которых theme.css собирает --card-color, и атрибут data-card-glass,
 * включающий backdrop-filter на карточках.
 */
export function applyCardStyle(opacity = 100, blur = 0): void {
  const root = document.documentElement
  const op = Math.min(100, Math.max(20, opacity))
  root.style.setProperty('--card-alpha', String(op / 100))
  root.style.setProperty('--card-blur', `${Math.min(30, Math.max(0, blur))}px`)
  if (op < 100 || blur > 0) root.setAttribute('data-card-glass', '')
  else root.removeAttribute('data-card-glass')
}

// --- свой цвет текста: отдельно для тёмной и светлой темы ---

let textColors = { dark: '', light: '' }

/** Ставит --text-color под актуальную тему (пусто — цвет темы по умолчанию). */
export function applyTextColors(dark = textColors.dark, light = textColors.light): void {
  textColors = { dark, light }
  const theme = document.documentElement.dataset.theme === 'light' ? 'light' : 'dark'
  const color = theme === 'dark' ? dark : light
  if (color) document.documentElement.style.setProperty('--text-color', color)
  else document.documentElement.style.removeProperty('--text-color')
}

// смена темы (тумблер в Settings или системная) — переприменяем цвет
new MutationObserver(() => applyTextColors()).observe(document.documentElement, {
  attributes: true,
  attributeFilter: ['data-theme'],
})

/** Мгновенно применяет закэшированный фон до ответа сервера (без «мигания»). */
export function applyCachedBackground(): void {
  try {
    const raw = localStorage.getItem(CACHE_KEY)
    if (!raw) return
    const c = JSON.parse(raw) as {
      url: string
      position: BgPosition
      blur?: number
      dim?: number
      text_dark?: string
      text_light?: string
      card_opacity?: number
      card_blur?: number
    }
    applyBackground(c.url, c.position, c.blur ?? 0, c.dim ?? 0)
    applyTextColors(c.text_dark ?? '', c.text_light ?? '')
    applyCardStyle(c.card_opacity ?? 100, c.card_blur ?? 0)
  } catch {
    /* ignore */
  }
}

function cacheAndApply(state: BackgroundState): void {
  localStorage.setItem(
    CACHE_KEY,
    JSON.stringify({
      url: state.url,
      position: state.position,
      blur: state.blur,
      dim: state.dim,
      text_dark: state.text_dark,
      text_light: state.text_light,
      card_opacity: state.card_opacity,
      card_blur: state.card_blur,
    }),
  )
  applyBackground(state.url, state.position, state.blur, state.dim)
  applyTextColors(state.text_dark, state.text_light)
  applyCardStyle(state.card_opacity, state.card_blur)
}

export async function loadBackground(): Promise<BackgroundState | null> {
  try {
    const state = await api.get<BackgroundState>('/settings/background')
    cacheAndApply(state)
    return state
  } catch {
    return null // вне Telegram / нет сети — остаёмся на кэше
  }
}

export async function setBackground(req: {
  kind: 'none' | 'file' | 'url'
  image_id?: number
  url?: string
  position: BgPosition
  blur?: number
  dim?: number
  text_dark?: string
  text_light?: string
  card_opacity?: number
  card_blur?: number
}): Promise<BackgroundState> {
  const state = await api.put<BackgroundState>('/settings/background', req)
  cacheAndApply(state)
  return state
}

export async function uploadBackground(file: File): Promise<BackgroundImageItem> {
  const form = new FormData()
  form.append('file', file)
  const { image } = await api.upload<{ image: BackgroundImageItem }>('/settings/background/upload', form)
  return image
}

export function deleteBackgroundImage(id: number): Promise<void> {
  return api.delete<void>(`/settings/background/images/${id}`)
}
