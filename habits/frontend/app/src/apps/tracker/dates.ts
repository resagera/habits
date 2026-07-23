export const WEEKS_TO_SHOW = 8

/** Локальная дата в формате YYYY-MM-DD (как ждёт API). */
export function toISODate(d: Date): string {
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  return `${d.getFullYear()}-${mm}-${dd}`
}

/** Понедельник недели, в которую входит d. */
export function weekStart(d: Date): Date {
  const r = new Date(d)
  const day = r.getDay()
  r.setDate(r.getDate() + (day === 0 ? -6 : 1 - day))
  r.setHours(0, 0, 0, 0)
  return r
}

export function addDays(d: Date, days: number): Date {
  const r = new Date(d)
  r.setDate(r.getDate() + days)
  return r
}

/** Метка недели вида DD.MM по её первому дню. */
export function weekLabel(start: Date): string {
  return `${String(start.getDate()).padStart(2, '0')}.${String(start.getMonth() + 1).padStart(2, '0')}`
}

/** Окно из n недель, заканчивающееся текущей: массив дат-понедельников, старейшая первой. */
export function lastWeekStarts(n: number, today = new Date()): Date[] {
  const current = weekStart(today)
  const result: Date[] = []
  for (let w = n - 1; w >= 0; w--) {
    result.push(addDays(current, -7 * w))
  }
  return result
}

export interface MonthGrid {
  /** пустых ячеек перед 1-м числом (неделя с понедельника) */
  leadingEmpty: number
  daysInMonth: number
}

export function monthGrid(year: number, month: number): MonthGrid {
  const first = new Date(year, month, 1)
  return {
    leadingEmpty: (first.getDay() + 6) % 7,
    daysInMonth: new Date(year, month + 1, 0).getDate(),
  }
}
