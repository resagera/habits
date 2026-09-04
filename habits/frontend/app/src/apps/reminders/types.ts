export type ReminderKind = 'once' | 'daily' | 'weekly' | 'monthly' | 'yearly' | 'interval' | 'tracker'

export interface Reminder {
  id: number
  title: string
  note: string
  kind: ReminderKind
  at?: string
  time_of_day?: string
  days_mask: number
  day_of_month?: number
  month?: number
  interval_minutes?: number
  category_id?: number
  group_id?: number
  tz_offset_minutes: number
  enabled: boolean
  next_fire_at?: string
  last_fired_at?: string
}

/** Своя категория напоминаний (группа в списке). */
export interface ReminderCategory {
  id: number
  name: string
  position: number
}

/** Тело create/update — то же, что Reminder, без служебных полей. */
export type ReminderDraft = Omit<Reminder, 'id' | 'next_fire_at' | 'last_fired_at'>

export const KIND_LABELS: Record<ReminderKind, string> = {
  once: 'Один раз',
  daily: 'Каждый день',
  weekly: 'По дням недели',
  monthly: 'Ежемесячно',
  yearly: 'Ежегодно (дата)',
  interval: 'Через интервал',
  tracker: 'Привычка из Tracker',
}

export const MONTHS = [
  'января', 'февраля', 'марта', 'апреля', 'мая', 'июня',
  'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря',
]

/** Единицы интервала: значение в минутах (месяц ≈ 30 дней). */
export const INTERVAL_UNITS: { key: string; label: string; minutes: number }[] = [
  { key: 'min', label: 'минут', minutes: 1 },
  { key: 'hour', label: 'часов', minutes: 60 },
  { key: 'day', label: 'дней', minutes: 1440 },
  { key: 'week', label: 'недель', minutes: 10080 },
  { key: 'month', label: 'месяцев (30 дн)', minutes: 43200 },
]

/** Разложить interval_minutes в наибольшую целую единицу. */
export function splitInterval(min: number): { value: number; unit: string } {
  for (let i = INTERVAL_UNITS.length - 1; i >= 0; i--) {
    const u = INTERVAL_UNITS[i]
    if (min % u.minutes === 0 && min >= u.minutes) return { value: min / u.minutes, unit: u.key }
  }
  return { value: min, unit: 'min' }
}

export const WEEKDAYS = ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс']

export function maskToText(mask: number): string {
  if (mask === 127 || mask === 0) return 'каждый день'
  return WEEKDAYS.filter((_, i) => mask & (1 << i)).join(', ')
}

/** Текущее смещение локального времени от UTC в минутах (восток — плюс). */
export function currentTzOffset(): number {
  return -new Date().getTimezoneOffset()
}

function fmtInterval(min: number): string {
  if (min % 43200 === 0) return `${min / 43200} мес`
  if (min % 10080 === 0) return `${min / 10080} нед`
  if (min % 1440 === 0) return `${min / 1440} дн`
  if (min % 60 === 0) return `${min / 60} ч`
  return `${min} мин`
}

/** Краткое описание расписания для списка. */
export function scheduleText(r: Reminder, categoryName?: string): string {
  switch (r.kind) {
    case 'once':
      return r.at ? `один раз, ${fmtDateTime(r.at)}` : 'один раз'
    case 'daily':
      return `каждый день в ${r.time_of_day}`
    case 'weekly':
      return `${maskToText(r.days_mask)} в ${r.time_of_day}`
    case 'monthly':
      return `${r.day_of_month}-го числа в ${r.time_of_day}`
    case 'yearly':
      return `каждый год ${r.day_of_month} ${MONTHS[(r.month ?? 1) - 1]} в ${r.time_of_day}`
    case 'interval':
      return `каждые ${fmtInterval(r.interval_minutes ?? 0)}`
    case 'tracker':
      return `${categoryName ? `«${categoryName}»` : 'привычка'} — в ${r.time_of_day}, если не отмечено`
  }
}

/** «сегодня 21:00», «завтра 09:00» или «18.07 10:00». */
export function fmtDateTime(iso: string): string {
  const d = new Date(iso)
  const now = new Date()
  const time = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  const dayStart = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime()
  const diffDays = Math.round((dayStart(d) - dayStart(now)) / 86_400_000)
  if (diffDays === 0) return `сегодня ${time}`
  if (diffDays === 1) return `завтра ${time}`
  const date = `${String(d.getDate()).padStart(2, '0')}.${String(d.getMonth() + 1).padStart(2, '0')}`
  return d.getFullYear() === now.getFullYear() ? `${date} ${time}` : `${date}.${d.getFullYear()} ${time}`
}
