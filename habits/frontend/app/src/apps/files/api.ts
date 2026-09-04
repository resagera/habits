import { api, apiAuthHeader, apiBase } from '../../shared/api/client'
import type { FileEntry, FileMachine } from './types'

export function fetchMachines() {
  return api.get<{ machines: FileMachine[] }>('/files/machines')
}

export function createMachine(name: string) {
  return api.post<{ machine: FileMachine }>('/files/machines', { name })
}

export function renameMachine(id: number, name: string) {
  return api.patch<{ machine: FileMachine }>(`/files/machines/${id}`, { name })
}

export function deleteMachine(id: number) {
  return api.delete<void>(`/files/machines/${id}`)
}

export function listDir(id: number, path: string) {
  return api.get<{ entries: FileEntry[]; top?: boolean }>(
    `/files/machines/${id}/list?path=${encodeURIComponent(path)}`,
  )
}

export function mkdir(id: number, path: string) {
  return api.post<void>(`/files/machines/${id}/mkdir`, { path })
}

export function renameEntry(id: number, path: string, to: string) {
  return api.post<void>(`/files/machines/${id}/rename`, { path, to })
}

export function removeEntry(id: number, path: string, is_dir: boolean) {
  return api.post<void>(`/files/machines/${id}/remove`, { path, is_dir })
}

/** Пропуск на стриминг файла и готовый URL для <img>/<video>/<a>. */
export async function streamUrl(id: number, path: string, download = false): Promise<string> {
  const { ticket } = await api.post<{ ticket: string }>(`/files/machines/${id}/ticket`, { path, download })
  return apiBase() + '/files/stream/' + ticket
}

/** Загрузка файла в rw-папку сырым телом (бэкенд пишет чанками). */
export async function uploadFile(id: number, dir: string, file: File): Promise<void> {
  const url = `${apiBase()}/files/machines/${id}/upload?path=${encodeURIComponent(dir)}&name=${encodeURIComponent(file.name)}`
  const res = await fetch(url, {
    method: 'POST',
    headers: { Authorization: apiAuthHeader() },
    body: file,
  })
  if (!res.ok) {
    const data = await res.json().catch(() => null)
    throw new Error(data?.error?.message ?? 'upload failed')
  }
}
