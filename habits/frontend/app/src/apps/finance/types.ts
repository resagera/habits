// Finance — плановые траты с оплатой и напоминаниями + долги в обе стороны.

export type RepeatKind = 'once' | 'monthly' | 'yearly' | 'interval_months' | 'interval_days'

export interface FinancePlan {
  id: number
  name: string
  amount: number
  currency: string
  is_estimate: boolean
  next_due_date: string
  repeat_kind: RepeatKind
  interval_n: number
  /** имя категории: денормализовано сервером, чтобы списки не ходили за деревом */
  category: string
  note: string
  autopay: boolean
  notify_days_before: number
  paused_until: string | null
  enabled: boolean
  archived_at: string | null
  category_id: number | null
  account_id: number | null
}

/** Конкретный платёж конкретной даты: будущие не хранятся, их считает сервер. */
export interface Occurrence {
  plan_id: number
  name: string
  amount: number
  currency: string
  amount_base: number
  due_date: string
  is_estimate: boolean
  autopay: boolean
  repeat_kind: RepeatKind
  interval_n: number
  category: string
  note: string
  overdue: boolean
  /** первое вхождение плана — только у него есть действия «Оплатил»/«Пропустить» */
  first: boolean
}

export interface FinancePayment {
  id: number
  plan_id: number
  plan_name: string
  paid_on: string
  due_date: string
  amount: number
  currency: string
  base_currency: string
  rate_to_base: number
  periods: number
  skipped: boolean
  note: string
}

export type DebtDirection = 'owed_to_me' | 'i_owe'
export type DebtStatus = 'open' | 'partial' | 'paid' | 'forgiven'

export interface FinanceDebt {
  id: number
  direction: DebtDirection
  person: string
  contact_user_id: number | null
  subject: string
  amount: number
  currency: string
  expected_on: string | null
  status: DebtStatus
  note: string
  archived_at: string | null
  paid: number
  remaining: number
}

export interface FinanceDebtPayment {
  id: number
  debt_id: number
  paid_on: string
  amount: number
  note: string
}

export interface FinanceSummary {
  this_month: number
  next_month: number
  overdue: number
  autopay_this_month: number
  monthly_normalized: number
  debts_owed_to_me: number
  debts_i_owe: number
  debts_overdue: number
  debts_this_month: number
  /** факт текущего месяца — рядом с планом */
  spent_this_month: number
  earned_this_month: number
  accounts_total: number
}

// --- фазы 3–4: траты, категории, счета, цели ---

export type TxKind = 'expense' | 'income' | 'transfer'

/** Узел дерева категорий любой вложенности. */
export interface FinanceCategory {
  id: number
  parent_id: number | null
  name: string
  kind: 'expense' | 'income'
  icon: string
  color: string
  position: number
  archived_at: string | null
}

export interface FinanceTx {
  id: number
  kind: TxKind
  spent_on: string
  amount: number
  currency: string
  base_currency: string
  rate_to_base: number
  category_id: number | null
  account_id: number | null
  to_account_id: number | null
  plan_id: number | null
  payment_id: number | null
  merchant: string
  note: string
  external_id: string | null
}

export type AccountKind = 'cash' | 'card' | 'bank' | 'savings' | 'other'

export interface FinanceAccount {
  id: number
  name: string
  kind: AccountKind
  currency: string
  start_balance: number
  include_in_total: boolean
  note: string
  position: number
  archived_at: string | null
  /** остаток в валюте счёта и в базовой — считает сервер по движениям */
  balance: number
  balance_base: number
}

export interface FinanceGoal {
  id: number
  name: string
  target_amount: number
  currency: string
  account_id: number | null
  due_date: string | null
  note: string
  archived_at: string | null
  saved: number
}

export interface FinanceGoalMove {
  id: number
  goal_id: number
  moved_on: string
  amount: number
  note: string
}

/** Справочники одним запросом: категории, счета, цели, валюты. */
export interface FinanceRefs {
  base_currency: string
  categories: FinanceCategory[]
  accounts: FinanceAccount[]
  goals: FinanceGoal[]
  currencies: string[]
  totals: { balance_base: number; base_currency: string }
}

export interface MonthStat {
  month: string
  expense: number
  income: number
}

export interface CatStat {
  id: number
  parent_id: number | null
  name: string
  icon: string
  depth: number
  own: number
  total: number
  share: number
  prev: number
}

export interface FinanceStats {
  base_currency: string
  from: string
  to: string
  months: MonthStat[]
  categories: CatStat[]
  uncategorized: number
  total_expense: number
  total_income: number
  prev_expense: number
  avg_month: number
  prev_from: string
  prev_to: string
  category_scope: number
}

// --- группы товаров, разметка и история цен ---

export interface ItemGroup {
  code: string
  title: string
  icon: string
}

/** Товар в сводке: сколько раз брали, сколько ушло, как менялась цена. */
export interface TopItem {
  name_key: string
  name: string
  category_id: number | null
  times: number
  qty: number
  spent: number
  last_price: number
  first_price: number
  last_at: string | null
}

export interface ItemRule {
  id: number
  merchant: string
  name_key: string
  name_sample: string
  category_id: number
  source: string
  hits: number
}

export interface PricePoint {
  date: string
  qty: number
  unit: string
  price: number
  total: number
}

export interface ItemPriceHistory {
  name_key: string
  name: string
  currency: string
  points: PricePoint[]
  times: number
  spent: number
  first: number
  last: number
}

export interface ItemSuggestion {
  name: string
  name_key: string
  group: string
  group_title: string
  category_id: number | null
}

/** Своё словарное правило: «всё, где есть это слово, — в эту категорию». */
export interface WordRule {
  id: number
  pattern: string
  category_id: number
  position: number
}

export interface CategoryItemStats {
  category_id: number
  items: number
  words: number
}

/** Доля траты: часть суммы, отнесённая к категории. */
export interface TxSplit {
  id: number
  category_id: number | null
  amount: number
  position: number
}

export const ACCOUNT_KINDS: Record<AccountKind, string> = {
  cash: 'Наличные',
  card: 'Карта',
  bank: 'Счёт в банке',
  savings: 'Накопительный',
  other: 'Другое',
}

export const ACCOUNT_ICONS: Record<AccountKind, string> = {
  cash: '💵', card: '💳', bank: '🏦', savings: '🐷', other: '📦',
}

/** Плоский список дерева в порядке обхода — для селектов и настроек. */
export function flattenCategories(
  list: FinanceCategory[], parent: number | null = null, depth = 0,
): { cat: FinanceCategory; depth: number }[] {
  const out: { cat: FinanceCategory; depth: number }[] = []
  for (const c of list.filter((x) => (x.parent_id ?? null) === parent)) {
    out.push({ cat: c, depth })
    out.push(...flattenCategories(list, c.id, depth + 1))
  }
  return out
}

/** Название месяца из «2026-08» — подпись на столбике отчёта. */
export function monthLabel(m: string): string {
  const [y, mm] = m.split('-')
  const d = new Date(Number(y), Number(mm) - 1, 1)
  return d.toLocaleDateString('ru-RU', { month: 'short' })
}

export interface FinanceOverview {
  base_currency: string
  today: string
  summary: FinanceSummary
  buckets: {
    overdue: Occurrence[]
    this_month: Occurrence[]
    next_month: Occurrence[]
    later: Occurrence[]
  }
  debts: FinanceDebt[]
  hide_amounts: boolean
}

export interface FinanceSettings {
  base_currency: string
  notify_hour: number
  tz_off: number
  hide_amounts: boolean
}

export const REPEAT_LABELS: Record<RepeatKind, string> = {
  once: 'разово',
  monthly: 'ежемесячно',
  yearly: 'ежегодно',
  interval_months: 'раз в N месяцев',
  interval_days: 'раз в N дней',
}

/** Короткая подпись повтора для карточки. */
export function repeatShort(kind: RepeatKind, n: number): string {
  switch (kind) {
    case 'once':
      return 'разово'
    case 'monthly':
      return 'ежемес.'
    case 'yearly':
      return 'ежегодно'
    case 'interval_months':
      return `раз в ${n} мес.`
    case 'interval_days':
      return `раз в ${n} дн.`
  }
}

export function fmtMoney(v: number, currency: string): string {
  const n = Math.abs(v) >= 1000 ? Math.round(v) : Math.round(v * 100) / 100
  return `${n.toLocaleString('ru-RU')} ${currency.toUpperCase()}`
}

export function fmtDate(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleDateString('ru-RU', { day: '2-digit', month: '2-digit', year: '2-digit' })
}

/** Сегодняшняя дата в формате input[type=date]. */
export function todayStr(): string {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}
