// Свёрнутость групп Checker/Tracker хранится на сервере
// (localStorage в Telegram-webview периодически очищается).
import { api } from './api/client'

type CollapsedApp = 'checker' | 'tracker' | 'tasks' | 'reminders' | 'projects' | 'projects_cat'

export async function loadCollapsed(app: CollapsedApp): Promise<Set<number>> {
  try {
    const { collapsed } = await api.get<{ collapsed: Record<string, number[]> }>('/settings/collapsed')
    return new Set(collapsed[app] ?? [])
  } catch {
    return new Set()
  }
}

/** Сохранение fire-and-forget: сеть не должна тормозить сворачивание. */
export function saveCollapsed(app: CollapsedApp, ids: Set<number>): void {
  api.put('/settings/collapsed', { app, ids: [...ids] }).catch(() => {})
}
