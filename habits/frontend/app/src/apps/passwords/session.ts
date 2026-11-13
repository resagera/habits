// Состояние хранилища паролей и синхронизация с сервером.
// Источник истины — сервер (один зашифрованный блоб + версия,
// optimistic-блокировка против перезаписи со старого устройства).
// localStorage — кэш на случай недоступности сервера/плохой связи.
import { ref } from 'vue'
import { api, ApiError } from '../../shared/api/client'
import { showToast } from '../../shared/toast'
import { openVaultBlob, sealVault, type VaultData, type VaultEntry, type VaultFolder } from './crypto'

export const unlocked = ref(false)
export const entries = ref<VaultEntry[]>([])
export const folders = ref<VaultFolder[]>([])

/** Версия блоба на сервере и время последнего дампа. */
export const vaultVersion = ref(0)
export const lastSyncAt = ref<string | null>(null)
/** Есть несинхронизированные изменения (сервер был недоступен). */
export const syncDirty = ref(false)
/** Последняя загрузка пришла из кэша (оффлайн). */
export const offlineMode = ref(false)

let masterPassword: string | null = null
let lockTimer: ReturnType<typeof setTimeout> | undefined

// старый ключ v1 остаётся кэшом — существующие локальные хранилища
// автоматически мигрируют на сервер при первом входе
const CACHE_KEY = 'passwords_vault_v1'
const META_KEY = 'passwords_vault_meta_v1'

interface CacheMeta {
  baseVersion: number
  dirty: boolean
  updatedAt: string | null
}

function readMeta(): CacheMeta {
  try {
    const raw = localStorage.getItem(META_KEY)
    if (raw) return JSON.parse(raw)
  } catch {
    /* повреждено — считаем legacy */
  }
  return { baseVersion: 0, dirty: true, updatedAt: null }
}

function writeCache(blob: string, meta: CacheMeta): void {
  localStorage.setItem(CACHE_KEY, blob)
  localStorage.setItem(META_KEY, JSON.stringify(meta))
}

interface ServerVault {
  version: number
  blob: string
  updated_at: string
}

async function fetchServer(): Promise<ServerVault | null | 'offline'> {
  try {
    const { vault } = await api.get<{ vault: ServerVault | null }>('/passwords/vault')
    return vault
  } catch {
    return 'offline'
  }
}

export interface VaultState {
  blob: string | null
  baseVersion: number
  dirty: boolean
  offline: boolean
}

/**
 * Определяет актуальный блоб: сервер / несинхронизированный кэш / legacy
 * локальное хранилище (dirty → будет отправлен после разблокировки).
 */
export async function fetchVaultState(): Promise<VaultState> {
  const server = await fetchServer()
  const cached = localStorage.getItem(CACHE_KEY)
  const meta = readMeta()

  if (server === 'offline') {
    offlineMode.value = true
    return { blob: cached, baseVersion: meta.baseVersion, dirty: meta.dirty, offline: true }
  }
  offlineMode.value = false

  if (server === null) {
    // на сервере пусто: локальный кэш (в т.ч. старое локальное хранилище) — мигрируем
    return { blob: cached, baseVersion: 0, dirty: cached !== null, offline: false }
  }

  vaultVersion.value = server.version
  lastSyncAt.value = server.updated_at
  if (cached !== null && meta.dirty && meta.baseVersion === server.version) {
    // оффлайн-изменения поверх актуальной серверной версии — дошлём их
    return { blob: cached, baseVersion: meta.baseVersion, dirty: true, offline: false }
  }
  // сервер главнее (в т.ч. если кэш опоздал: изменения с другого устройства)
  writeCache(server.blob, { baseVersion: server.version, dirty: false, updatedAt: server.updated_at })
  return { blob: server.blob, baseVersion: server.version, dirty: false, offline: false }
}

let currentBaseVersion = 0

export function openSession(password: string, data: VaultData, baseVersion: number, dirty: boolean): void {
  masterPassword = password
  entries.value = data.entries
  folders.value = data.folders
  currentBaseVersion = baseVersion
  syncDirty.value = dirty
  unlocked.value = true
  resetAutoLock()
}

export function sessionData(): VaultData {
  return { folders: folders.value, entries: entries.value }
}

/**
 * Шифрует и отправляет хранилище на сервер. При недоступности сервера —
 * сохраняет в кэш (dirty). При конфликте версий подтягивает серверную
 * версию и заменяет данные сессии (возвращает 'conflict').
 */
export async function persistSession(): Promise<'ok' | 'offline' | 'conflict'> {
  if (!masterPassword) throw new Error('vault is locked')
  const blob = await sealVault(masterPassword, sessionData())
  try {
    const res = await api.put<{ version: number; updated_at: string }>('/passwords/vault', {
      blob,
      base_version: currentBaseVersion,
    })
    currentBaseVersion = res.version
    vaultVersion.value = res.version
    lastSyncAt.value = res.updated_at
    syncDirty.value = false
    writeCache(blob, { baseVersion: res.version, dirty: false, updatedAt: res.updated_at })
    return 'ok'
  } catch (e) {
    if (e instanceof ApiError && e.status === 409) {
      // хранилище обновлено с другого устройства — берём серверную версию
      const server = await fetchServer()
      if (server && server !== 'offline') {
        const data = await openVaultBlob(masterPassword, server.blob)
        if (data) {
          entries.value = data.entries
          folders.value = data.folders
          currentBaseVersion = server.version
          vaultVersion.value = server.version
          lastSyncAt.value = server.updated_at
          syncDirty.value = false
          writeCache(server.blob, { baseVersion: server.version, dirty: false, updatedAt: server.updated_at })
        }
      }
      showToast('⚠️ Хранилище обновлено с другого устройства — последнее изменение не применено')
      return 'conflict'
    }
    // сервер недоступен: кэшируем локально, дошлём при следующем сохранении
    writeCache(blob, { baseVersion: currentBaseVersion, dirty: true, updatedAt: readMeta().updatedAt })
    syncDirty.value = true
    return 'offline'
  }
}

/** Импорт контейнера как нового хранилища (заменит серверное при входе). */
export function importVaultContainer(raw: string): void {
  writeCache(raw, { baseVersion: -1, dirty: true, updatedAt: null })
}

/** baseVersion=-1 у импорта означает «перезаписать сервер осознанно». */
export async function resolveImportBase(): Promise<number> {
  const server = await fetchServer()
  if (server && server !== 'offline') return server.version
  return 0
}

export function cacheExists(): boolean {
  return localStorage.getItem(CACHE_KEY) !== null
}

/** Есть ли хранилище (на сервере или в кэше) — для выбора экрана. */
export async function initVault(): Promise<'exists' | 'none'> {
  const state = await fetchVaultState()
  return state.blob !== null ? 'exists' : 'none'
}

/**
 * Полный сценарий разблокировки: тянет актуальный блоб, расшифровывает,
 * открывает сессию и досылает несинхронизированные изменения
 * (миграцию старого локального хранилища в том числе).
 */
export async function unlockWithPassword(password: string): Promise<'ok' | 'wrong' | 'empty'> {
  const state = await fetchVaultState()
  if (!state.blob) return 'empty'
  const data = await openVaultBlob(password, state.blob)
  if (!data) return 'wrong'
  let base = state.baseVersion
  if (base === -1) base = await resolveImportBase() // импорт файла: осознанная перезапись
  openSession(password, data, base, state.dirty)
  if (state.dirty && !state.offline) {
    await persistSession()
  }
  return 'ok'
}

/** Создание нового хранилища (сервер пуст) с немедленным дампом. */
export async function createVault(password: string): Promise<void> {
  openSession(password, { folders: [], entries: [] }, 0, true)
  await persistSession()
}

export function deleteVaultEverywhere(): void {
  localStorage.removeItem(CACHE_KEY)
  localStorage.removeItem(META_KEY)
}

export function lock(): void {
  masterPassword = null
  entries.value = []
  folders.value = []
  unlocked.value = false
  clearTimeout(lockTimer)
}

// --- автоблокировка (опциональная, по умолчанию выключена) ---

const AUTOLOCK_KEY = 'passwords_autolock_min'

export function autoLockMinutes(): number {
  const n = Number(localStorage.getItem(AUTOLOCK_KEY) ?? '0')
  return Number.isFinite(n) && n > 0 ? n : 0
}

export function setAutoLockMinutes(min: number): void {
  localStorage.setItem(AUTOLOCK_KEY, String(min))
  resetAutoLock()
}

export function resetAutoLock(): void {
  clearTimeout(lockTimer)
  if (!unlocked.value) return
  const min = autoLockMinutes()
  if (min > 0) lockTimer = setTimeout(lock, min * 60 * 1000)
}

for (const evt of ['click', 'keydown', 'touchstart', 'scroll']) {
  window.addEventListener(evt, resetAutoLock, { passive: true })
}
