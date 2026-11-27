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

// Запросы по одному приложению идут строго по очереди: два подряд свёрнутых
// блока улетали двумя PUT сразу, и сервер мог применить их в обратном порядке
// — второй блок «разворачивался» сам после перезагрузки.
const pending = new Map<CollapsedApp, number[]>()
const inflight = new Set<CollapsedApp>()

/** Сохранение fire-and-forget: сеть не должна тормозить сворачивание. */
export function saveCollapsed(app: CollapsedApp, ids: Set<number>): void {
  pending.set(app, [...ids])
  flush(app)
}

function flush(app: CollapsedApp): void {
  if (inflight.has(app)) return
  const ids = pending.get(app)
  if (!ids) return
  pending.delete(app)
  inflight.add(app)
  api
    .put('/settings/collapsed', { app, ids })
    .catch(() => {})
    .finally(() => {
      inflight.delete(app)
      flush(app) // пока ждали ответ, состояние могло измениться ещё раз
    })
}
