export type StatusKind = 'open' | 'done' | 'archived'
export type RepeatKind = 'daily' | 'weekly' | 'monthly' | 'interval'

export interface CategoryStatus {
  name: string
  kind: StatusKind
}

export const DEFAULT_STATUSES: CategoryStatus[] = [
  { name: 'Открыта', kind: 'open' },
  { name: 'Выполнена', kind: 'done' },
  { name: 'Архив', kind: 'archived' },
]

export interface TaskDefaults {
  priority?: number
  remind?: boolean
  remind_before_min?: number
  repeat_kind?: RepeatKind
  repeat_param?: number
}

// wire-имена (project/projects/project_id) сохранены для совместимости с API
export interface TaskCategory {
  id: number
  name: string
  color: string
  position: number
  statuses: CategoryStatus[] | null
  defaults: TaskDefaults | null
  owner_id: number
  mine: boolean
  shared: boolean
}

export interface Task {
  id: number
  user_id: number
  project_id: number | null
  title: string
  note: string
  status: string
  status_kind: StatusKind
  priority: number
  due_date: string | null
  due_time: string | null
  remind: boolean
  remind_before_min: number
  repeat_kind: RepeatKind | null
  repeat_param: number | null
  assignee_id: number | null
  tz_offset_minutes: number
  position: number
  completed_at: string | null
  checklist_total: number
  checklist_done: number
  mine: boolean
}

export interface ChecklistItem {
  id: number
  name: string
  done: boolean
  position: number
}

export interface ShareUser {
  id: number
  username: string
  first_name: string
}

export function statusesOf(c: TaskCategory | null | undefined): CategoryStatus[] {
  return c?.statuses?.length ? c.statuses : DEFAULT_STATUSES
}

export function currentTzOffset(): number {
  return -new Date().getTimezoneOffset()
}

export function todayISO(): string {
  const d = new Date()
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  return `${d.getFullYear()}-${mm}-${dd}`
}

/** Просрочена ли задача (дата в прошлом, либо сегодня с прошедшим временем). */
export function isOverdue(t: Task, now = new Date()): boolean {
  if (!t.due_date || t.status_kind !== 'open') return false
  const today = todayISO()
  if (t.due_date < today) return true
  if (t.due_date > today) return false
  if (!t.due_time) return false
  const [hh, mm] = t.due_time.split(':').map(Number)
  return now.getHours() * 60 + now.getMinutes() >= hh * 60 + mm
}

export const PRIORITY_COLORS = ['transparent', '#8bc34a', '#ff9800', '#f44336']
export const PRIORITY_NAMES = ['Нет', 'Низкий', 'Средний', 'Высокий']

export const REPEAT_NAMES: Record<RepeatKind, string> = {
  daily: 'Каждый день',
  weekly: 'Каждую неделю',
  monthly: 'Каждый месяц',
  interval: 'Через N дней',
}

/** «сегодня», «завтра», «12.05» + время. */
export function fmtDue(t: Task): string {
  if (!t.due_date) return ''
  const today = todayISO()
  let s: string
  if (t.due_date === today) {
    s = 'сегодня'
  } else {
    const [y, m, d] = t.due_date.split('-')
    const tomorrow = new Date()
    tomorrow.setDate(tomorrow.getDate() + 1)
    const tmm = String(tomorrow.getMonth() + 1).padStart(2, '0')
    const tdd = String(tomorrow.getDate()).padStart(2, '0')
    if (t.due_date === `${tomorrow.getFullYear()}-${tmm}-${tdd}`) s = 'завтра'
    else s = `${d}.${m}${y !== String(new Date().getFullYear()) ? '.' + y : ''}`
  }
  if (t.due_time) s += ' ' + t.due_time
  return s
}

/** Заголовок группы дат в «Планах». */
export function fmtDateGroup(date: string): string {
  const today = todayISO()
  if (date < today) return 'Просрочено'
  if (date === today) return 'Сегодня'
  const d = new Date(date + 'T00:00:00')
  const tomorrow = new Date()
  tomorrow.setDate(tomorrow.getDate() + 1)
  if (d.toDateString() === tomorrow.toDateString()) return 'Завтра'
  return d.toLocaleDateString('ru-RU', { weekday: 'short', day: 'numeric', month: 'long' })
}
