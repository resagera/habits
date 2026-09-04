import { api } from '../../shared/api/client'
import type {
  FinanceAccount, FinanceCategory, FinanceDebt, FinanceDebtPayment, FinanceGoal,
  FinanceGoalMove, FinanceOverview, FinancePayment, FinancePlan, FinanceRefs,
  FinanceSettings, FinanceStats, FinanceTx, ItemGroup, ItemPriceHistory, ItemRule,
  ItemSuggestion, TopItem, TxSplit, WordRule, CategoryItemStats,
} from './types'

/** tz_off нужен серверу, чтобы «сегодня» и утренние напоминания были местными. */
export function fetchOverview(): Promise<FinanceOverview> {
  const tzOff = -new Date().getTimezoneOffset()
  return api.get(`/finance/overview?tz_off=${tzOff}`)
}

export function fetchPlans(archived = false): Promise<{ plans: FinancePlan[] }> {
  return api.get(`/finance/plans${archived ? '?archived=1' : ''}`)
}

export function createPlan(body: Partial<FinancePlan>): Promise<{ plan: FinancePlan }> {
  return api.post('/finance/plans', body)
}

export function updatePlan(id: number, body: Partial<FinancePlan>): Promise<{ plan: FinancePlan }> {
  return api.put(`/finance/plans/${id}`, body)
}

export function archivePlan(id: number, archived: boolean): Promise<{ archived: boolean }> {
  return api.post(`/finance/plans/${id}/archive`, { archived })
}

export function deletePlan(id: number): Promise<void> {
  return api.delete(`/finance/plans/${id}`)
}

export function payPlan(
  id: number, body: { paid_on?: string; amount?: number; periods?: number; note?: string },
): Promise<{ plan: FinancePlan }> {
  return api.post(`/finance/plans/${id}/pay`, body)
}

export function skipPlan(id: number, body: { periods?: number } = {}): Promise<{ plan: FinancePlan }> {
  return api.post(`/finance/plans/${id}/skip`, body)
}

export function fetchPayments(
  planId?: number,
): Promise<{ payments: FinancePayment[]; average?: number }> {
  return api.get(`/finance/payments${planId ? `?plan_id=${planId}` : ''}`)
}

export function fetchDebts(archived = false): Promise<{ debts: FinanceDebt[] }> {
  return api.get(`/finance/debts${archived ? '?archived=1' : ''}`)
}

export function createDebt(body: Partial<FinanceDebt>): Promise<{ debt: FinanceDebt }> {
  return api.post('/finance/debts', body)
}

export function updateDebt(id: number, body: Partial<FinanceDebt>): Promise<{ debt: FinanceDebt }> {
  return api.put(`/finance/debts/${id}`, body)
}

export function addDebtPayment(
  id: number, body: { amount: number; paid_on?: string; note?: string },
): Promise<{ debt: FinanceDebt }> {
  return api.post(`/finance/debts/${id}/payments`, body)
}

export function fetchDebtPayments(id: number): Promise<{ payments: FinanceDebtPayment[] }> {
  return api.get(`/finance/debts/${id}/payments`)
}

export function setDebtStatus(id: number, status: string): Promise<{ debt: FinanceDebt }> {
  return api.post(`/finance/debts/${id}/status`, { status })
}

export function archiveDebt(id: number, archived: boolean): Promise<{ archived: boolean }> {
  return api.post(`/finance/debts/${id}/archive`, { archived })
}

export function deleteDebt(id: number): Promise<void> {
  return api.delete(`/finance/debts/${id}`)
}

// --- фазы 3–4: траты, категории, счета, цели, отчёт ---

/** Справочники одним запросом: за ними ходят все формы страницы. */
export function fetchRefs(): Promise<FinanceRefs> {
  return api.get('/finance/refs')
}

export interface TxQuery {
  from?: string
  to?: string
  category_id?: number
  account_id?: number
  kind?: string
  q?: string
  limit?: number
  offset?: number
}

export function fetchTransactions(q: TxQuery = {}): Promise<{ transactions: FinanceTx[]; total: number }> {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(q)) {
    if (v !== undefined && v !== '' && v !== 0) p.set(k, String(v))
  }
  const s = p.toString()
  return api.get(`/finance/transactions${s ? `?${s}` : ''}`)
}

export function createTx(body: Partial<FinanceTx>): Promise<{ transaction: FinanceTx }> {
  return api.post('/finance/transactions', body)
}

export function updateTx(id: number, body: Partial<FinanceTx>): Promise<{ transaction: FinanceTx }> {
  return api.put(`/finance/transactions/${id}`, body)
}

export function deleteTx(id: number): Promise<void> {
  return api.delete(`/finance/transactions/${id}`)
}

export function fetchCategories(): Promise<{ categories: FinanceCategory[] }> {
  return api.get('/finance/categories')
}

export function createCategory(body: Partial<FinanceCategory>): Promise<{ category: FinanceCategory }> {
  return api.post('/finance/categories', body)
}

export function updateCategory(id: number, body: Partial<FinanceCategory>): Promise<{ category: FinanceCategory }> {
  return api.put(`/finance/categories/${id}`, body)
}

export function deleteCategory(id: number): Promise<void> {
  return api.delete(`/finance/categories/${id}`)
}

export function seedCategories(): Promise<{ created: number; categories: FinanceCategory[] }> {
  return api.post('/finance/categories/seed', {})
}

export function fetchAccounts(): Promise<{ accounts: FinanceAccount[]; totals: { balance_base: number } }> {
  return api.get('/finance/accounts')
}

export function createAccount(body: Partial<FinanceAccount>): Promise<{ account: FinanceAccount }> {
  return api.post('/finance/accounts', body)
}

export function updateAccount(id: number, body: Partial<FinanceAccount>): Promise<{ account: FinanceAccount }> {
  return api.put(`/finance/accounts/${id}`, body)
}

export function archiveAccount(id: number, archived: boolean): Promise<{ archived: boolean }> {
  return api.post(`/finance/accounts/${id}/archive`, { archived })
}

export function deleteAccount(id: number): Promise<void> {
  return api.delete(`/finance/accounts/${id}`)
}

export function fetchGoals(): Promise<{ goals: FinanceGoal[] }> {
  return api.get('/finance/goals')
}

export function createGoal(body: Partial<FinanceGoal>): Promise<{ goal: FinanceGoal }> {
  return api.post('/finance/goals', body)
}

export function updateGoal(id: number, body: Partial<FinanceGoal>): Promise<{ goal: FinanceGoal }> {
  return api.put(`/finance/goals/${id}`, body)
}

export function deleteGoal(id: number): Promise<void> {
  return api.delete(`/finance/goals/${id}`)
}

export function addGoalMove(
  id: number, body: { amount: number; moved_on?: string; note?: string },
): Promise<{ goal: FinanceGoal }> {
  return api.post(`/finance/goals/${id}/moves`, body)
}

export function fetchGoalMoves(id: number): Promise<{ moves: FinanceGoalMove[] }> {
  return api.get(`/finance/goals/${id}/moves`)
}

export function deleteGoalMove(id: number, moveId: number): Promise<{ goal: FinanceGoal }> {
  return api.delete(`/finance/goals/${id}/moves/${moveId}`)
}

export function fetchStats(q: { months?: number; from?: string; to?: string; category_id?: number } = {}): Promise<FinanceStats> {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(q)) {
    if (v !== undefined && v !== '' && v !== 0) p.set(k, String(v))
  }
  const s = p.toString()
  return api.get(`/finance/stats${s ? `?${s}` : ''}`)
}

// --- группы товаров, разметка позиций и история цен ---

export function fetchItemGroups(): Promise<{
  groups: ItemGroup[]
  map: Record<string, number>
  /** слова встроенного словаря по группам — только для показа */
  dictionary: Record<string, string[]>
  words: WordRule[]
  stats: CategoryItemStats[]
}> {
  return api.get('/finance/item-groups')
}

export function createWordRule(pattern: string, categoryId: number): Promise<{
  word: WordRule; reclassified: number
}> {
  return api.post('/finance/items/words', { pattern, category_id: categoryId })
}

export function deleteWordRule(id: number): Promise<{ reclassified: number }> {
  return api.delete(`/finance/items/words/${id}`)
}

/**
 * reset перепривязывает ВСЕ группы к своим категориям и перебирает уже
 * разобранные чеки: это единственный способ починить состояние «все группы
 * смотрят в одну категорию», при котором диаграмма схлопывается в один сектор.
 */
export function seedItemGroups(reset = false): Promise<{
  groups: ItemGroup[]; map: Record<string, number>; categories: FinanceCategory[]
  reclassified: number
}> {
  return api.post('/finance/item-groups/seed', { reset })
}

export function setItemGroup(code: string, categoryId: number): Promise<{
  ok: boolean; reclassified: number
}> {
  return api.put(`/finance/item-groups/${code}`, { category_id: categoryId })
}

export function fetchUnclassified(): Promise<{ items: TopItem[] }> {
  return api.get('/finance/items/unclassified')
}

/** Решение по товару применяется ко всем чекам, включая прошлые. */
export function assignItems(
  items: { name_key: string; name_sample?: string; merchant?: string; category_id: number | null }[],
  opts: { remember?: boolean; source?: string } = {},
): Promise<{ assigned: number }> {
  return api.post('/finance/items/assign', { items, ...opts })
}

export function fetchItemRules(): Promise<{ rules: ItemRule[] }> {
  return api.get('/finance/items/rules')
}

export function deleteItemRule(id: number): Promise<void> {
  return api.delete(`/finance/items/rules/${id}`)
}

export function fetchTopItems(q: { from?: string; to?: string; limit?: number } = {}): Promise<{
  items: TopItem[]; from: string; to: string
}> {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(q)) if (v) p.set(k, String(v))
  const s = p.toString()
  return api.get(`/finance/items/top${s ? `?${s}` : ''}`)
}

export function fetchItemPrices(nameKey: string): Promise<ItemPriceHistory> {
  return api.get(`/finance/items/prices?name_key=${encodeURIComponent(nameKey)}`)
}

export function suggestGroups(names: string[]): Promise<{
  run_id: number; machine: string; queued_offline: boolean
}> {
  return api.post('/finance/items/suggest', { names })
}

export function fetchSuggestions(runId: number): Promise<{
  status: string; error: string; suggestions?: ItemSuggestion[]; parse_error?: string; raw?: string
}> {
  return api.get(`/finance/items/suggest/${runId}`)
}

export function fetchTxSplits(txId: number): Promise<{ splits: TxSplit[] }> {
  return api.get(`/finance/transactions/${txId}/splits`)
}

export function fetchFinanceSettings(): Promise<FinanceSettings> {
  return api.get('/finance/settings')
}

export function saveFinanceSettings(body: Partial<FinanceSettings>): Promise<FinanceSettings> {
  return api.put('/finance/settings', body)
}
