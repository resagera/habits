import { api, apiAuthHeader, apiBase } from '../../shared/api/client'
import type {
  BlockKind,
  HistoryEntry,
  Project,
  ProjectBlock,
  ProjectCategory,
  ShareUser,
} from './types'

export function fetchProjects() {
  return api.get<{ categories: ProjectCategory[]; projects: Project[]; types: string[] }>(
    '/projects',
  )
}

export function createProject(fields: Record<string, unknown>) {
  return api.post<{ project: Project }>('/projects', fields)
}

export function updateProject(id: number, patch: Record<string, unknown>) {
  return api.patch<{ project: Project }>(`/projects/${id}`, patch)
}

export function deleteProject(id: number) {
  return api.delete<void>(`/projects/${id}`)
}

export function fetchProject(id: number) {
  return api.get<{ project: Project; blocks: ProjectBlock[]; members: ShareUser[] }>(
    `/projects/${id}`,
  )
}

export function fetchHistory(id: number) {
  return api.get<{ history: HistoryEntry[] }>(`/projects/${id}/history`)
}

export function createCategory(name: string) {
  return api.post<{ category: ProjectCategory }>('/projects/categories', { name })
}

export function renameCategory(id: number, name: string) {
  return api.patch<void>(`/projects/categories/${id}`, { name })
}

export function deleteCategory(id: number) {
  return api.delete<void>(`/projects/categories/${id}`)
}

export function createBlock(
  projectId: number,
  fields: {
    kind: BlockKind
    content?: Record<string, unknown>
    bg?: string
    collapsed?: boolean
    position?: number
    create_name?: string
  },
) {
  return api.post<{ block: ProjectBlock }>(`/projects/${projectId}/blocks`, fields)
}

export function updateBlock(id: number, patch: Record<string, unknown>) {
  return api.patch<{ block: ProjectBlock }>(`/projects/blocks/${id}`, patch)
}

export function deleteBlock(id: number) {
  return api.delete<void>(`/projects/blocks/${id}`)
}

export interface Uploaded {
  url: string
  name: string
  size: number
  image: boolean
}

/** Загрузка файла/картинки (multipart) — api-клиент шлёт JSON, поэтому raw fetch. */
export async function uploadFile(projectId: number, file: File): Promise<Uploaded> {
  const form = new FormData()
  form.append('file', file)
  const res = await fetch(`${apiBase()}/projects/${projectId}/upload`, {
    method: 'POST',
    headers: { Authorization: apiAuthHeader() },
    body: form,
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new Error(data?.error?.message ?? `upload failed: ${res.status}`)
  return data as Uploaded
}

export function shareProject(id: number, to: string) {
  return api.post<{ shared_with: ShareUser; queued: boolean }>(`/projects/${id}/share`, { to })
}

export function fetchShares(id: number) {
  return api.get<{ users: ShareUser[] }>(`/projects/${id}/shares`)
}

export function revokeShare(id: number, userId: number) {
  return api.delete<void>(`/projects/${id}/shares/${userId}`)
}

// --- списки существующих сущностей для ref-блоков (родные API страниц) ---

export function fetchCheckerGroups() {
  return api.get<{ groups: { id: number; parent_id: number | null; name: string }[] }>(
    '/checker/groups',
  )
}

export function fetchArticles() {
  return api.get<{ articles: { id: number; title: string }[] }>('/articles/tree')
}

export function fetchTasksAll() {
  return api.get<{
    projects: { id: number; name: string }[]
    tasks: { id: number; title: string; project_id: number | null }[]
  }>('/tasks')
}
