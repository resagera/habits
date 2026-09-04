// Пользовательский фон приложения. Рисуется в отдельном fixed-слое
// (#app-background), а не через background-attachment: fixed — тот сломан
// в iOS Safari/WebView: фон дёргается и «уезжает» при скролле.
// Поверх — слой #app-background-dim для затемнения/осветления.
import { api } from './api/client'
import { bgHidden } from './appearance'

// contain («вписать») добавлен в v2.67.3: cover для широкой картинки на
// узком экране увеличивает её в разы и обрезает — выглядит как «растянуло».
export type BgPosition = 'cover' | 'contain' | 'repeat' | 'center'

export interface BackgroundImageItem {
  id: number
  url: string
  /** папка, в которой лежит картинка (null — в корне) */
  folder_id?: number | null
  /** уменьшенная копия для экрана выбора */
  thumb?: string
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
  /** свой цвет фона ('' — цвет темы по умолчанию) */
  bg_dark: string
  bg_light: string
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

export interface BgPlacement {
  scale: number
  offset_x: number
  offset_y: number
  focal_x: number
  focal_y: number
}

const DEFAULT_PLACEMENT: BgPlacement = {
  scale: 100, offset_x: 0, offset_y: 0, focal_x: 50, focal_y: 50,
}

let placement: BgPlacement = DEFAULT_PLACEMENT

/** Размещение приходит из настроек оформления; перерисовываем сразу. */
export function setBgPlacement(p: Partial<BgPlacement>, redraw = true): void {
  placement = { ...DEFAULT_PLACEMENT, ...p }
  if (redraw) reapplyBackground()
}

let last: { url: string; position: BgPosition; blur: number; dim: number } | null = null

/**
 * Натуральные размеры картинок. Масштаб в CSS считается от размера КОНТЕЙНЕРА
 * (`background-size: 150%` — это 150% ширины экрана), а человек ожидает
 * «в полтора раза больше самой картинки». Поэтому размер считаем в пикселях
 * от натурального, а для этого его надо знать.
 */
const natural = new Map<string, { w: number; h: number }>()

export function bgNaturalSize(url: string): { w: number; h: number } | null {
  return natural.get(url) ?? null
}

function withNatural(url: string, cb: (size: { w: number; h: number }) => void): void {
  const known = natural.get(url)
  if (known) {
    cb(known)
    return
  }
  const img = new Image()
  img.onload = () => {
    const size = { w: img.naturalWidth, h: img.naturalHeight }
    natural.set(url, size)
    cb(size)
  }
  img.src = resolveBgUrl(url)
}

export function reapplyBackground(): void {
  if (last) applyBackground(last.url, last.position, last.blur, last.dim)
}

export function applyBackground(url: string, position: BgPosition, blur = 0, dim = 0): void {
  last = { url, position, blur, dim }
  const el = document.getElementById('app-background')
  const dimEl = document.getElementById('app-background-dim')
  if (!el || !dimEl) return

  if (!url || bgHidden()) {
    el.style.backgroundImage = ''
    el.style.filter = ''
    el.style.inset = '0'
    dimEl.style.background = ''
    return
  }

  el.style.backgroundImage = `url("${resolveBgUrl(url)}")`
  const { scale, offset_x: ox, offset_y: oy, focal_x: fx, focal_y: fy } = placement
  if (position === 'repeat' || position === 'center') {
    el.style.backgroundRepeat = position === 'repeat' ? 'repeat' : 'no-repeat'
    el.style.backgroundPosition =
      position === 'repeat' ? `${ox}% ${oy}%` : `${50 + ox / 2}% ${50 + oy / 2}%`
    if (scale === 100) {
      el.style.backgroundSize = 'auto'
    } else {
      // до загрузки размеров показываем натуральный масштаб, потом уточняем
      el.style.backgroundSize = 'auto'
      withNatural(url, ({ w }) => {
        // размеры приходят асинхронно: пока их ждали, режим или картинка могли
        // смениться — тогда результат уже не наш, иначе он перетрёт cover/contain
        if (last?.url === url && last.position === position) {
          el.style.backgroundSize = `${Math.round((w * scale) / 100)}px auto`
        }
      })
    }
  } else if (position === 'contain') {
    // вся картинка целиком, ничего не обрезается и не растягивается
    el.style.backgroundRepeat = 'no-repeat'
    el.style.backgroundSize = 'contain'
    el.style.backgroundPosition = `${fx}% ${fy}%`
  } else {
    // «Заполнить» — cover: картинка не сжимается, а обрезается. Точка фокуса
    // решает, какая её часть останется видимой (у широких фото центр редко
    // оказывается тем, что хочется видеть).
    el.style.backgroundRepeat = 'no-repeat'
    el.style.backgroundSize = 'cover'
    el.style.backgroundPosition = `${fx}% ${fy}%`
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

// ВАЖНО: цвет текста, цвет фона и «стекло» карточек здесь больше НЕ
// применяются. С v2.67 это токены темы (shared/themes.ts): applyTokens ставит
// те же CSS-переменные, и повторное применение из ответа /settings/background
// затирало тему — после смены картинки карточки становились непрозрачными.
// Этот модуль отвечает только за саму картинку и её эффекты.

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
    }
    applyBackground(c.url, c.position, c.blur ?? 0, c.dim ?? 0)
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
    }),
  )
  applyBackground(state.url, state.position, state.blur, state.dim)
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
  bg_dark?: string
  bg_light?: string
  card_opacity?: number
  card_blur?: number
}): Promise<BackgroundState> {
  const state = await api.put<BackgroundState>('/settings/background', req)
  cacheAndApply(state)
  return state
}

export async function uploadBackground(file: File, thumb?: Blob | null): Promise<BackgroundImageItem> {
  const form = new FormData()
  if (thumb) form.append('thumb', thumb, 'thumb.webp')
  form.append('file', file)
  const { image } = await api.upload<{ image: BackgroundImageItem }>('/settings/background/upload', form)
  return image
}

export function deleteBackgroundImage(id: number): Promise<void> {
  return api.delete<void>(`/settings/background/images/${id}`)
}
