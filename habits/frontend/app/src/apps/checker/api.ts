import { api } from '../../shared/api/client'
import type { ExportItem, ExportSubgroup } from './transfer'
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

export function updateGroup(
  id: number,
  patch: { name?: string; hide_done?: boolean; progress_mode?: boolean },
) {
  return api.patch<{ group: CheckGroup }>(`/checker/groups/${id}`, patch)
}

/** Сменить родителя группы (parentId=null — в верхний уровень). */
export function moveGroup(id: number, parentId: number | null) {
  return api.post<{ group: CheckGroup }>(`/checker/groups/${id}/move`, { parent_id: parentId })
}

/** Порядок групп-соседей одного родителя. */
export function reorderGroups(parentId: number | null, orderedIds: number[]) {
  return api.post<void>(`/checker/groups/reorder`, { parent_id: parentId, ordered_ids: orderedIds })
}

/** Дублировать группу со всем поддеревом. */
export function duplicateGroup(id: number) {
  return api.post<{ group: CheckGroup }>(`/checker/groups/${id}/duplicate`)
}

interface ShareUser {
  id: number
  username: string
  first_name: string
}

/** Открыть совместный доступ к списку пользователю (по id/@логину). */
export function shareGroupAccess(id: number, to: string) {
  return api.post<{ shared_with: ShareUser; queued: boolean }>(`/checker/groups/${id}/share-access`, { to })
}

/** Участники совместного доступа (для владельца). */
export function listGroupShares(id: number) {
  return api.get<{ users: ShareUser[] }>(`/checker/groups/${id}/shares`)
}

/** Владелец отзывает доступ у участника. */
export function revokeGroupShare(id: number, userId: number) {
  return api.delete<void>(`/checker/groups/${id}/shares/${userId}`)
}

/** Участник убирает у себя доступ к общему списку. */
export function leaveSharedGroup(id: number) {
  return api.delete<void>(`/checker/shared/${id}`)
}

export interface HistoryEntry {
  user_id: number
  user_name: string
  action: string
  at: string
}

/** История изменений списка (последние записи). */
export function groupHistory(id: number) {
  return api.get<{ history: HistoryEntry[] }>(`/checker/groups/${id}/history`)
}

// --- повторяющиеся списки ---

export interface Recurrence {
  period: string
  minute: number
  dow: number
  dom: number
  tz_off: number
}

/** Задать расписание сброса списка (владелец). */
export function setRecurring(id: number, r: Recurrence) {
  return api.patch<{ group: CheckGroup }>(`/checker/groups/${id}/recurring`, r)
}

/** Ручной сброс списка сейчас (снимок + снятие отметок). */
export function resetNow(id: number) {
  return api.post<void>(`/checker/groups/${id}/reset`)
}

export interface SnapshotDay {
  day: string
  done: number
  total: number
}

/** Дни со снимками (для календаря). */
export function listSnapshots(id: number) {
  return api.get<{ days: SnapshotDay[] }>(`/checker/groups/${id}/snapshots`)
}

export interface SnapshotNode {
  name: string
  items: { name: string; done: boolean }[]
  subgroups: SnapshotNode[]
}

/** Снимок конкретного дня (дерево). */
export function getSnapshot(id: number, day: string) {
  return api.get<{ data: SnapshotNode }>(`/checker/groups/${id}/snapshots/${day}`)
}

/** Дедлайн/напоминание у пункта (remindAt=null — снять). */
export function setItemReminder(id: number, remindAt: string | null) {
  return api.post<{ remind_at: string | null }>(`/checker/items/${id}/reminder`, { remind_at: remindAt })
}

/** Напоминание о списке (владелец; remindAt=null — снять). */
export function setGroupReminder(id: number, remindAt: string | null) {
  return api.post<{ remind_at: string | null }>(`/checker/groups/${id}/reminder`, { remind_at: remindAt })
}

/** Мягкое удаление группы (в корзину). Возвращает имя (для «Отменить»). */
export function deleteGroup(id: number) {
  return api.delete<{ name: string }>(`/checker/groups/${id}`)
}

/** Восстановить группу из корзины. */
export function restoreGroup(id: number) {
  return api.post<void>(`/checker/groups/${id}/restore`)
}

export interface TrashGroup {
  id: number
  name: string
  deleted_at: string
  groups: number
  items: number
}

/** Корзина: список удалённых групп-корней + срок хранения. */
export function listTrash() {
  return api.get<{ trashed: TrashGroup[]; retention_days: number }>(`/checker/trash`)
}

/** Удалить группу из корзины навсегда. */
export function purgeTrashGroup(id: number) {
  return api.delete<void>(`/checker/trash/${id}`)
}

/** Очистить корзину. */
export function emptyTrash() {
  return api.delete<void>(`/checker/trash`)
}

/** Задать срок хранения корзины (1..365 дней). */
export function setTrashDays(days: number) {
  return api.put<{ retention_days: number }>(`/checker/trash-days`, { days })
}

export function createItem(groupId: number, name: string) {
  return api.post<{ item: CheckItem }>(`/checker/groups/${groupId}/items`, { name })
}

export function updateItem(
  id: number,
  patch: {
    name?: string
    done?: boolean
    note?: string
    label?: string
    group_id?: number
    in_progress?: boolean
  },
) {
  return api.patch<{ item: CheckItem }>(`/checker/items/${id}`, patch)
}

/** Массовое действие над пунктами группы: check_all / uncheck_all / delete_done. */
export function bulkGroupItems(groupId: number, action: 'check_all' | 'uncheck_all' | 'delete_done') {
  return api.post<{ items: CheckItem[] }>(`/checker/groups/${groupId}/items/bulk`, { action })
}

export function deleteItem(id: number) {
  return api.delete<void>(`/checker/items/${id}`)
}

// --- шаблоны (многоразовые списки) ---

// Тело шаблона — дерево (пункты + подгруппы), как у экспорта группы.
export interface TemplateTree {
  items: ExportItem[]
  subgroups: ExportSubgroup[]
}

export interface CheckTemplate extends TemplateTree {
  id: number
  name: string
  share_token?: string
}

export function fetchTemplates() {
  return api.get<{ templates: CheckTemplate[] }>('/checker/templates')
}

export function createTemplate(name: string, tree: TemplateTree) {
  return api.post<{ template: CheckTemplate }>('/checker/templates', { name, ...tree })
}

export function updateTemplate(id: number, name: string, tree: TemplateTree) {
  return api.put<{ template: CheckTemplate }>(`/checker/templates/${id}`, { name, ...tree })
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
