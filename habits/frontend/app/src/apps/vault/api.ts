import { api, apiAuthHeader, apiBase, ApiError } from '../../shared/api/client'
import type { AccessEntry, ShareUser, VaultFile, VaultFolder, VaultLink, VaultQuota } from './types'
import type { LinkEnvelopes } from './crypto'

export function fetchVault() {
  return api.get<{ folders: VaultFolder[]; files: VaultFile[]; quota: VaultQuota }>('/vault')
}

export function createFolder(body: {
  parent_id: number | null
  name: string
  hint: string
  thumbs: boolean
  hide_children: boolean
  auto_delete_days: number
  kdf_salt: string
  kdf_iter: number
  wrapped_key: string
  wrap_iv: string
}) {
  return api.post<{ folder: VaultFolder }>('/vault/folders', body)
}

export function updateFolder(
  id: number,
  patch: {
    name?: string
    hint?: string
    kdf_salt?: string
    kdf_iter?: number
    wrapped_key?: string
    wrap_iv?: string
    hide_children?: boolean
    auto_delete_days?: number
  },
) {
  return api.patch<{ folder: VaultFolder }>(`/vault/folders/${id}`, patch)
}

export function deleteFolder(id: number) {
  return api.delete(`/vault/folders/${id}`)
}

export function initUpload(body: { folder_id: number; plain_size: number; chunk_size: number }) {
  return api.post<{ upload_id: string }>('/vault/uploads', body)
}

/** Чанк уходит сырыми байтами: base64 раздул бы трафик на треть. */
export async function uploadChunk(uploadId: string, chunk: Uint8Array): Promise<void> {
  const res = await fetch(`${apiBase()}/vault/uploads/${uploadId}/chunk`, {
    method: 'POST',
    headers: { Authorization: apiAuthHeader(), 'Content-Type': 'application/octet-stream' },
    body: chunk as BodyInit,
  })
  if (!res.ok) {
    const data = await res.json().catch(() => null)
    throw new ApiError(res.status, data?.error?.code ?? 'unknown', data?.error?.message ?? res.statusText)
  }
}

/** Шифроблоб файла целиком (Range сервер поддерживает, но нам нужен весь). */
export async function fetchBlob(id: number, thumb = false): Promise<Uint8Array> {
  const res = await fetch(`${apiBase()}/vault/files/${id}/${thumb ? 'thumb' : 'blob'}`, {
    headers: { Authorization: apiAuthHeader() },
  })
  if (!res.ok) throw new ApiError(res.status, 'fetch_failed', 'не удалось скачать файл')
  return new Uint8Array(await res.arrayBuffer())
}

export function finishUpload(
  uploadId: string,
  body: { key_env: string; meta_env: string; thumb?: string },
) {
  return api.post<{ file: VaultFile }>(`/vault/uploads/${uploadId}/finish`, body)
}

export function updateFile(
  id: number,
  patch: { meta_env?: string; key_env?: string; folder_id?: number },
) {
  return api.patch<{ file: VaultFile }>(`/vault/files/${id}`, patch)
}

export function deleteFiles(ids: number[]) {
  return api.post<{ deleted: number }>('/vault/files/delete', { ids })
}

export function shareTarget(kind: 'folder' | 'file', id: number, to: string) {
  return api.post<{ queued: boolean; name: string; shared_with: ShareUser }>(
    `/vault/${kind}s/${id}/share`,
    { to },
  )
}

export function fetchShares(kind: 'folder' | 'file', id: number) {
  return api.get<{ users: ShareUser[] }>(`/vault/${kind}s/${id}/shares`)
}

export function revokeShare(kind: 'folder' | 'file', id: number, userId: number) {
  return api.delete(`/vault/${kind}s/${id}/share/${userId}`)
}

export function copyFile(id: number, body: { folder_id: number; key_env: string; meta_env: string }) {
  return api.post<{ file: VaultFile }>(`/vault/files/${id}/copy`, body)
}

export function setExpiry(ids: number[], days: number) {
  return api.post<{ updated: number }>('/vault/files/expiry', { ids, days })
}

export function fetchAccessLog(id: number) {
  return api.get<{ entries: AccessEntry[] }>(`/vault/files/${id}/access`)
}

export function createLink(
  id: number,
  body: LinkEnvelopes & { ttl_minutes: number; max_views: number },
) {
  return api.post<{ link: VaultLink; path: string }>(`/vault/files/${id}/links`, body)
}

export function fetchLinks(id: number) {
  return api.get<{ links: VaultLink[] }>(`/vault/files/${id}/links`)
}

export function revokeLink(id: number) {
  return api.delete(`/vault/links/${id}`)
}

/** Сколько байт уже принято: по этому числу возобновляется прерванная загрузка. */
export function uploadStatus(uploadId: string) {
  return api.get<{ written: number }>(`/vault/uploads/${uploadId}`)
}

// --- публичная страница временной ссылки (вне авторизации) ---

export function fetchPublicLink(token: string) {
  return api.get<LinkEnvelopes & { plain_size: number; chunk_size: number; expires_at: string }>(
    `/vault/public/links/${token}`,
  )
}

export async function fetchPublicLinkBlob(token: string): Promise<Uint8Array> {
  const res = await fetch(`${apiBase()}/vault/public/links/${token}/blob`)
  if (!res.ok) throw new ApiError(res.status, 'fetch_failed', 'ссылка не найдена или истекла')
  return new Uint8Array(await res.arrayBuffer())
}
