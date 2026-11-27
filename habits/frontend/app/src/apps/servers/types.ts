export interface AgentDisk {
  mount: string
  device: string
  total: number
  free: number
}

export interface AgentData {
  hostname: string
  os: string
  kernel: string
  arch: string
  external_ip: string
  uptime_sec: number
  cpu_pct: number
  cpu_cores: number
  load1: number
  ram: { total: number; used: number; available: number }
  disks: AgentDisk[]
}

export interface DiskRule {
  mount: string
  min_free_mb: number
}

export interface ServerAlerts {
  enabled: boolean
  disk_min_free_mb: number
  disk_rules: DiskRule[]
  ram_pct: number
  ram_minutes: number
  cpu_pct: number
  cpu_minutes: number
}

export function defaultAlerts(): ServerAlerts {
  return {
    enabled: false,
    disk_min_free_mb: 1024,
    disk_rules: [],
    ram_pct: 95,
    ram_minutes: 5,
    cpu_pct: 95,
    cpu_minutes: 10,
  }
}

export interface ServerEntry {
  id: number
  kind: 'pull' | 'push'
  name: string
  url: string
  token?: string
  push_token?: string
  last_ok_at?: string
  last_error: string
  last_data?: AgentData | null
  alerts?: ServerAlerts
}

/** Минуты с последнего удачного отчёта; null — отчётов ещё не было. */
export function minutesSinceOk(s: ServerEntry): number | null {
  if (!s.last_ok_at) return null
  return Math.max(0, Math.floor((Date.now() - new Date(s.last_ok_at).getTime()) / 60_000))
}

/** Online = свежие данные (моложе 3 минут — порог offline на бэкенде). */
export function isOnline(s: ServerEntry): boolean {
  const m = minutesSinceOk(s)
  return m !== null && m < 3
}

export interface ServerSample {
  at: string
  cpu_pct: number
  ram_used: number
  ram_total: number
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

export function fmtUptime(sec: number): string {
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (d > 0) return `${d} дн ${h} ч`
  if (h > 0) return `${h} ч ${m} мин`
  return `${m} мин`
}
