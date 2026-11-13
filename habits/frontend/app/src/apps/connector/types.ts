// Коннектор: правила «источник → условия → результат».
//
// Реестр источников, полей, операций и результатов приходит с сервера
// (meta в overview) — формы рисуются по нему. Новое поле на бэкенде
// появляется в интерфейсе само, править здесь ничего не нужно.
import type { Category } from '../tracker/types'
import type { MailAddress, MailMessage } from '../mail/types'

/**
 * Узел дерева условий: либо ГРУППА (op + nodes), либо ЛИСТ (field + cmp + value).
 * Одного типа хватает на любую вложенность — различие в непустом op.
 */
export interface ConnectorNode {
  op?: 'and' | 'or'
  nodes?: ConnectorNode[]
  field?: string
  cmp?: string
  value?: string
}

export interface ConnectorAction {
  type: string
  /** отметка в трекере */
  category_id?: number
  color?: string
  emoji?: string
  count?: number
  /** event — день события (дата письма), today — день срабатывания */
  day?: 'event' | 'today'
  /** сообщение в Telegram */
  text?: string
  /** пометка письма */
  flag?: string
  value?: boolean
}

export interface ConnectorRule {
  id: number
  name: string
  enabled: boolean
  sources: string[]
  match: ConnectorNode
  actions: ConnectorAction[]
  tz_offset_minutes: number
  runs: number
  last_run_at: string | null
  created_at: string
  updated_at: string
}

export interface ConnectorRun {
  id: number
  rule_id: number
  source: string
  event_id: number | null
  event_label: string
  status: string
  detail: string
  at: string
}

export interface MetaField {
  code: string
  title: string
  kind: 'text' | 'flag'
  hint?: string
}

export interface MetaSource {
  code: string
  title: string
  icon: string
  event: string
  fields: MetaField[]
}

export interface MetaCmp {
  code: string
  title: string
  kind: 'text' | 'flag'
  /** значение не нужно: «пусто», «да», «нет» */
  no_value?: boolean
}

export interface MetaAction {
  code: string
  title: string
  hint?: string
}

export interface ConnectorMeta {
  sources: MetaSource[]
  cmps: MetaCmp[]
  actions: MetaAction[]
  mail_flags: MetaAction[]
  limits: { depth: number; nodes: number; actions: number }
}

export interface ConnectorOverview {
  rules: ConnectorRule[]
  runs: ConnectorRun[]
  meta: ConnectorMeta
  /** трекеры человека — куда ставить отметку */
  trackers: Category[]
  addresses: MailAddress[]
  /** последние письма: на них проверяется правило */
  messages: MailMessage[]
}

/** Разбор проверки: повторяет форму дерева, чтобы рисовать ✓/✗ у условий. */
export interface Trace {
  op?: string
  field?: string
  cmp?: string
  value?: string
  got?: string
  ok: boolean
  nodes?: Trace[]
}

export interface TestResult {
  matched: boolean
  trace: Trace
  message_id: number
  event_label: string
}

export interface RunResult {
  matched: boolean
  trace: Trace
  done: string[]
  skipped: boolean
  error?: string
}

export function emptyGroup(op: 'and' | 'or' = 'and'): ConnectorNode {
  return { op, nodes: [] }
}

/** Новое правило: источник «почта», пустое дерево, один результат-заглушка. */
export function emptyRule(): Omit<ConnectorRule, 'id' | 'runs' | 'last_run_at' | 'created_at' | 'updated_at'> {
  return {
    name: '',
    enabled: true,
    sources: ['mail'],
    match: emptyGroup('and'),
    actions: [],
    tz_offset_minutes: -new Date().getTimezoneOffset(),
  }
}

export function isGroup(n: ConnectorNode): boolean {
  return !!n.op || Array.isArray(n.nodes) || !n.field
}

export function fmtWhen(iso: string): string {
  return new Date(iso).toLocaleString('ru-RU', {
    day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit',
  })
}
