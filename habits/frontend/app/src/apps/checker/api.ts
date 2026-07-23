import { api } from '../../shared/api/client'
import type { CheckGroup, CheckItem } from './types'

export function fetchGroups() {
  return api.get<{ groups: CheckGroup[] }>('/checker/groups')
}

export function createGroup(name: string, parentId?: number) {
  return api.post<{ group: CheckGroup }>('/checker/groups', {
    name,
    parent_id: parentId ?? null,
  })
}

/** Токен и ссылка-приглашение t.me/…?startapp=chg_<token> для группы. */
export function groupShareToken(id: number) {
  return api.post<{ token: string; link: string }>(`/checker/groups/${id}/share-token`)
}

/** Отправка копии группы пользователю приложения (точный id или @логин). */
export function sendGroup(id: number, to: string) {
  return api.post<{ sent_to: { id: number; username: string; first_name: string } }>(
    `/checker/groups/${id}/send`,
    { to },
  )
}

/** Импорт группы из дерева (текст/JSON распарсены на клиенте). */
export function importGroup(tree: import('./transfer').ExportGroup) {
  return api.post<{ group: CheckGroup }>('/checker/groups/import', tree)
}

export function updateGroup(id: number, patch: { name?: string; hide_done?: boolean }) {
  return api.patch<{ group: CheckGroup }>(`/checker/groups/${id}`, patch)
}

export function deleteGroup(id: number) {
  return api.delete<void>(`/checker/groups/${id}`)
}

export function createItem(groupId: number, name: string) {
  return api.post<{ item: CheckItem }>(`/checker/groups/${groupId}/items`, { name })
}

export function updateItem(id: number, patch: { name?: string; done?: boolean; group_id?: number }) {
  return api.patch<{ item: CheckItem }>(`/checker/items/${id}`, patch)
}

export function deleteItem(id: number) {
  return api.delete<void>(`/checker/items/${id}`)
}

// --- шаблоны (многоразовые списки) ---

export interface CheckTemplate {
  id: number
  name: string
  share_token?: string
  items: string[]
}

export function fetchTemplates() {
  return api.get<{ templates: CheckTemplate[] }>('/checker/templates')
}

export function createTemplate(name: string, items: string[]) {
  return api.post<{ template: CheckTemplate }>('/checker/templates', { name, items })
}

export function updateTemplate(id: number, name: string, items: string[]) {
  return api.put<{ template: CheckTemplate }>(`/checker/templates/${id}`, { name, items })
}

export function deleteTemplate(id: number) {
  return api.delete<void>(`/checker/templates/${id}`)
}

/** Разворачивает шаблон в новую группу. */
export function startTemplate(id: number) {
  return api.post<{ group: CheckGroup }>(`/checker/templates/${id}/start`)
}

/** Токен и ссылка-приглашение t.me/…?startapp=chk_<token>. */
export function shareToken(id: number) {
  return api.post<{ token: string; link: string }>(`/checker/templates/${id}/share-token`)
}

/** Отправка шаблона пользователю приложения (точный id или @логин). */
export function sendTemplate(id: number, to: string) {
  return api.post<{ sent_to: { id: number; username: string; first_name: string } }>(
    `/checker/templates/${id}/send`,
    { to },
  )
}
