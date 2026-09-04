// Свёрнутость групп Checker/Tracker хранится на сервере
// (localStorage в Telegram-webview периодически очищается).
import { api } from './api/client'

// food_plan_open — исключение: хранит РАСКРЫТЫЕ дни плана (по умолчанию всё
// свёрнуто), ключ дня — planId * 100 + dayIndex.
type CollapsedApp =
  | 'checker'
  | 'tracker'
  | 'tasks'
  | 'reminders'
  | 'projects'
  | 'projects_cat'
  | 'food_plan_open'
  // settings: id 1 — блок «Оформление» (свёрнут, если id в списке)
  | 'settings'
  // main: id 1 — блок плиток на главной
  | 'main'

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
