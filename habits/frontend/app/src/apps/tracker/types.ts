export type TrackerKind = 'marks' | 'counter'
export type TrackerStyle = 'square' | 'circle' | 'emoji'

/**
 * Цвет из набора трекера. Непустой color2 — градиент.
 * Название нужно человеку в настройках: на отметках оно не рисуется.
 */
export interface CategoryColor {
  color: string
  color2?: string
  name: string
}

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
  /** именованные цвета; пусто — трекер одноцветный, как раньше */
  palette: CategoryColor[]
  owner_id: number
  mine: boolean
  /** есть участники (у владельца) или трекер получен по шарингу */
  shared: boolean
  /**
   * Во сколько у трекера начинается день, минуты от полуночи. 300 = 05:00:
   * отметка в час ночи относится ко вчерашнему дню, потому что по-человечески
   * сутки переключаются, когда встал.
   */
  day_start_minutes: number
  /** сколько отметок ждём за день — напоминание сравнивает «не меньше чем» */
  daily_target: number
  /** повторять напоминание каждые N минут, пока не отмечено (null — не повторять) */
  spam_minutes: number | null
  /** сколько повторов максимум за день */
  spam_max: number
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
  palette?: CategoryColor[]
  day_start_minutes?: number
  daily_target?: number
  spam_minutes?: number
  /** выключить повторы: null в патче значит «не трогать» */
  spam_off?: boolean
  spam_max?: number
}

/** Заглушённый день: трекер общий, а «хватит напоминать» у каждого своё. */
export interface Hush {
  category_id: number
  day: string
}

export interface ShareUser {
  id: number
  username: string
  first_name: string
}
