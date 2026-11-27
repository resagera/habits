// Картинка в просмотрщике на весь экран: и своя, и из общей галереи.
export interface ViewerItem {
  /** готовый адрес полноразмерной картинки для <img> */
  src: string
  /** адрес в том виде, в каком его хранит сервер (сравнение с bg.url) */
  url: string
  /** id своей картинки (null — картинка из общей галереи) */
  id: number | null
  /** имя файла в галерее (null — своя картинка) */
  filename: string | null
  /** вес файла в байтах (0 — неизвестен) */
  size: number
}

/** «1,8 МБ» — вес файла человеческим текстом. */
export function humanSize(bytes: number): string {
  if (!bytes) return ''
  if (bytes < 1024) return `${bytes} Б`
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} КБ`
  return `${(bytes / 1024 / 1024).toFixed(1).replace('.', ',')} МБ`
}

/** Папка своих фонов (дерево строится по parent_id). */
export interface BgFolder {
  id: number
  parent_id: number | null
  name: string
  position: number
  collapsed: boolean
}
