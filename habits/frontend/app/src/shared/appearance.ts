// Оформление приложения: выбор темы, «своя тема» и её сохранение.
//
// Источник истины — сервер: тема одинакова на всех устройствах, а localStorage
// остаётся кэшем, чтобы первый кадр рисовался сразу и не мигал.
//
// Для входа по токену (веб, расширение) оформление своё и на мини-приложение
// не влияет — сервер сам выбирает колонку по виду сессии (с v2.61).
import { computed, ref } from 'vue'
import { api } from './api/client'
import { setBgPlacement } from './background'
import { currentColorScheme, onColorSchemeChange } from './telegram'
import {
  applyTokens, builtinTheme, defaultTokens,
  type Theme, type ThemeKind, type ThemeTokens,
} from './themes'

export interface AppearanceState {
  mode: 'auto' | 'fixed'
  theme_id: string
  auto_light: string
  auto_dark: string
  draft?: Partial<ThemeTokens> & { kind?: ThemeKind }
  /** не показывать фоновую картинку (настройка режима входа) */
  bg_off?: boolean
}

/** Снимок фона, сохранённый вместе с темой (файл у темы свой, скопированный). */
export interface ThemeBackground {
  kind: 'none' | 'file' | 'url'
  file?: string
  thumb?: string
  url?: string
  position?: string
  blur?: number
  dim?: number
  scale?: number
  offset_x?: number
  offset_y?: number
  focal_x?: number
  focal_y?: number
}

export interface SavedTheme {
  id: number
  name: string
  kind: ThemeKind
  tokens: Partial<ThemeTokens>
  bg: ThemeBackground
  position: number
}

/** Ссылка на картинку темы для мини-превью карточки ('' — фона нет). */
export function themeBgUrl(t: SavedTheme, thumb = true): string {
  const bg = t.bg
  if (!bg || bg.kind === 'none') return ''
  if (bg.kind === 'url') return bg.url ?? ''
  const name = thumb && bg.thumb ? bg.thumb : bg.file
  return name ? import.meta.env.BASE_URL + 'uploads/backgrounds/' + name : ''
}

export interface BackgroundPlacement {
  scale: number
  offset_x: number
  offset_y: number
  focal_x: number
  focal_y: number
}

const CACHE_KEY = 'appearance_cache_v2'

export const state = ref<AppearanceState>({
  mode: 'auto', theme_id: 'night', auto_light: 'day', auto_dark: 'night',
})
export const savedThemes = ref<SavedTheme[]>([])
export const placement = ref<BackgroundPlacement>({
  scale: 100, offset_x: 0, offset_y: 0, focal_x: 50, focal_y: 50,
})

/** Идентификатор черновика «Своя тема» — он же значение theme_id. */
export const DRAFT_ID = 'draft'

/** Тема, действующая прямо сейчас (с учётом «авто» и черновика). */
export const activeTheme = computed<Theme>(() => resolveTheme(state.value))

function savedById(id: number): SavedTheme | undefined {
  return savedThemes.value.find((t) => t.id === id)
}

/** id → тема. Понимает встроенные, `saved:<id>` и черновик. */
export function themeById(id: string, fallbackKind: ThemeKind = 'dark'): Theme {
  if (id === DRAFT_ID) {
    const draft = state.value.draft ?? {}
    const kind = draft.kind ?? fallbackKind
    return {
      id: DRAFT_ID, name: 'Своя тема', kind,
      tokens: { ...defaultTokens(kind), ...stripKind(draft) },
    }
  }
  if (id.startsWith('saved:')) {
    const s = savedById(Number(id.slice(6)))
    if (s) {
      return { id, name: s.name, kind: s.kind, tokens: { ...defaultTokens(s.kind), ...s.tokens } }
    }
  }
  return builtinTheme(id) ?? builtinTheme(fallbackKind === 'light' ? 'day' : 'night')!
}

function stripKind(d: Partial<ThemeTokens> & { kind?: ThemeKind }): Partial<ThemeTokens> {
  const { kind, ...rest } = d
  void kind
  return rest
}

function resolveTheme(st: AppearanceState): Theme {
  if (st.mode === 'auto') {
    const scheme = currentColorScheme()
    return themeById(scheme === 'light' ? st.auto_light : st.auto_dark, scheme)
  }
  return themeById(st.theme_id)
}

/** Применить действующую тему к документу. */
export function apply(): void {
  const t = activeTheme.value
  applyTokens(t.tokens, t.kind)
}

function cache(): void {
  try {
    localStorage.setItem(CACHE_KEY, JSON.stringify({
      state: state.value, themes: savedThemes.value, placement: placement.value,
    }))
  } catch {
    /* приватный режим — переживём, сервер всё равно источник истины */
  }
}

/** Мгновенное применение из кэша: до ответа сервера страница уже в теме. */
export function applyCachedAppearance(): void {
  try {
    const raw = localStorage.getItem(CACHE_KEY)
    if (!raw) return
    const c = JSON.parse(raw)
    if (c.state) state.value = c.state
    if (Array.isArray(c.themes)) savedThemes.value = c.themes
    if (c.placement) {
      placement.value = c.placement
      setBgPlacement(c.placement, false) // фон применит его сам следующим кадром
    }
  } catch {
    /* битый кэш — просто рисуем тему по умолчанию */
  }
  apply()
  // «авто» следит за системой: в Telegram — событие темы, в браузере — медиа-запрос
  onColorSchemeChange(() => {
    if (state.value.mode === 'auto') apply()
  })
}

export async function loadAppearance(): Promise<void> {
  try {
    const res = await api.get<{
      state: AppearanceState; themes: SavedTheme[]; placement: BackgroundPlacement
    }>('/appearance')
    state.value = res.state
    savedThemes.value = res.themes ?? []
    placement.value = res.placement ?? placement.value
    setBgPlacement(placement.value)
    cache()
    apply()
  } catch {
    apply() // нет сети — остаёмся на кэше
  }
}

async function push(next: AppearanceState): Promise<void> {
  const before = state.value
  state.value = next
  cache()
  apply()
  try {
    const res = await api.put<{ state: AppearanceState; themes: SavedTheme[] }>('/appearance', next)
    state.value = res.state
    savedThemes.value = res.themes ?? savedThemes.value
    cache()
    apply()
  } catch {
    state.value = before // не сохранилось — не делаем вид, что сохранилось
    cache()
    apply()
    throw new Error('appearance save failed')
  }
}

/**
 * Выбрать тему. У сохранённой темы включаем её целиком (цвета + фон) отдельной
 * ручкой: тема — это весь вид, а не только палитра.
 */
export async function selectTheme(id: string): Promise<void> {
  if (id.startsWith('saved:')) {
    const res = await api.post<{ state: AppearanceState; themes: SavedTheme[] }>(
      `/appearance/themes/${id.slice(6)}/apply`, {},
    )
    state.value = res.state
    savedThemes.value = res.themes ?? savedThemes.value
    cache()
    apply()
    const { loadBackground } = await import('./background')
    await loadBackground() // фон темы уже сохранён сервером — забираем и рисуем
    return
  }
  return push({ ...state.value, mode: 'fixed', theme_id: id })
}

/** Режим «как в системе»: пара тем — светлая и тёмная. */
export function selectAuto(light: string, dark: string): Promise<void> {
  return push({ ...state.value, mode: 'auto', auto_light: light, auto_dark: dark })
}

/**
 * Правка одного токена. Если сейчас выбрана не своя тема — её токены
 * копируются в черновик, и приложение переключается на «Свою тему»: менять
 * встроенную тему на месте нельзя, иначе к ней уже не вернуться.
 */
export function editToken<K extends keyof ThemeTokens>(key: K, value: ThemeTokens[K]): Promise<void> {
  const current = activeTheme.value
  const draft: Partial<ThemeTokens> & { kind?: ThemeKind } =
    state.value.theme_id === DRAFT_ID && state.value.mode === 'fixed'
      ? { ...state.value.draft }
      : { ...current.tokens, kind: current.kind }
  draft[key] = value
  return push({ ...state.value, mode: 'fixed', theme_id: DRAFT_ID, draft })
}

/** Целиком заменить черновик (генератор темы, импорт). */
export function setDraft(tokens: ThemeTokens, kind: ThemeKind): Promise<void> {
  return push({ ...state.value, mode: 'fixed', theme_id: DRAFT_ID, draft: { ...tokens, kind } })
}

/** Сбросить токен к значению темы, на которой черновик основан. */
export function resetToken(key: keyof ThemeTokens): Promise<void> {
  const draft = { ...(state.value.draft ?? {}) }
  delete draft[key]
  return push({ ...state.value, draft })
}

/** Сохранить текущий вид как тему. Фон снимает сервер (он копирует файл). */
export async function saveThemeAs(name: string): Promise<void> {
  const t = activeTheme.value
  const res = await api.post<{ theme: SavedTheme }>('/appearance/themes', {
    name, kind: t.kind, tokens: t.tokens,
  })
  savedThemes.value = [...savedThemes.value.filter((x) => x.id !== res.theme.id), res.theme]
  await push({ ...state.value, mode: 'fixed', theme_id: `saved:${res.theme.id}` })
}

export async function deleteSavedTheme(id: number): Promise<void> {
  await api.delete(`/appearance/themes/${id}`)
  savedThemes.value = savedThemes.value.filter((t) => t.id !== id)
  if (state.value.theme_id === `saved:${id}`) await selectTheme('night')
  else cache()
}

export async function savePlacement(p: BackgroundPlacement): Promise<void> {
  placement.value = await api.put<BackgroundPlacement>('/appearance/placement', p)
  cache()
}

/** Не показывать фоновую картинку (настройка своя у Telegram и у браузера). */
export function bgHidden(): boolean {
  return state.value.bg_off === true
}

export function setBgOff(off: boolean): Promise<void> {
  return push({ ...state.value, bg_off: off })
}
