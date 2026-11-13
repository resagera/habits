export interface ArticleFolder {
  id: number
  parent_id: number | null
  name: string
  position: number
}

export interface ArticleMeta {
  id: number
  folder_id: number | null
  title: string
  created_at: string
  updated_at: string
}

/** Запись истории изменений (метаданные, без содержимого). */
export interface ArticleRevision {
  id: number
  saved_at: string
  size: number
}

export interface Article extends ArticleMeta {
  content: string
}

/** Чужая категория, доступная по шарингу (контент живёт у владельца). */
export interface SharedTree {
  root: ArticleFolder
  folders: ArticleFolder[]
  articles: ArticleMeta[]
  owner: { id: number; username: string; first_name: string }
}

export interface ContentHit {
  id: number
  title: string
  snippet: string
}

export function fmtDate(iso: string): string {
  const d = new Date(iso)
  return `${String(d.getDate()).padStart(2, '0')}.${String(d.getMonth() + 1).padStart(2, '0')}.${d.getFullYear()}`
}

export function fmtDateTime(iso: string): string {
  const d = new Date(iso)
  return `${fmtDate(iso)} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

const SIZE_UNITS = ['Б', 'КБ', 'МБ']

export function fmtSize(bytes: number): string {
  let v = bytes
  let u = 0
  while (v >= 1024 && u < SIZE_UNITS.length - 1) {
    v /= 1024
    u++
  }
  return `${u === 0 ? Math.round(v) : v.toFixed(1)} ${SIZE_UNITS[u]}`
}
