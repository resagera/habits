export type ProjectStatus =
  | 'draft'
  | 'planned'
  | 'active'
  | 'paused'
  | 'done'
  | 'cancelled'
  | 'archived'

export const STATUS_LABELS: Record<ProjectStatus, string> = {
  draft: '📝 Черновик',
  planned: '🗓 Запланирован',
  active: '🚀 Активен',
  paused: '⏸ Приостановлен',
  done: '✅ Завершён',
  cancelled: '🚫 Отменён',
  archived: '🗄 Архивирован',
}

/** Предустановленные типы проектов (плюс подсказки из уже упомянутых). */
export const PRESET_TYPES = ['рабочий', 'личный', 'разработка', 'обучение', 'мероприятие', 'покупки']

export interface ProjectCategory {
  id: number
  name: string
  position: number
}

export interface Project {
  id: number
  category_id: number | null
  name: string
  description: string
  icon: string
  color: string
  cover: string
  ptype: string
  status: ProjectStatus
  tags: string[]
  start_date: string | null
  due_date: string | null
  tz: string
  position: number
  created_at: string
  updated_at: string
  owner_id: number
  owner_name: string
  mine: boolean
  shared: boolean
  changed: boolean
}

export type BlockKind =
  | 'text'
  | 'images'
  | 'file'
  | 'location'
  | 'checker_group'
  | 'article'
  | 'task'
  | 'task_category'

export const KIND_LABELS: Record<BlockKind, string> = {
  text: '📝 Текст',
  images: '🖼 Картинки',
  file: '📎 Файл',
  location: '📍 Геолокация',
  checker_group: '✅ Чек-лист',
  article: '📄 Статья',
  task: '🗂 Задача',
  task_category: '🗃 Категория задач',
}

export interface TextContent {
  text: string
  rich: boolean
}

export interface ImagesContent {
  images: string[]
}

export interface FileContent {
  url: string
  name: string
  size: number
}

export interface LocationContent {
  lat: number
  lon: number
  label: string
}

export interface RefContent {
  ref_id: number
}

export interface ResolvedCheckItem {
  id: number
  name: string
  done: boolean
}

export interface ResolvedCheckGroup {
  id: number
  name: string
  items: ResolvedCheckItem[]
  subgroups?: ResolvedCheckGroup[]
  missing?: boolean
}

export interface ResolvedArticle {
  id: number
  title: string
  content: string
  updated_at: string
  missing?: boolean
}

export interface ResolvedTask {
  id: number
  title: string
  status: string
  status_kind: 'open' | 'done' | 'archived'
  priority: number
  due_date: string | null
  due_time: string | null
  checklist_done: number
  checklist_total: number
  missing?: boolean
}

export interface ResolvedTaskCategory {
  id: number
  name: string
  color: string
  tasks: ResolvedTask[]
  missing?: boolean
}

export interface ProjectBlock {
  id: number
  user_id: number
  kind: BlockKind
  position: number
  collapsed: boolean
  bg: string
  content: TextContent | ImagesContent | FileContent | LocationContent | RefContent
  data?:
    | ResolvedCheckGroup
    | ResolvedArticle
    | ResolvedTask
    | ResolvedTaskCategory
    | { missing: true }
}

export interface HistoryEntry {
  id: number
  user_id: number
  user_name: string
  action: string
  at: string
}

export interface ShareUser {
  id: number
  username: string
  first_name: string
}

/** Абсолютный URL загруженного файла: в проде приложение живёт под /app/habits/. */
export function assetUrl(u: string): string {
  return import.meta.env.BASE_URL + u
}

export function fmtSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} Б`
  if (bytes < 1 << 20) return `${(bytes / 1024).toFixed(1)} КБ`
  if (bytes < 1 << 30) return `${(bytes / (1 << 20)).toFixed(1)} МБ`
  return `${(bytes / (1 << 30)).toFixed(2)} ГБ`
}

export function fmtDate(iso: string): string {
  return new Date(iso).toLocaleDateString('ru-RU', { day: 'numeric', month: 'short', year: 'numeric' })
}

export function fmtDateTime(iso: string): string {
  return new Date(iso).toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    year: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function userLabel(u: ShareUser): string {
  return u.first_name || (u.username ? '@' + u.username : `#${u.id}`)
}
