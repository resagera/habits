export interface ContactPhoto {
  id: number
  url: string
}

export interface Contact {
  id: number
  /** id зарегистрированного пользователя; null — «ещё не в боте» */
  contact_id: number | null
  tg_id: number | null
  username: string
  first_name: string
  ext_username: string
  ext_name: string
  note: string
  auto_accept: boolean
  photos: ContactPhoto[]
}

/** Человек уже открывал бота (контакт привязан к пользователю). */
export function inBot(c: Contact): boolean {
  return c.contact_id !== null
}

export interface Suggestion {
  id: number
  username: string
  first_name: string
}

export type ShareKind =
  | 'checker_template'
  | 'checker_group'
  | 'reminder_category'
  | 'article'
  | 'tracker'
  | 'task_project'
  | 'project'

export interface IncomingShare {
  id: number
  from_id: number
  from_username: string
  from_first_name: string
  kind: ShareKind
  title: string
  created_at: string
}

export const KIND_LABELS: Record<ShareKind, string> = {
  checker_template: '📋 шаблон чек-листа',
  checker_group: '✅ список',
  reminder_category: '🔔 категория напоминаний',
  article: '📄 статья',
  tracker: '📊 доступ к трекеру',
  task_project: '🗂 доступ к категории задач',
  project: '📦 доступ к проекту',
}

/** Куда попадает принятое (для тоста). */
export const KIND_PAGES: Record<ShareKind, string> = {
  checker_template: 'Checker',
  checker_group: 'Checker',
  reminder_category: 'Reminders',
  article: 'Articles',
  tracker: 'Tracker',
  task_project: 'Tasks',
  project: 'Projects',
}

export function userLabel(u: {
  first_name: string
  username: string
  id?: number
  contact_id?: number | null
}): string {
  return u.first_name || (u.username ? '@' + u.username : `#${u.id ?? u.contact_id}`)
}

/** Имя контакта с учётом «внешних» (ещё не в боте). */
export function contactLabel(c: Contact): string {
  if (inBot(c)) return userLabel(c)
  return c.ext_name || (c.ext_username ? '@' + c.ext_username : 'Новый контакт')
}

/** Подстрока-идентификатор под именем. */
export function contactSub(c: Contact): string {
  if (inBot(c)) {
    return (c.username ? '@' + c.username + ' · ' : '') + '#' + c.contact_id
  }
  const parts: string[] = []
  if (c.ext_username) parts.push('@' + c.ext_username)
  if (c.tg_id) parts.push('#' + c.tg_id)
  return parts.join(' · ') || 'данные появятся после первого входа'
}
