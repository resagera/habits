/**
 * Ключи открытых папок живут ТОЛЬКО в памяти страницы.
 *
 * Ключ хранится неизвлекаемым CryptoKey: даже выполнившийся на странице
 * чужой скрипт не сможет его выгрузить. Строка пароля живёт столько же и
 * нужна ровно для двух вещей — открыть соседнюю папку с тем же паролем без
 * переспроса и переобернуть ключ при смене пароля.
 *
 * Через минуту бездействия всё забывается; на уход со вкладки — тоже.
 */
import { ref } from 'vue'
import { unwrapFolderKey } from './crypto'
import type { VaultFolder } from './types'

/** Сколько ключ живёт без действий. */
export const TTL_MS = 60_000

interface Entry {
  key: CryptoKey
  password: string
  timer: ReturnType<typeof setTimeout>
}

const open = new Map<number, Entry>()

/** Счётчик изменений: интерфейс перерисовывается по нему (Map не реактивна). */
export const lockVersion = ref(0)

function bump() {
  lockVersion.value++
}

function arm(id: number, entry: Entry) {
  clearTimeout(entry.timer)
  entry.timer = setTimeout(() => lockFolder(id), TTL_MS)
}

export function isUnlocked(id: number): boolean {
  return open.has(id)
}

export function keyFor(id: number): CryptoKey | null {
  const e = open.get(id)
  if (!e) return null
  arm(id, e) // любое обращение продлевает жизнь ключа
  return e.key
}

export function passwordFor(id: number): string | null {
  return open.get(id)?.password ?? null
}

export function unlockedCount(): number {
  return open.size
}

function remember(id: number, key: CryptoKey, password: string) {
  const entry: Entry = { key, password, timer: setTimeout(() => lockFolder(id), TTL_MS) }
  open.set(id, entry)
  bump()
}

/** Открыть папку паролем. false — пароль не подошёл. */
export async function unlock(folder: VaultFolder, password: string): Promise<boolean> {
  const key = await unwrapFolderKey(password, folder)
  if (!key) return false
  remember(folder.id, key, password)
  return true
}

/**
 * Попробовать уже введённые пароли: у папок с одним паролем это избавляет
 * от переспроса на каждой, а стоит один PBKDF2 на попытку.
 */
export async function tryKnownPasswords(folder: VaultFolder): Promise<boolean> {
  if (open.has(folder.id)) return true
  const seen = new Set<string>()
  for (const e of open.values()) {
    if (seen.has(e.password)) continue
    seen.add(e.password)
    const key = await unwrapFolderKey(e.password, folder)
    if (key) {
      remember(folder.id, key, e.password)
      return true
    }
  }
  return false
}

export function lockFolder(id: number): void {
  const e = open.get(id)
  if (!e) return
  clearTimeout(e.timer)
  open.delete(id)
  bump()
}

export function lockAll(): void {
  for (const [id, e] of open) {
    clearTimeout(e.timer)
    open.delete(id)
  }
  bump()
}

/** Уход со вкладки запирает сейф: телефон убрали в карман — открыто не осталось. */
export function installAutoLock(): () => void {
  const onHide = () => {
    if (document.visibilityState === 'hidden') lockAll()
  }
  document.addEventListener('visibilitychange', onHide)
  return () => document.removeEventListener('visibilitychange', onHide)
}
