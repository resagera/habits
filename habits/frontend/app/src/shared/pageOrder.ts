// Порядок страниц в меню и на плитках главной.
//
// Раскладка одна и та же в обоих местах: закреплённые вручную сверху, дальше
// страницы, где у пользователя есть данные, внизу пустые. Смысл — не прятать
// ничего, но не заставлять каждый раз пролистывать два десятка страниц, в
// которые человек ни разу не заходил.
//
// Признак «есть данные» считает сервер (одна проверка EXISTS на страницу),
// закрепление хранится там же — в localStorage оно терялось бы, как терялась
// настройка липкой шапки до v2.65. localStorage остался кэшем на первый кадр:
// без него плитки главной успевали нарисоваться в исходном порядке и прыгали.
import { ref } from 'vue'
import type { RouteRecordNormalized } from 'vue-router'
import { api } from './api/client'
import { showToast } from './toast'

const KEY = 'page_order_cache'

interface PageOrder {
  usage: Record<string, boolean>
  pinned: string[]
  /** страницы, скрытые пользователем У СЕБЯ (не доступ — см. setPageHidden) */
  hidden: string[]
}

function loadCache(): PageOrder {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || '{}')
    const codes = (v: unknown) =>
      Array.isArray(v) ? (v as unknown[]).filter((x): x is string => typeof x === 'string') : []
    return {
      usage: raw && typeof raw.usage === 'object' ? raw.usage : {},
      pinned: codes(raw?.pinned),
      hidden: codes(raw?.hidden),
    }
  } catch {
    return { usage: {}, pinned: [], hidden: [] }
  }
}

function saveCache(o: PageOrder): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(o))
  } catch {
    /* приватный режим — переживём, сервер всё равно источник истины */
  }
}

const cached = loadCache()
export const pageUsage = ref<Record<string, boolean>>(cached.usage)
export const menuPinned = ref<string[]>(cached.pinned)
export const menuHidden = ref<string[]>(cached.hidden)

function apply(o: PageOrder): void {
  pageUsage.value = o.usage
  menuPinned.value = o.pinned
  menuHidden.value = o.hidden
  saveCache(o)
}

function snapshot(): PageOrder {
  return { usage: pageUsage.value, pinned: menuPinned.value, hidden: menuHidden.value }
}

/** Загрузка при старте приложения. */
export async function loadPageOrder(): Promise<void> {
  try {
    apply(await api.get<PageOrder>('/me/pages/order'))
  } catch {
    /* нет сети — остаёмся на кэше, порядок просто не обновится */
  }
}

export function isMenuPinned(code: string): boolean {
  return menuPinned.value.includes(code)
}

/**
 * Скрыта ли страница у этого пользователя. Скрытие — не доступ: страница
 * остаётся рабочей по прямой ссылке и в результатах поиска, из меню и с
 * плиток она просто исчезает.
 */
export function isPageHidden(code: string): boolean {
  return menuHidden.value.includes(code)
}

export function hasPageData(code: string): boolean {
  return pageUsage.value[code] === true
}

/** Закрепить/открепить страницу. Оптимистично, с откатом при ошибке. */
export async function toggleMenuPin(code: string): Promise<void> {
  const before = menuPinned.value
  const next = isMenuPinned(code)
    ? before.filter((c) => c !== code)
    : [...before, code] // новые уходят в конец закреплённых: порядок = порядок закрепления
  menuPinned.value = next
  saveCache(snapshot())
  try {
    const resp = await api.put<{ pinned: string[] }>('/me/pages/pinned', { pinned: next })
    menuPinned.value = resp.pinned
    saveCache(snapshot())
  } catch {
    menuPinned.value = before
    saveCache(snapshot())
    showToast('Не удалось сохранить закрепление')
  }
}

/**
 * Заменить список скрытых целиком. Именно списком, а не по странице за раз:
 * «показать все» иначе слало бы пачку запросов, каждый со своим полным
 * списком, и ответ отставшего вернул бы в интерфейс промежуточное состояние.
 */
export async function setHiddenPages(next: string[]): Promise<void> {
  const before = menuHidden.value
  menuHidden.value = next
  saveCache(snapshot())
  try {
    const resp = await api.put<{ hidden: string[] }>('/me/pages/hidden', { hidden: next })
    menuHidden.value = resp.hidden
    saveCache(snapshot())
  } catch {
    menuHidden.value = before
    saveCache(snapshot())
    showToast('Не удалось сохранить видимость страницы')
  }
}

/** Спрятать страницу у себя или вернуть её в меню. */
export function setPageHidden(code: string, hidden: boolean): Promise<void> {
  const before = menuHidden.value
  return setHiddenPages(hidden ? [...before, code] : before.filter((c) => c !== code))
}

/**
 * Группа страницы: 0 — закреплённая, 1 — с данными, 2 — пустая.
 * Экспортируется ради разделителя в меню.
 */
export function pageRank(code: string): number {
  if (isMenuPinned(code)) return 0
  return hasPageData(code) ? 1 : 2
}

/**
 * Скрытые убираются, остальное сортируется. Фильтр здесь, а не у вызывающих:
 * меню и главная зовут одну функцию, и забыть его в одном из мест нельзя.
 * Копия массива обязательна — filter её и делает, sort правит на месте.
 */
export function sortPages(routes: RouteRecordNormalized[]): RouteRecordNormalized[] {
  const code = (r: RouteRecordNormalized) => String(r.name)
  return routes
    .filter((r) => !isPageHidden(code(r)))
    .sort((a, b) => {
      const ra = pageRank(code(a))
      const rb = pageRank(code(b))
      if (ra !== rb) return ra - rb
      if (ra === 0) return menuPinned.value.indexOf(code(a)) - menuPinned.value.indexOf(code(b))
      return 0
    })
}
