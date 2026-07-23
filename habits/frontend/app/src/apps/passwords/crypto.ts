// Криптослой хранилища паролей. Шифрование ВСЕГДА на устройстве:
// PBKDF2-SHA256 (310k итераций) -> AES-256-GCM; мастер-пароль не покидает
// устройство. Наружу (на сервер и в кэш) уходит только шифротекст.
// GCM аутентифицирует данные, поэтому неверный мастер-пароль надёжно
// определяется ошибкой расшифровки.

export interface VaultFolder {
  id: number
  name: string
  /** вложенность; null — корень (v2-папки мигрируются в корень) */
  parent_id: number | null
  /** закреплённые папки показываются первыми */
  pinned?: boolean
}

export interface VaultEntry {
  id: number
  folder_id: number | null
  name: string
  login: string
  password: string
  url?: string
  description?: string
  /** base32-секрет TOTP (2FA), если задан */
  totp?: string
}

export interface VaultData {
  folders: VaultFolder[]
  entries: VaultEntry[]
}

export interface EncryptedContainer {
  kdf: 'PBKDF2-SHA256'
  iter: number
  salt: string // base64
  iv: string // base64
  data: string // base64, AES-GCM(JSON)
}

export type StoredVault = { v: 1 | 2 | 3 } & EncryptedContainer

const ITERATIONS = 310_000

function toB64(buf: ArrayBuffer | Uint8Array): string {
  const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf)
  let s = ''
  for (const b of bytes) s += String.fromCharCode(b)
  return btoa(s)
}

function fromB64(s: string): Uint8Array {
  return Uint8Array.from(atob(s), (c) => c.charCodeAt(0))
}

async function deriveKey(password: string, salt: Uint8Array, iterations: number): Promise<CryptoKey> {
  const material = await crypto.subtle.importKey(
    'raw',
    new TextEncoder().encode(password),
    'PBKDF2',
    false,
    ['deriveKey'],
  )
  return crypto.subtle.deriveKey(
    { name: 'PBKDF2', salt: salt as BufferSource, iterations, hash: 'SHA-256' },
    material,
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt', 'decrypt'],
  )
}

export async function encryptPayload(password: string, payload: unknown): Promise<EncryptedContainer> {
  const salt = crypto.getRandomValues(new Uint8Array(16))
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const key = await deriveKey(password, salt, ITERATIONS)
  const plaintext = new TextEncoder().encode(JSON.stringify(payload))
  const data = await crypto.subtle.encrypt({ name: 'AES-GCM', iv: iv as BufferSource }, key, plaintext)
  return { kdf: 'PBKDF2-SHA256', iter: ITERATIONS, salt: toB64(salt), iv: toB64(iv), data: toB64(data) }
}

/** null — неверный пароль или повреждённые данные. */
export async function decryptPayload(password: string, c: EncryptedContainer): Promise<unknown | null> {
  try {
    const key = await deriveKey(password, fromB64(c.salt), c.iter)
    const plaintext = await crypto.subtle.decrypt(
      { name: 'AES-GCM', iv: fromB64(c.iv) as BufferSource },
      key,
      fromB64(c.data) as BufferSource,
    )
    return JSON.parse(new TextDecoder().decode(plaintext))
  } catch {
    return null
  }
}

/** Приводит расшифрованный payload любой версии к VaultData.
    v1 — массив записей; v2 — папки без вложенности. */
export function normalizeData(payload: unknown): VaultData | null {
  if (Array.isArray(payload)) {
    return {
      folders: [],
      entries: payload.map((e, i) => ({
        id: i + 1,
        folder_id: null,
        name: String(e.name ?? ''),
        login: String(e.login ?? ''),
        password: String(e.password ?? ''),
        description: e.description ? String(e.description) : undefined,
      })),
    }
  }
  const d = payload as VaultData
  if (d && Array.isArray(d.folders) && Array.isArray(d.entries)) {
    for (const f of d.folders) f.parent_id ??= null
    return d
  }
  return null
}

/** Шифрует данные в строку-блоб для сервера/кэша. */
export async function sealVault(password: string, data: VaultData): Promise<string> {
  const container = await encryptPayload(password, data)
  const stored: StoredVault = { v: 3, ...container }
  return JSON.stringify(stored)
}

/** null — неверный пароль или повреждённый блоб. */
export async function openVaultBlob(password: string, blob: string): Promise<VaultData | null> {
  try {
    const stored = JSON.parse(blob) as StoredVault
    const payload = await decryptPayload(password, stored)
    if (payload === null) return null
    return normalizeData(payload)
  } catch {
    return null
  }
}

/** Проверка, что строка похожа на зашифрованный контейнер. */
export function looksLikeContainer(raw: string): boolean {
  try {
    const p = JSON.parse(raw)
    return Boolean(p.salt && p.iv && p.data && p.iter)
  } catch {
    return false
  }
}

// ---------- пакет передачи папки (шифруется случайным ключом) ----------

export interface FolderPackage {
  name: string
  folders: VaultFolder[] // поддерево (parent_id относительны пакета)
  entries: VaultEntry[]
}

export function randomKey(): string {
  const buf = crypto.getRandomValues(new Uint8Array(16))
  return [...buf].map((b) => b.toString(16).padStart(2, '0')).join('')
}

export async function sealPackage(key: string, pkg: FolderPackage): Promise<string> {
  return JSON.stringify(await encryptPayload(key, pkg))
}

export async function openPackage(key: string, payload: string): Promise<FolderPackage | null> {
  try {
    const container = JSON.parse(payload) as EncryptedContainer
    const data = (await decryptPayload(key, container)) as FolderPackage | null
    if (!data || !Array.isArray(data.entries)) return null
    data.folders ??= []
    return data
  } catch {
    return null
  }
}

// ---------- экспорт/импорт файлов ----------

export interface PlainExportFile extends VaultData {
  v: 2 | 3
  format: 'plain'
}

export interface EncryptedExportFile extends EncryptedContainer {
  v: 2 | 3
  format: 'encrypted'
}

export function exportPlainFile(data: VaultData): string {
  const file: PlainExportFile = { v: 3, format: 'plain', folders: data.folders, entries: data.entries }
  return JSON.stringify(file, null, 2)
}

export async function exportEncryptedFile(data: VaultData, filePassword: string): Promise<string> {
  const container = await encryptPayload(filePassword, data)
  const file: EncryptedExportFile = { v: 3, format: 'encrypted', ...container }
  return JSON.stringify(file, null, 2)
}

export type ParsedImport =
  | { kind: 'plain'; data: VaultData }
  | { kind: 'encrypted'; container: EncryptedContainer }

/** Распознаёт файл импорта: plain, encrypted или старый контейнер. */
export function parseImportFile(raw: string): ParsedImport | null {
  try {
    const parsed = JSON.parse(raw)
    if (parsed.format === 'plain') {
      const data = normalizeData({ folders: parsed.folders, entries: parsed.entries })
      return data ? { kind: 'plain', data } : null
    }
    if (parsed.salt && parsed.iv && parsed.data && parsed.iter) {
      return {
        kind: 'encrypted',
        container: {
          kdf: 'PBKDF2-SHA256',
          iter: parsed.iter,
          salt: parsed.salt,
          iv: parsed.iv,
          data: parsed.data,
        },
      }
    }
    return null
  } catch {
    return null
  }
}

/** null — неверный пароль файла. */
export async function decryptImport(container: EncryptedContainer, filePassword: string): Promise<VaultData | null> {
  const payload = await decryptPayload(filePassword, container)
  if (payload === null) return null
  return normalizeData(payload)
}

// ---------- индикатор надёжности пароля ----------

export interface Strength {
  /** 0..4: слабый → отличный */
  score: number
  label: string
  color: string
}

const STRENGTH_LEVELS: [string, string][] = [
  ['очень слабый', '#dc2626'],
  ['слабый', '#f97316'],
  ['средний', '#eab308'],
  ['хороший', '#84cc16'],
  ['отличный', '#22c55e'],
]

export function passwordStrength(p: string): Strength | null {
  if (!p) return null
  let pool = 0
  if (/[a-zа-яё]/.test(p)) pool += 30
  if (/[A-ZА-ЯЁ]/.test(p)) pool += 30
  if (/[0-9]/.test(p)) pool += 10
  if (/[^a-zA-Zа-яёА-ЯЁ0-9]/.test(p)) pool += 25
  // энтропия ~ длина * log2(алфавит); повторы обесцениваем
  const unique = new Set(p).size
  const effLen = Math.min(p.length, unique * 2)
  const bits = effLen * Math.log2(Math.max(pool, 10))
  let score = 0
  if (bits >= 30) score = 1
  if (bits >= 45) score = 2
  if (bits >= 62) score = 3
  if (bits >= 80) score = 4
  const [label, color] = STRENGTH_LEVELS[score]
  return { score, label, color }
}
