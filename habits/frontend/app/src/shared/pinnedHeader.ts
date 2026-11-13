// Настройка «закрепить заголовок при прокрутке».
//
// Хранится НА СЕРВЕРЕ: в localStorage она терялась (Telegram чистит хранилище
// WebView, а у веб-версии и расширения оно вообще своё) — ровно та же история,
// что была с темой до v2.61. localStorage остался кэшем, чтобы первый кадр
// рисовался сразу, без ожидания ответа сервера и без прыжка вёрстки.
//
// Есть общий выключатель pinAll — «закрепить на всех страницах», он живёт в
// общих настройках и перекрывает точечные отметки.
import { ref } from 'vue'
import { api } from './api/client'
import { showToast } from './toast'

const KEY = 'pinned_header_pages'
const KEY_ALL = 'pinned_header_all'

interface HeaderSettings {
  pin_all: boolean
  pages: string[]
}

function loadCache(): HeaderSettings {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || '[]')
    return {
      pin_all: localStorage.getItem(KEY_ALL) === '1',
      pages: Array.isArray(raw) ? raw.filter((x) => typeof x === 'string') : [],
    }
  } catch {
    return { pin_all: false, pages: [] }
  }
}

function saveCache(s: HeaderSettings): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(s.pages))
    localStorage.setItem(KEY_ALL, s.pin_all ? '1' : '0')
  } catch {
    /* приватный режим — переживём, сервер всё равно источник истины */
  }
}

const cached = loadCache()
export const pinnedPages = ref<string[]>(cached.pages)
export const pinAllHeaders = ref<boolean>(cached.pin_all)

/** Закреплён ли заголовок на странице (по имени роута). */
export function isHeaderPinned(name: string | null | undefined): boolean {
  if (pinAllHeaders.value) return true
  return !!name && pinnedPages.value.includes(name)
}

function apply(s: HeaderSettings): void {
  pinnedPages.value = s.pages
  pinAllHeaders.value = s.pin_all
  saveCache(s)
}

/** Загрузка с сервера при старте приложения. */
export async function loadPinnedHeaders(): Promise<void> {
  try {
    apply(await api.get<HeaderSettings>('/settings/headers'))
  } catch {
    /* нет сети — остаёмся на кэше */
  }
}

async function push(s: HeaderSettings): Promise<void> {
  const before: HeaderSettings = { pin_all: pinAllHeaders.value, pages: pinnedPages.value }
  apply(s) // оптимистично: интерфейс не ждёт сервер
  try {
    apply(await api.put<HeaderSettings>('/settings/headers', s))
  } catch {
    // молча оставить включённым то, что не сохранилось, — значит снова
    // получить «настройка сбрасывается»: откатываем и говорим об этом
    apply(before)
    showToast('Не удалось сохранить настройку заголовка')
  }
}

export function setHeaderPinned(name: string, pinned: boolean): void {
  if (!name) return
  const set = new Set(pinnedPages.value)
  if (pinned) set.add(name)
  else set.delete(name)
  void push({ pin_all: pinAllHeaders.value, pages: [...set] })
}

/** Общий выключатель: закрепить заголовок на всех страницах. */
export function setPinAllHeaders(pinned: boolean): void {
  void push({ pin_all: pinned, pages: pinnedPages.value })
}
