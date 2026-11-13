// Automation: типы и справочники

export interface AutomationConfigView {
  login: string // маскированный
  quantity: number
  tare_mode: 'auto' | 'fixed'
  tare_qty: number
  time_slot: string
  payment: string
  comment: string
}

export interface Automation {
  id: number
  kind: string
  title: string
  enabled: boolean
  interval_days: number
  next_run_at: string | null
  last_run_at: string | null
  last_status: string
  created_at: string
  config: AutomationConfigView
  has_creds: boolean
}

export interface RunStep {
  name: string
  ok: boolean
  detail: string
}

export interface AutomationRun {
  id: number
  status: 'running' | 'success' | 'failed'
  dry_run: boolean
  trigger: 'schedule' | 'manual' | 'dry_run'
  steps: RunStep[]
  error: string
  started_at: string
  finished_at: string | null
}

export interface AgentInfo {
  token: string
  online: boolean
  install: string
}

export const PAYMENT_LABELS: Record<string, string> = {
  checkmo: 'Наличные курьеру',
  banktransfer: 'Банковский перевод',
  ameriapay: 'Банковская карта',
}

export const STATUS_LABELS: Record<string, string> = {
  success: '✅ Успешно',
  failed: '❌ Ошибка',
  dry_run: '🧪 Пробный прогон',
  '': '— ещё не запускалась',
}

export function fmtDateTime(iso: string | null): string {
  if (!iso) return '—'
  return new Date(iso).toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}
