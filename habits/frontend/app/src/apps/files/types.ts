export interface FileRoot {
  path: string
  mode: 'ro' | 'rw'
}

export interface FileMachine {
  id: number
  name: string
  token: string
  roots: FileRoot[]
  last_seen_at?: string
  created_at: string
  online: boolean
}

export interface FileEntry {
  name: string
  path: string
  is_dir: boolean
  size: number
  mod_time: number
}

export type FileKind = 'text' | 'image' | 'audio' | 'video' | 'pdf' | 'other'

const EXT: Record<string, FileKind> = {}
const add = (kind: FileKind, exts: string[]) => exts.forEach((e) => (EXT[e] = kind))
add('text', ['txt', 'md', 'markdown', 'log', 'json', 'csv', 'tsv', 'xml', 'yaml', 'yml', 'ini', 'conf', 'cfg', 'sh', 'js', 'ts', 'go', 'py', 'rs', 'c', 'h', 'cpp', 'java', 'css', 'html', 'sql', 'toml', 'env', 'srt', 'vtt'])
add('image', ['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'ico', 'avif'])
add('audio', ['mp3', 'ogg', 'oga', 'wav', 'flac', 'm4a', 'aac', 'opus', 'weba'])
add('video', ['mp4', 'webm', 'mkv', 'mov', 'avi', 'm4v', 'ogv'])
add('pdf', ['pdf'])

export function fileKind(name: string): FileKind {
  const dot = name.lastIndexOf('.')
  if (dot < 0) return 'other'
  return EXT[name.slice(dot + 1).toLowerCase()] ?? 'other'
}

export function fileIcon(e: FileEntry): string {
  if (e.is_dir) return '📁'
  switch (fileKind(e.name)) {
    case 'text':
      return '📄'
    case 'image':
      return '🖼'
    case 'audio':
      return '🎵'
    case 'video':
      return '🎬'
    case 'pdf':
      return '📕'
    default:
      return '📦'
  }
}

const UNITS = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ']

export function fmtBytes(n: number): string {
  let v = n
  let u = 0
  while (v >= 1024 && u < UNITS.length - 1) {
    v /= 1024
    u++
  }
  return `${v >= 10 || u === 0 ? Math.round(v) : v.toFixed(1)} ${UNITS[u]}`
}

export function fmtDate(unix: number): string {
  if (!unix) return ''
  const d = new Date(unix * 1000)
  return d.toLocaleDateString('ru-RU', { day: '2-digit', month: '2-digit', year: 'numeric' }) +
    ' ' + d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
}

/** Режим папки, которой принадлежит путь (для показа кнопок записи). */
export function rootModeOf(m: FileMachine, path: string): 'ro' | 'rw' | null {
  for (const r of m.roots) {
    if (path === r.path || path.startsWith(r.path + '/')) return r.mode
  }
  return null
}
