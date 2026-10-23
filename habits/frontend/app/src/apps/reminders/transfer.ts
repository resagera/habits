// Экспорт/импорт категории напоминаний (JSON — полное восстановление,
// текст — читаемая сводка). Напоминания-привычки (kind=tracker) при импорте
// пропускаются: они завязаны на Tracker получателя.
import { scheduleText, type Reminder } from './types'

export interface ReminderExportItem {
  title: string
  note: string
  kind: Reminder['kind']
  at?: string
  time_of_day?: string
  days_mask: number
  day_of_month?: number
  month?: number
  interval_minutes?: number
  category_id?: number
  tz_offset_minutes: number
  enabled: boolean
}

export interface ReminderExport {
  name: string
  reminders: ReminderExportItem[]
}

export function buildExport(name: string, reminders: Reminder[]): ReminderExport {
  return {
    name,
    reminders: reminders.map((r) => ({
      title: r.title,
      note: r.note,
      kind: r.kind,
      at: r.at,
      time_of_day: r.time_of_day,
      days_mask: r.days_mask,
      day_of_month: r.day_of_month,
      month: r.month,
      interval_minutes: r.interval_minutes,
      category_id: r.category_id,
      tz_offset_minutes: r.tz_offset_minutes,
      enabled: r.enabled,
    })),
  }
}

export function toJson(e: ReminderExport): string {
  return JSON.stringify(e, null, 2)
}

/** Читаемая сводка (только для чтения/копирования; импорт — через JSON). */
export function toText(e: ReminderExport, categoryNames: Map<number, string>): string {
  const lines = [e.name]
  for (const r of e.reminders) {
    const cat = r.category_id ? categoryNames.get(r.category_id) : undefined
    lines.push(`• ${r.title} — ${scheduleText(r as Reminder, cat)}`)
  }
  return lines.join('\n')
}

export function parseJson(text: string): ReminderExport {
  const raw = JSON.parse(text) as Partial<ReminderExport>
  return {
    name: String(raw.name ?? '').trim(),
    reminders: Array.isArray(raw.reminders) ? raw.reminders : [],
  }
}
