// Почта: письма, принятые собственным SMTP-приёмником на resager.ru.

export interface MailAddress {
  id: number
  address: string
  label: string
  kind: 'address' | 'alias'
  /** принимать только с этого домена отправителя — фильтр для алиаса магазина */
  only_from: string
  enabled: boolean
  received: number
  rejected: number
  last_at: string | null
  note: string
  /** разбор писем как чеков магазина ('' — не разбирать) */
  parser: string
  parser_account_id: number | null
  parser_category_id: number | null
}

export interface MailMessage {
  id: number
  address_id: number | null
  rcpt: string
  mail_from: string
  from_name: string
  from_addr: string
  subject: string
  message_id: string
  sent_at: string | null
  received_at: string
  size_bytes: number
  text_body: string
  html_body: string
  remote_ip: string
  helo: string
  ptr: string
  tls: boolean
  spf: string
  spam_score: number
  spam_reasons: string
  is_spam: boolean
  is_read: boolean
  starred: boolean
  archived_at: string | null
}

export interface MailAttachment {
  id: number
  message_id: number
  filename: string
  content_type: string
  size_bytes: number
}

export interface MailIPStat {
  ip: string
  first_seen: string
  last_seen: string
  connections: number
  accepted: number
  rejected: number
  blocked_until: string | null
  last_reason: string
  ptr: string
}

/**
 * Отбитая попытка доставки. Раньше по порту 25 были только счётчики — по ним
 * нельзя ответить, КТО и НА КАКОЙ АДРЕС стучался, а именно это и нужно видеть.
 * Журнал ограничен: 30 дней и не больше 5000 строк (чистит сам приёмник).
 */
export interface MailAttempt {
  id: number
  at: string
  ip: string
  ptr: string
  helo: string
  mail_from: string
  /** адрес, на который пытались писать; пусто — до RCPT TO не дошло */
  rcpt: string
  address_id: number | null
  stage: string
  reason: string
  code: string
  blocked: boolean
  tls: boolean
  /** сколько однотипных отказов свёрнуто в строку (мусорные команды сканера) */
  hits: number
}

/** Строка сводки журнала: по адресам получателей или по причинам. */
export interface MailAttemptGroup {
  key: string
  hits: number
  ips: number
  last_at: string
}

export interface MailAttemptStats {
  /** с какого момента ведётся журнал (null — он пуст) */
  since: string | null
  total: number
  day: number
  targets: MailAttemptGroup[]
  reasons: MailAttemptGroup[]
}

export const ATTEMPT_STAGES: Record<string, string> = {
  connect: 'подключение',
  helo: 'приветствие',
  mail: 'отправитель',
  rcpt: 'получатель',
  data: 'передача письма',
  session: 'разговор',
}

export interface MailTotals {
  messages: number
  spam: number
  unread: number
  ips: number
  blocked: number
  rejected: number
  addresses: number
}

/** Разборщик писем магазина. */
export interface ReceiptParser {
  code: string
  title: string
  merchant: string
  from: string
}

/** Чек, разобранный из письма. Сумма в Finance — total. */
export interface MailReceipt {
  id: number
  message_id: number | null
  parser: string
  merchant: string
  order_no: string
  purchased_at: string | null
  purchased_on: string | null
  currency: string
  subtotal: number
  delivery_fee: number
  service_fee: number
  tip: number
  total: number
  paid_with: string
  tx_id: number | null
  status: 'parsed' | 'imported' | 'failed' | 'skipped'
  error: string
  /** подробности вместо позиций: маршрут поездки, машина, тариф */
  note: string
  created_at: string
}

export interface MailReceiptItem {
  id: number
  position: number
  name: string
  qty: number
  unit: string
  amount: number
}

export interface MailOverview {
  messages: MailMessage[]
  total: number
  addresses: MailAddress[]
  totals: MailTotals
  domains: string[]
  hostname: string
  notify: boolean
  /** выдана ли персональная опция «чеки магазинов» */
  receipts_allowed: boolean
  parsers?: ReceiptParser[]
  receipts?: MailReceipt[]
}

export const SPF_LABELS: Record<string, string> = {
  pass: 'SPF пройден',
  fail: 'SPF не пройден',
  softfail: 'SPF softfail',
  neutral: 'SPF нейтрально',
  none: 'SPF не настроен',
  permerror: 'SPF: ошибка записи',
  temperror: 'SPF: сбой проверки',
}

export const PAID_WITH: Record<string, string> = {
  card: 'картой', cash: 'наличными',
}

export function fmtSize(n: number): string {
  if (n < 1024) return `${n} Б`
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} КБ`
  return `${(n / 1024 / 1024).toFixed(1)} МБ`
}

export function fmtWhen(iso: string): string {
  const d = new Date(iso)
  const today = new Date()
  const sameDay = d.toDateString() === today.toDateString()
  if (sameDay) return d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
  return d.toLocaleDateString('ru-RU', { day: '2-digit', month: '2-digit', year: '2-digit' })
}

export function fmtFull(iso: string): string {
  return new Date(iso).toLocaleString('ru-RU')
}
