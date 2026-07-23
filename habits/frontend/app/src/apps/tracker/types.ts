export type TrackerKind = 'marks' | 'counter'
export type TrackerStyle = 'square' | 'circle' | 'emoji'

export interface Category {
  id: number
  name: string
  color: string
  position: number
  /** ежедневная привычка — для неё можно создать напоминание в Reminders */
  daily: boolean
  kind: TrackerKind
  style: TrackerStyle
  /** мультицвет (square/circle) или мульти-эмодзи (emoji) */
  multi: boolean
  emoji: string
  owner_id: number
  mine: boolean
  /** есть участники (у владельца) или трекер получен по шарингу */
  shared: boolean
}

export interface MarkInfo {
  color: string | null
  emoji: string | null
  count: number
}

export interface MarkDay extends MarkInfo {
  day: string
}

export interface CategoryMarks {
  category_id: number
  days: MarkDay[]
}

export interface CategoryPatch {
  name?: string
  color?: string
  position?: number
  daily?: boolean
  kind?: TrackerKind
  style?: TrackerStyle
  multi?: boolean
  emoji?: string
}

export interface ShareUser {
  id: number
  username: string
  first_name: string
}
