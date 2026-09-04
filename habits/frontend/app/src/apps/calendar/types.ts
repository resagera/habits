export interface CalCategory {
  id: number
  name: string
  color: string
  style: string // square | circle | emoji
  emoji: string
  kind: string
  mine: boolean
  shared: boolean
}

export interface CalMarkDay {
  day: string
  color?: string | null
  emoji?: string | null
  count: number
}

export interface CalCategoryMarks {
  category_id: number
  days: CalMarkDay[]
}

export interface CalReminder {
  day: string
  time: string
  id: number
  title: string
}

export interface CalDiary {
  day: string
  time: string
  id: number
  snippet: string
}

export interface CalTask {
  day: string
  time: string
  id: number
  title: string
  done: boolean
}

export interface CalCheckerDay {
  day: string
  root_id: number
  name: string
  done: number
  total: number
}

export interface CalDeadline {
  day: string
  time: string
  title: string
  group_id: number
}

export interface CalFoodDay {
  day: string
  meals: number
  kcal: number
  goal_kcal: number
}

export interface CalAIRun {
  day: string
  time: string
  id: number
  prompt: string
}

export interface CalendarPayload {
  categories: CalCategory[]
  marks: CalCategoryMarks[]
  reminders: CalReminder[]
  diary: CalDiary[]
  tasks: CalTask[]
  checker_days: CalCheckerDay[]
  deadlines: CalDeadline[]
  food: CalFoodDay[]
  ai: CalAIRun[]
}

/** Настройки слоёв (хранятся на сервере как есть). */
export interface CalPrefs {
  /** выбранные трекеры; null — все доступные */
  trackers: number[] | null
  layers: {
    reminders: boolean
    diary: boolean
    tasks: boolean
    checker: boolean
    food: boolean
    ai: boolean
  }
}

export const DEFAULT_PREFS: CalPrefs = {
  trackers: null,
  layers: { reminders: true, diary: true, tasks: true, checker: true, food: true, ai: true },
}
