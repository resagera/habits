// Хранилище ссылок опционально: локально (localStorage, данные не покидают
// устройство) или на сервере (доступно с любого устройства). Оба варианта
// реализуют один интерфейс LinksBackend; активный выбирается настройкой.
import { api } from '../../shared/api/client'
import type { FolderPatch, LinkFolder, LinkItem, LinkPatch, LinksData, NewLink } from './types'

export type LinksMode = 'local' | 'server'

const MODE_KEY = 'links_storage_mode'
const DATA_KEY = 'links_data_v1'

/**
 * Синхронное чтение из локального кэша. ВАЖНО: источник истины — сервер
 * (см. loadLinksMode): Telegram-webview периодически очищает localStorage,
 * из-за чего выбор «слетал» на локальное хранилище.
 */
export function getLinksMode(): LinksMode {
  return localStorage.getItem(MODE_KEY) === 'server' ? 'server' : 'local'
}

/** Загружает выбор хранилища с сервера; пустой кэш мигрирует на сервер. */
export async function loadLinksMode(): Promise<LinksMode> {
  try {
    const { storage } = await api.get<{ storage: string }>('/links/storage')
    if (storage === 'server' || storage === 'local') {
      localStorage.setItem(MODE_KEY, storage)
      return storage
    }
    // на сервере выбор ещё не сохранён — переносим локальный (если был)
    const cached = localStorage.getItem(MODE_KEY)
    if (cached === 'server' || cached === 'local') {
      await api.put('/links/storage', { storage: cached })
      return cached
    }
  } catch {
    // оффлайн или ошибка — работаем по кэшу
  }
  return getLinksMode()
}

/**
 * Отправка копии папки (с вложенными папками и ссылками) пользователю
 * приложения по точному id или @логину. Только для серверного хранилища —
 * на сервере должна существовать папка с этим id.
 */
export function shareFolderToUser(id: number, to: string) {
  return api.post<{ sent_to: { id: number; username: string; first_name: string }; queued: boolean }>(
    `/links/folders/${id}/share`,
    { to },
  )
}

/** Отправка копии одной ссылки пользователю приложения. */
export function shareLinkToUser(id: number, to: string) {
  return api.post<{ sent_to: { id: number; username: string; first_name: string }; queued: boolean }>(
    `/links/${id}/share`,
    { to },
  )
}

/** Токен и ссылка-приглашение t.me/…?startapp=lnf_<token> для папки. */
export function folderShareToken(id: number) {
  return api.post<{ token: string; link: string }>(`/links/folders/${id}/share-token`)
}

/** Токен и ссылка-приглашение t.me/…?startapp=lnk_<token> для ссылки. */
export function linkShareToken(id: number) {
  return api.post<{ token: string; link: string }>(`/links/${id}/share-token`)
}

export function setLinksMode(mode: LinksMode): void {
  localStorage.setItem(MODE_KEY, mode)
  // сервер — источник истины; ошибка не мешает работе (уйдёт при след. загрузке)
  api.put('/links/storage', { storage: mode }).catch(() => {})
}

export interface LinksBackend {
  loadTree(): Promise<LinksData>
  createFolder(name: string, parentId: number | null): Promise<LinkFolder>
  updateFolder(id: number, patch: FolderPatch): Promise<LinkFolder>
  deleteFolder(id: number): Promise<void>
  createLink(data: NewLink): Promise<LinkItem>
  updateLink(id: number, patch: LinkPatch): Promise<LinkItem>
  deleteLink(id: number): Promise<void>
  /** счётчик переходов (для топ-10); возвращает новое значение */
  click(id: number): Promise<number>
  replaceAll(data: LinksData): Promise<void>
}

// ---------- сервер ----------

const serverBackend: LinksBackend = {
  loadTree: () => api.get<LinksData>('/links/tree'),
  createFolder: (name, parentId) =>
    api.post<{ folder: LinkFolder }>('/links/folders', { name, parent_id: parentId }).then((r) => r.folder),
  updateFolder: (id, patch) =>
    api.patch<{ folder: LinkFolder }>(`/links/folders/${id}`, patch).then((r) => r.folder),
  deleteFolder: (id) => api.delete<void>(`/links/folders/${id}`),
  createLink: (data) => api.post<{ link: LinkItem }>('/links', data).then((r) => r.link),
  updateLink: (id, patch) => api.patch<{ link: LinkItem }>(`/links/${id}`, patch).then((r) => r.link),
  deleteLink: (id) => api.delete<void>(`/links/${id}`),
  click: (id) => api.post<{ clicks: number }>(`/links/${id}/click`).then((r) => r.clicks),
  replaceAll: (data) => api.put<void>('/links/tree', data),
}

// ---------- локально ----------

interface LocalData extends LinksData {
  nextId: number
}

function localLoad(): LocalData {
  try {
    const raw = localStorage.getItem(DATA_KEY)
    if (raw) {
      const d = JSON.parse(raw) as LocalData
      if (Array.isArray(d.folders) && Array.isArray(d.links)) {
        for (const l of d.links) l.clicks ??= 0 // данные старых версий
        return d
      }
    }
  } catch {
    /* повреждённые данные — начинаем заново */
  }
  return { nextId: 1, folders: [], links: [] }
}

function localSave(d: LocalData): void {
  localStorage.setItem(DATA_KEY, JSON.stringify(d))
}

const localBackend: LinksBackend = {
  async loadTree() {
    const d = localLoad()
    return { folders: d.folders, links: d.links }
  },

  async createFolder(name, parentId) {
    const d = localLoad()
    const position = d.folders.filter((f) => f.parent_id === parentId).length
    const folder: LinkFolder = { id: d.nextId++, parent_id: parentId, name, position }
    d.folders.push(folder)
    localSave(d)
    return folder
  },

  async updateFolder(id, patch) {
    const d = localLoad()
    const folder = d.folders.find((f) => f.id === id)
    if (!folder) throw new Error('folder not found')
    if (patch.name !== undefined) folder.name = patch.name
    if (patch.parent_id !== undefined) folder.parent_id = patch.parent_id
    localSave(d)
    return folder
  },

  async deleteFolder(id) {
    const d = localLoad()
    // каскад: собираем id папки и всех потомков
    const doomed = new Set<number>([id])
    let grew = true
    while (grew) {
      grew = false
      for (const f of d.folders) {
        if (f.parent_id !== null && doomed.has(f.parent_id) && !doomed.has(f.id)) {
          doomed.add(f.id)
          grew = true
        }
      }
    }
    d.folders = d.folders.filter((f) => !doomed.has(f.id))
    d.links = d.links.filter((l) => l.folder_id === null || !doomed.has(l.folder_id))
    localSave(d)
  },

  async createLink(data) {
    const d = localLoad()
    const position = d.links.filter((l) => l.folder_id === data.folder_id).length
    const link: LinkItem = { id: d.nextId++, position, clicks: 0, ...data }
    d.links.push(link)
    localSave(d)
    return link
  },

  async updateLink(id, patch) {
    const d = localLoad()
    const link = d.links.find((l) => l.id === id)
    if (!link) throw new Error('link not found')
    Object.assign(link, patch)
    localSave(d)
    return link
  },

  async deleteLink(id) {
    const d = localLoad()
    d.links = d.links.filter((l) => l.id !== id)
    localSave(d)
  },

  async click(id) {
    const d = localLoad()
    const link = d.links.find((l) => l.id === id)
    if (!link) return 0
    link.clicks = (link.clicks ?? 0) + 1
    localSave(d)
    return link.clicks
  },

  async replaceAll(data) {
    let nextId = 1
    for (const f of data.folders) nextId = Math.max(nextId, f.id + 1)
    for (const l of data.links) nextId = Math.max(nextId, l.id + 1)
    localSave({ nextId, folders: data.folders, links: data.links })
  },
}

export function linksBackend(mode: LinksMode = getLinksMode()): LinksBackend {
  return mode === 'server' ? serverBackend : localBackend
}

/**
 * Перенос данных между хранилищами: читает из противоположного режима
 * и полностью заменяет данные в target.
 */
export async function transferLinksData(target: LinksMode): Promise<number> {
  const source: LinksMode = target === 'server' ? 'local' : 'server'
  const data = await linksBackend(source).loadTree()
  await linksBackend(target).replaceAll(data)
  return data.links.length
}

// ---------- настройки отображения ----------

const PREFS_KEY = 'links_prefs_v1'

export interface LinksPrefs {
  showFavorites: boolean
  showTop10: boolean
}

export function getLinksPrefs(): LinksPrefs {
  try {
    const raw = localStorage.getItem(PREFS_KEY)
    if (raw) return { showFavorites: true, showTop10: false, ...JSON.parse(raw) }
  } catch {
    /* ignore */
  }
  return { showFavorites: true, showTop10: false }
}

export function setLinksPrefs(prefs: LinksPrefs): void {
  localStorage.setItem(PREFS_KEY, JSON.stringify(prefs))
}
