export interface Release {
  id: number
  version: string
  released_on: string
  title: string
  public_notes: string
  status: string
  // только для админа — в ответе обычному пользователю этих полей нет
  tech_notes?: string
  comment?: string
}

export const RELEASE_STATUSES = [
  'released',
  'planned',
  'in_progress',
  'rolled_back',
  'deprecated',
] as const

export const STATUS_LABELS: Record<string, string> = {
  released: 'Выпущен',
  planned: 'Запланирован',
  in_progress: 'В работе',
  rolled_back: 'Откачен',
  deprecated: 'Устарел',
}
