/**
 * Криптослой сейфа. Всё шифрование и расшифровка — в браузере; на сервер
 * уходят только шифробайты и конверты ключей.
 *
 * Схема (habits/PLAN-vault.md):
 *   пароль ──PBKDF2-SHA256(salt, 310k)──▶ KEK ──разворачивает──▶ FK (ключ папки)
 *   файл: содержимое ──AES-GCM(CK) чанками──▶ блоб
 *         CK ──AES-GCM(FK)──▶ key_env,  метаданные ──AES-GCM(FK)──▶ meta_env
 *
 * Ключ содержимого на каждый файл (CK) — чтобы смена пароля папки
 * перезаписывала одну обёртку FK, а не перешифровывала все файлы, и чтобы
 * перенос файла в другую папку стоил 32 байта вместо перезаливки.
 */
import type { FileMeta, VaultFolder } from './types'

const ITERATIONS = 310_000
/** Размер чанка: компромисс между числом запросов и памятью на телефоне. */
export const CHUNK_SIZE = 4 << 20
const MAGIC = [0x48, 0x56, 0x4c, 0x54] // 'HVLT'
const VERSION = 1
const HEADER_LEN = 4 + 1 + 4 + 8 // magic + версия + размер чанка + база IV
const TAG_LEN = 16

export function toB64(buf: ArrayBuffer | Uint8Array): string {
  const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf)
  let s = ''
  for (let i = 0; i < bytes.length; i += 0x8000) {
    s += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return btoa(s)
}

export function fromB64(s: string): Uint8Array {
  return Uint8Array.from(atob(s), (c) => c.charCodeAt(0))
}

async function deriveKek(password: string, salt: Uint8Array, iterations: number): Promise<CryptoKey> {
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

/** Конверт: [12 байт IV][шифротекст с тегом] в base64. */
async function seal(key: CryptoKey, plain: BufferSource): Promise<string> {
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const ct = new Uint8Array(await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, key, plain))
  const out = new Uint8Array(iv.length + ct.length)
  out.set(iv)
  out.set(ct, iv.length)
  return toB64(out)
}

async function open(key: CryptoKey, envelope: string): Promise<Uint8Array | null> {
  try {
    const raw = fromB64(envelope)
    const iv = raw.subarray(0, 12)
    const data = raw.subarray(12)
    return new Uint8Array(
      await crypto.subtle.decrypt({ name: 'AES-GCM', iv: iv as BufferSource }, key, data as BufferSource),
    )
  } catch {
    return null // неверный пароль или испорченные данные — GCM это ловит
  }
}

export interface NewFolderKeys {
  kdf_salt: string
  kdf_iter: number
  wrapped_key: string
  wrap_iv: string
  key: CryptoKey
}

/** Новая папка: случайный ключ папки, обёрнутый ключом из пароля. */
export async function createFolderKey(password: string): Promise<NewFolderKeys> {
  const salt = crypto.getRandomValues(new Uint8Array(16))
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const raw = crypto.getRandomValues(new Uint8Array(32))
  const kek = await deriveKek(password, salt, ITERATIONS)
  const wrapped = new Uint8Array(
    await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, kek, raw as BufferSource),
  )
  const key = await crypto.subtle.importKey('raw', raw as BufferSource, 'AES-GCM', false, [
    'encrypt',
    'decrypt',
  ])
  return {
    kdf_salt: toB64(salt),
    kdf_iter: ITERATIONS,
    wrapped_key: toB64(wrapped),
    wrap_iv: toB64(iv),
    key,
  }
}

/**
 * Развернуть ключ папки паролем. null — пароль не подошёл: отдельная
 * проверочная запись не нужна, GCM-тег на обёртке и есть проверка.
 */
export async function unwrapFolderKey(password: string, f: VaultFolder): Promise<CryptoKey | null> {
  try {
    const kek = await deriveKek(password, fromB64(f.kdf_salt), f.kdf_iter)
    const raw = new Uint8Array(
      await crypto.subtle.decrypt(
        { name: 'AES-GCM', iv: fromB64(f.wrap_iv) as BufferSource },
        kek,
        fromB64(f.wrapped_key) as BufferSource,
      ),
    )
    return await crypto.subtle.importKey('raw', raw as BufferSource, 'AES-GCM', false, [
      'encrypt',
      'decrypt',
    ])
  } catch {
    return null
  }
}

/**
 * Переобернуть ключ папки новым паролем (смена пароля).
 * Ключ из памяти сюда не годится — он неизвлекаемый; сырьё разворачиваем
 * заново старым паролем, поэтому его и спрашиваем.
 */
export async function rewrapFolderKey(
  oldPassword: string,
  f: VaultFolder,
  newPassword: string,
): Promise<Omit<NewFolderKeys, 'key'> | null> {
  const kek = await deriveKek(oldPassword, fromB64(f.kdf_salt), f.kdf_iter)
  let raw: Uint8Array
  try {
    raw = new Uint8Array(
      await crypto.subtle.decrypt(
        { name: 'AES-GCM', iv: fromB64(f.wrap_iv) as BufferSource },
        kek,
        fromB64(f.wrapped_key) as BufferSource,
      ),
    )
  } catch {
    return null
  }
  const salt = crypto.getRandomValues(new Uint8Array(16))
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const newKek = await deriveKek(newPassword, salt, ITERATIONS)
  const wrapped = new Uint8Array(
    await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, newKek, raw as BufferSource),
  )
  raw.fill(0)
  return {
    kdf_salt: toB64(salt),
    kdf_iter: ITERATIONS,
    wrapped_key: toB64(wrapped),
    wrap_iv: toB64(iv),
  }
}

/** Метаданные файла (имя и тип) шифруются ключом папки — сервер их не видит. */
export async function sealMeta(fk: CryptoKey, meta: FileMeta): Promise<string> {
  return seal(fk, new TextEncoder().encode(JSON.stringify(meta)))
}

export async function openMeta(fk: CryptoKey, envelope: string): Promise<FileMeta | null> {
  const raw = await open(fk, envelope)
  if (!raw) return null
  try {
    return JSON.parse(new TextDecoder().decode(raw)) as FileMeta
  } catch {
    return null
  }
}

/** Переобернуть ключ содержимого под другую папку (перенос файла). */
export async function rewrapContentKey(
  from: CryptoKey,
  to: CryptoKey,
  keyEnv: string,
): Promise<string | null> {
  const raw = await open(from, keyEnv)
  if (!raw) return null
  return seal(to, raw as BufferSource)
}

interface ContentKey {
  key: CryptoKey
  keyEnv: string
}

async function newContentKey(fk: CryptoKey): Promise<ContentKey> {
  const raw = crypto.getRandomValues(new Uint8Array(32))
  const key = await crypto.subtle.importKey('raw', raw as BufferSource, 'AES-GCM', false, [
    'encrypt',
    'decrypt',
  ])
  const keyEnv = await seal(fk, raw as BufferSource)
  raw.fill(0)
  return { key, keyEnv }
}

async function contentKey(fk: CryptoKey, keyEnv: string): Promise<CryptoKey | null> {
  const raw = await open(fk, keyEnv)
  if (!raw) return null
  return crypto.subtle.importKey('raw', raw as BufferSource, 'AES-GCM', false, ['encrypt', 'decrypt'])
}

/** IV чанка = 8 байт базы ‖ 4 байта номера: не повторяется внутри файла. */
function chunkIV(base: Uint8Array, index: number): Uint8Array {
  const iv = new Uint8Array(12)
  iv.set(base)
  new DataView(iv.buffer).setUint32(8, index, false)
  return iv
}

export interface EncryptedFile {
  chunks: Uint8Array[]
  keyEnv: string
  metaEnv: string
  plainSize: number
  chunkSize: number
}

/**
 * Шифрование файла чанками. Первый «чанк» — заголовок формата; дальше
 * блоки по CHUNK_SIZE, каждый со своим IV и тегом.
 */
export async function encryptFile(
  file: File,
  fk: CryptoKey,
  onProgress?: (done: number, total: number) => void,
): Promise<EncryptedFile> {
  const { key, keyEnv } = await newContentKey(fk)
  const base = crypto.getRandomValues(new Uint8Array(8))
  const header = new Uint8Array(HEADER_LEN)
  header.set(MAGIC)
  header[4] = VERSION
  new DataView(header.buffer).setUint32(5, CHUNK_SIZE, false)
  header.set(base, 9)

  const chunks: Uint8Array[] = [header]
  for (let offset = 0, i = 0; offset < file.size; offset += CHUNK_SIZE, i++) {
    const slice = await file.slice(offset, offset + CHUNK_SIZE).arrayBuffer()
    const ct = await crypto.subtle.encrypt(
      { name: 'AES-GCM', iv: chunkIV(base, i) as BufferSource },
      key,
      slice,
    )
    chunks.push(new Uint8Array(ct))
    onProgress?.(Math.min(offset + CHUNK_SIZE, file.size), file.size)
  }
  const metaEnv = await sealMeta(fk, { name: file.name, type: file.type, size: file.size })
  return { chunks, keyEnv, metaEnv, plainSize: file.size, chunkSize: CHUNK_SIZE }
}

/**
 * Превью шифруется тем же ключом содержимого и хранится сырыми байтами
 * [12 IV][шифротекст] — без base64 внутри base64: на сервер оно и так уедет
 * закодированным один раз.
 */
export async function encryptThumb(
  fk: CryptoKey,
  keyEnv: string,
  blob: Blob,
): Promise<Uint8Array | null> {
  const key = await contentKey(fk, keyEnv)
  if (!key) return null
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const ct = new Uint8Array(
    await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, key, await blob.arrayBuffer()),
  )
  const out = new Uint8Array(iv.length + ct.length)
  out.set(iv)
  out.set(ct, iv.length)
  return out
}

export async function decryptThumb(
  fk: CryptoKey,
  keyEnv: string,
  bytes: Uint8Array,
): Promise<Blob | null> {
  const key = await contentKey(fk, keyEnv)
  if (!key || bytes.length < 13) return null
  try {
    const plain = await crypto.subtle.decrypt(
      { name: 'AES-GCM', iv: bytes.subarray(0, 12) as BufferSource },
      key,
      bytes.subarray(12) as BufferSource,
    )
    return new Blob([plain], { type: 'image/webp' })
  } catch {
    return null
  }
}

/**
 * Расшифровка целого файла из шифробайтов. Собираем Blob из кусков, а не
 * один большой массив: браузер такие части умеет держать вне JS-кучи.
 */
export async function decryptFile(
  data: Uint8Array,
  fk: CryptoKey,
  keyEnv: string,
  meta: FileMeta,
): Promise<Blob | null> {
  const key = await contentKey(fk, keyEnv)
  if (!key) return null
  return decryptWithKey(data, key, meta.type)
}

/** То же, но ключ содержимого уже развёрнут: страница временной ссылки
 *  получает его из своего конверта, ключа папки у неё нет. */
export async function decryptWithKey(
  data: Uint8Array,
  key: CryptoKey,
  type: string,
): Promise<Blob | null> {
  if (data.length < HEADER_LEN || MAGIC.some((b, i) => data[i] !== b)) return null
  const view = new DataView(data.buffer, data.byteOffset)
  const chunkSize = view.getUint32(5, false)
  const base = data.subarray(9, 17)

  const parts: BlobPart[] = []
  let offset = HEADER_LEN
  for (let i = 0; offset < data.length; i++) {
    const len = Math.min(chunkSize + TAG_LEN, data.length - offset)
    try {
      const plain = await crypto.subtle.decrypt(
        { name: 'AES-GCM', iv: chunkIV(base, i) as BufferSource },
        key,
        data.subarray(offset, offset + len) as BufferSource,
      )
      parts.push(plain)
    } catch {
      return null
    }
    offset += len
  }
  return new Blob(parts, { type: type || 'application/octet-stream' })
}

// --- временные ссылки ---

export interface LinkEnvelopes {
  kdf_salt: string
  kdf_iter: number
  key_env: string
  meta_env: string
}

/**
 * Конверты для временной ссылки: ключ содержимого и метаданные заворачиваются
 * под ОТДЕЛЬНЫЙ пароль ссылки со своей солью. Ключ папки в ссылку не уходит —
 * утёкшая ссылка компрометирует максимум один файл, и то вместе с паролем.
 */
export async function createLinkEnvelopes(
  fk: CryptoKey,
  keyEnv: string,
  meta: FileMeta,
  password: string,
): Promise<LinkEnvelopes | null> {
  const raw = await open(fk, keyEnv)
  if (!raw) return null
  const salt = crypto.getRandomValues(new Uint8Array(16))
  const kek = await deriveKek(password, salt, ITERATIONS)
  const envelopes = {
    kdf_salt: toB64(salt),
    kdf_iter: ITERATIONS,
    key_env: await seal(kek, raw as BufferSource),
    // имя и тип — тем же ключом: расшифровать meta_env папки получателю нечем
    meta_env: await seal(kek, new TextEncoder().encode(JSON.stringify(meta))),
  }
  raw.fill(0)
  return envelopes
}

/** Открыть конверты ссылки паролем. null — пароль не подошёл. */
export async function openLink(
  password: string,
  env: LinkEnvelopes,
): Promise<{ key: CryptoKey; meta: FileMeta } | null> {
  const kek = await deriveKek(password, fromB64(env.kdf_salt), env.kdf_iter)
  const raw = await open(kek, env.key_env)
  const metaRaw = await open(kek, env.meta_env)
  if (!raw || !metaRaw) return null
  try {
    const key = await crypto.subtle.importKey('raw', raw as BufferSource, 'AES-GCM', false, [
      'encrypt',
      'decrypt',
    ])
    raw.fill(0)
    return { key, meta: JSON.parse(new TextDecoder().decode(metaRaw)) as FileMeta }
  } catch {
    return null
  }
}
