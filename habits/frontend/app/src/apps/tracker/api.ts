import { api } from '../../shared/api/client'
import type { Category, CategoryMarks, CategoryPatch, MarkDay, ShareUser } from './types'

export function fetchCategories() {
  return api.get<{ categories: Category[] }>('/tracker/categories')
}

export function createCategory(name: string, color?: string) {
  return api.post<{ category: Category }>('/tracker/categories', { name, color })
}

export function updateCategory(id: number, patch: CategoryPatch) {
  return api.patch<{ category: Category }>(`/tracker/categories/${id}`, patch)
}

export function deleteCategory(id: number) {
  return api.delete<void>(`/tracker/categories/${id}`)
}

export function fetchMarks(from: string, to: string, categoryId?: number) {
  const query = categoryId ? `&category_id=${categoryId}` : ''
  return api.get<{ marks: CategoryMarks[] }>(`/tracker/marks?from=${from}&to=${to}${query}`)
}

export function fetchHistory(categoryId: number) {
  return api.get<{ days: MarkDay[] }>(`/tracker/categories/${categoryId}/history`)
}

export function toggleMark(categoryId: number, day: string, color?: string, emoji?: string) {
  return api.post<{ marked: boolean }>('/tracker/marks/toggle', {
    category_id: categoryId,
    day,
    ...(color ? { color } : {}),
    ...(emoji ? { emoji } : {}),
  })
}

export function incrementMark(categoryId: number, day: string, delta: 1 | -1) {
  return api.post<{ count: number }>('/tracker/marks/increment', {
    category_id: categoryId,
    day,
    delta,
  })
}

export function shareCategory(id: number, to: string) {
  return api.post<{ shared_with: ShareUser }>(`/tracker/categories/${id}/share`, { to })
}

export function fetchShares(id: number) {
  return api.get<{ users: ShareUser[] }>(`/tracker/categories/${id}/shares`)
}

export function revokeShare(id: number, userId: number) {
  return api.delete<void>(`/tracker/categories/${id}/shares/${userId}`)
}
