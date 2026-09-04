import { api } from '../../shared/api/client'
import type { Reminder, ReminderCategory, ReminderDraft } from './types'

export function fetchCategories() {
  return api.get<{ categories: ReminderCategory[] }>('/reminder-categories')
}

export function createCategory(name: string) {
  return api.post<{ category: ReminderCategory }>('/reminder-categories', { name })
}

export function renameCategory(id: number, name: string) {
  return api.patch<{ category: ReminderCategory }>(`/reminder-categories/${id}`, { name })
}

export function deleteCategory(id: number) {
  return api.delete<void>(`/reminder-categories/${id}`)
}

export function fetchReminders() {
  return api.get<{ reminders: Reminder[] }>('/reminders')
}

export function fetchUpcoming(limit = 3) {
  return api.get<{ reminders: Reminder[] }>(`/reminders/upcoming?limit=${limit}`)
}

export function createReminder(draft: ReminderDraft) {
  return api.post<{ reminder: Reminder }>('/reminders', draft)
}

export function updateReminder(id: number, draft: ReminderDraft) {
  return api.put<{ reminder: Reminder }>(`/reminders/${id}`, draft)
}

export function toggleReminder(id: number, enabled: boolean) {
  return api.patch<{ reminder: Reminder }>(`/reminders/${id}/enabled`, { enabled })
}

export function deleteReminder(id: number) {
  return api.delete<void>(`/reminders/${id}`)
}

// --- шаринг и импорт категории ---

/** Токен и ссылка-приглашение t.me/…?startapp=rem_<token>. */
export function shareCategoryToken(id: number) {
  return api.post<{ token: string; link: string }>(`/reminder-categories/${id}/share-token`)
}

/** Отправка копии категории пользователю приложения (id или @логин). */
export function sendCategory(id: number, to: string) {
  return api.post<{ sent_to: { id: number; username: string; first_name: string } }>(
    `/reminder-categories/${id}/send`,
    { to },
  )
}

/** Импорт категории из JSON (напоминания-привычки пропускаются). */
export function importCategory(payload: { name: string; reminders: unknown[] }) {
  return api.post<{ category: ReminderCategory }>('/reminder-categories/import', payload)
}
