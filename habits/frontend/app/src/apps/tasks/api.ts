import { api } from '../../shared/api/client'
import type {
  CategoryStatus,
  ChecklistItem,
  ShareUser,
  Task,
  TaskCategory,
  TaskDefaults,
} from './types'

// в wire-формате категории по-прежнему называются project/projects — не меняем API
export function fetchAll() {
  return api.get<{ projects: TaskCategory[]; tasks: Task[] }>('/tasks')
}

export function fetchDone(limit = 200) {
  return api.get<{ tasks: Task[] }>(`/tasks/done?limit=${limit}`)
}

export function fetchSummary(tz: number) {
  return api.get<{ today: number; overdue: number }>(`/tasks/summary?tz=${tz}`)
}

export function createTask(fields: Partial<Task> & { title: string }) {
  return api.post<{ task: Task }>('/tasks', fields)
}

export function fetchTask(id: number) {
  return api.get<{ task: Task; checklist: ChecklistItem[] }>(`/tasks/${id}`)
}

export function updateTask(id: number, patch: Record<string, unknown>) {
  return api.patch<{ task: Task }>(`/tasks/${id}`, patch)
}

export function deleteTask(id: number) {
  return api.delete<void>(`/tasks/${id}`)
}

export function completeTask(id: number) {
  return api.post<{ task: Task; next?: Task }>(`/tasks/${id}/complete`, {})
}

export function addChecklistItem(taskId: number, name: string) {
  return api.post<{ item: ChecklistItem }>(`/tasks/${taskId}/checklist`, { name })
}

export function updateChecklistItem(id: number, patch: { name?: string; done?: boolean }) {
  return api.patch<{ item: ChecklistItem }>(`/tasks/checklist/${id}`, patch)
}

export function deleteChecklistItem(id: number) {
  return api.delete<void>(`/tasks/checklist/${id}`)
}

export function createCategory(name: string, color?: string) {
  return api.post<{ project: TaskCategory }>('/tasks/projects', { name, color })
}

export function updateCategory(
  id: number,
  patch: {
    name?: string
    color?: string
    position?: number
    statuses?: CategoryStatus[] | null
    defaults?: TaskDefaults | null
  },
) {
  return api.patch<{ project: TaskCategory }>(`/tasks/projects/${id}`, patch)
}

export function deleteCategory(id: number) {
  return api.delete<void>(`/tasks/projects/${id}`)
}

export function shareCategory(id: number, to: string) {
  return api.post<{ shared_with: ShareUser }>(`/tasks/projects/${id}/share`, { to })
}

export function fetchCategoryShares(id: number) {
  return api.get<{ users: ShareUser[] }>(`/tasks/projects/${id}/shares`)
}

export function revokeCategoryShare(id: number, userId: number) {
  return api.delete<void>(`/tasks/projects/${id}/shares/${userId}`)
}
