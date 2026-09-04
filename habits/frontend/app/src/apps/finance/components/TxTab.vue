<script setup lang="ts">
// Лента фактических трат. Реестр денег один: сюда же попадают оплаты плановых
// платежей (у них есть plan_id), поэтому отчёт собирается из одного источника.
import { computed, ref, watch } from 'vue'
import { confirmAction } from '../../../shared/telegram'
import { showToast } from '../../../shared/toast'
import { createTx, deleteTx, fetchTransactions, updateTx } from '../api'
import {
  fmtMoney, todayStr, type FinanceRefs, type FinanceTx, type TxKind,
} from '../types'
import CategoryPicker from './CategoryPicker.vue'
import CategoriesModal from './CategoriesModal.vue'

const props = defineProps<{ refs: FinanceRefs | null; hide: boolean }>()
const emit = defineEmits<{ changed: []; refsChanged: [] }>()

const PAGE = 30
const list = ref<FinanceTx[]>([])
const total = ref(0)
const loading = ref(true)
const busy = ref(false)
const catsOpen = ref(false)

// фильтры: месяц выбирается из последних 12 — «за всё время» на ленте почти
// никогда не нужно, зато мешает попасть в нужный месяц
const month = ref(todayStr().slice(0, 7))
const filterCat = ref<number | null>(null)
const filterAcc = ref(0)
const filterKind = ref('')
const search = ref('')

const months = computed(() => {
  const out: string[] = []
  const d = new Date()
  for (let i = 0; i < 12; i++) {
    out.push(`${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`)
    d.setMonth(d.getMonth() - 1)
  }
  return out
})

function monthRange(m: string): { from: string; to: string } {
  const [y, mm] = m.split('-').map(Number)
  const last = new Date(y, mm, 0).getDate()
  return { from: `${m}-01`, to: `${m}-${String(last).padStart(2, '0')}` }
}

async function load(more = false) {
  loading.value = !more
  try {
    const range = month.value ? monthRange(month.value) : {}
    const res = await fetchTransactions({
      ...range,
      category_id: filterCat.value ?? 0,
      account_id: filterAcc.value,
      kind: filterKind.value,
      q: search.value.trim(),
      limit: PAGE,
      offset: more ? list.value.length : 0,
    })
    list.value = more ? [...list.value, ...res.transactions] : res.transactions
    total.value = res.total
  } catch {
    showToast('Не удалось загрузить траты')
  } finally {
    loading.value = false
  }
}
void load()

watch([month, filterCat, filterAcc, filterKind], () => void load())
let searchTimer: ReturnType<typeof setTimeout> | undefined
watch(search, () => {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => void load(), 350)
})

function money(v: number, cur: string): string {
  return props.hide ? '•••' : fmtMoney(v, cur)
}

const catName = computed(() => {
  const map = new Map<number, string>()
  for (const c of props.refs?.categories ?? []) {
    map.set(c.id, `${c.icon ? c.icon + ' ' : ''}${c.name}`)
  }
  return map
})
const accName = computed(() => {
  const map = new Map<number, string>()
  for (const a of props.refs?.accounts ?? []) map.set(a.id, a.name)
  return map
})

/** Лента по дням с итогом дня: так видно, куда ушёл конкретный день. */
const byDay = computed(() => {
  const groups: { day: string; items: FinanceTx[]; sum: number; cur: string }[] = []
  for (const t of list.value) {
    const day = t.spent_on.slice(0, 10)
    let g = groups.find((x) => x.day === day)
    if (!g) {
      g = { day, items: [], sum: 0, cur: props.refs?.base_currency ?? 'amd' }
      groups.push(g)
    }
    g.items.push(t)
    if (t.kind === 'expense') g.sum += t.amount * (t.rate_to_base || 1)
  }
  return groups
})

function dayLabel(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleDateString('ru-RU', { day: '2-digit', month: 'long', weekday: 'short' })
}

// --- форма ---

const LAST_KEY = 'finance:tx:last'

const form = ref<{
  id: number | null
  kind: TxKind
  spent_on: string
  amount: string
  currency: string
  category_id: number | null
  account_id: number
  to_account_id: number
  merchant: string
  note: string
} | null>(null)

function open(t?: FinanceTx) {
  let last: { category_id?: number; account_id?: number } = {}
  try {
    last = JSON.parse(localStorage.getItem(LAST_KEY) ?? '{}')
  } catch {
    /* подсказка последнего ввода — не повод падать */
  }
  const accounts = props.refs?.accounts ?? []
  form.value = {
    id: t?.id ?? null,
    kind: t?.kind ?? 'expense',
    spent_on: t?.spent_on?.slice(0, 10) ?? todayStr(),
    amount: t ? String(t.amount) : '',
    currency: t?.currency ?? props.refs?.base_currency ?? 'amd',
    category_id: t ? t.category_id : (last.category_id ?? null),
    account_id: t?.account_id ?? last.account_id ?? accounts[0]?.id ?? 0,
    to_account_id: t?.to_account_id ?? 0,
    merchant: t?.merchant ?? '',
    note: t?.note ?? '',
  }
}

/** Валюта подставляется из счёта: платят обычно тем, что на счёте лежит. */
watch(() => form.value?.account_id, (id) => {
  const f = form.value
  if (!f || f.id) return
  const acc = props.refs?.accounts.find((a) => a.id === id)
  if (acc) f.currency = acc.currency
})

async function save() {
  const f = form.value
  if (!f) return
  const amount = Number(f.amount)
  if (!amount || amount <= 0) {
    showToast('Введите сумму')
    return
  }
  if (f.kind === 'transfer' && (!f.account_id || !f.to_account_id)) {
    showToast('Для перевода нужны оба счёта')
    return
  }
  busy.value = true
  try {
    const body = {
      kind: f.kind,
      spent_on: f.spent_on,
      amount,
      currency: f.currency,
      category_id: f.kind === 'transfer' ? null : f.category_id,
      account_id: f.account_id || null,
      to_account_id: f.kind === 'transfer' ? f.to_account_id : null,
      merchant: f.merchant,
      note: f.note,
    }
    if (f.id) await updateTx(f.id, body)
    else await createTx(body)
    localStorage.setItem(LAST_KEY, JSON.stringify({
      category_id: f.category_id, account_id: f.account_id,
    }))
    form.value = null
    await load()
    emit('changed')
    showToast('Записано ✅')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось сохранить')
  } finally {
    busy.value = false
  }
}

async function remove(t: FinanceTx) {
  const extra = t.plan_id ? ' Плановый платёж при этом останется оплаченным.' : ''
  if (!(await confirmAction(`Удалить запись на ${fmtMoney(t.amount, t.currency)}?${extra}`))) return
  try {
    await deleteTx(t.id)
    await load()
    emit('changed')
  } catch {
    showToast('Не удалось удалить')
  }
}

const KINDS: { k: TxKind; label: string }[] = [
  { k: 'expense', label: 'Расход' },
  { k: 'income', label: 'Доход' },
  { k: 'transfer', label: 'Перевод' },
]
</script>

<template>
  <div>
    <div class="head">
      <button class="btn primary grow" @click="open()">＋ Трата</button>
      <button class="btn" title="Категории" @click="catsOpen = true">🗂</button>
    </div>

    <div class="filters">
      <select v-model="month">
        <option value="">за всё время</option>
        <option v-for="m in months" :key="m" :value="m">{{ m }}</option>
      </select>
      <CategoryPicker v-model="filterCat" :categories="refs?.categories ?? []"
                      empty-label="все категории" />
      <select v-model.number="filterAcc">
        <option :value="0">все счета</option>
        <option v-for="a in refs?.accounts ?? []" :key="a.id" :value="a.id">{{ a.name }}</option>
      </select>
      <select v-model="filterKind">
        <option value="">всё</option>
        <option value="expense">расходы</option>
        <option value="income">доходы</option>
        <option value="transfer">переводы</option>
      </select>
    </div>
    <input v-model="search" class="search" placeholder="Поиск по заметке или магазину" />

    <p v-if="loading" class="hint">Загрузка…</p>
    <p v-else-if="!list.length" class="hint">
      Записей нет. Кнопка «＋ Трата» — и в отчёте появятся цифры; оплаты плановых
      платежей попадают сюда сами.
    </p>

    <template v-for="g in byDay" :key="g.day">
      <div class="day">
        <span>{{ dayLabel(g.day) }}</span>
        <span v-if="g.sum" class="day-sum">{{ money(g.sum, g.cur) }}</span>
      </div>
      <div v-for="t in g.items" :key="t.id" class="row" @click="open(t)">
        <div class="row-main">
          <span class="name">
            <template v-if="t.kind === 'transfer'">
              {{ accName.get(t.account_id ?? 0) ?? '?' }} → {{ accName.get(t.to_account_id ?? 0) ?? '?' }}
            </template>
            <template v-else>
              {{ t.merchant || (t.category_id ? catName.get(t.category_id) : '') || 'Без категории' }}
            </template>
            <span v-if="t.plan_id" title="Плановый платёж">📌</span>
          </span>
          <span class="meta">
            <template v-if="t.kind !== 'transfer' && t.merchant && t.category_id">
              {{ catName.get(t.category_id) }} ·
            </template>
            <template v-if="t.account_id && t.kind !== 'transfer'">
              {{ accName.get(t.account_id) }}
            </template>
            <template v-if="t.note"> · {{ t.note }}</template>
          </span>
        </div>
        <div class="row-right">
          <span class="amount" :class="t.kind">
            {{ t.kind === 'income' ? '+' : t.kind === 'expense' ? '−' : '' }}{{ money(t.amount, t.currency) }}
          </span>
          <button class="mini danger" title="Удалить" @click.stop="remove(t)">✕</button>
        </div>
      </div>
    </template>

    <button v-if="list.length < total" class="btn wide" @click="load(true)">
      Показать ещё ({{ total - list.length }})
    </button>

    <CategoriesModal v-if="catsOpen" :categories="refs?.categories ?? []"
                     @close="catsOpen = false" @changed="emit('refsChanged')" />

    <Teleport to="body">
      <div v-if="form" class="modal" @click.self="form = null">
        <div class="modal-box">
          <h3>{{ form.id ? 'Запись' : 'Новая запись' }}</h3>
          <div class="kinds">
            <button v-for="x in KINDS" :key="x.k" :class="{ on: form.kind === x.k }"
                    @click="form.kind = x.k">{{ x.label }}</button>
          </div>
          <div class="two">
            <div>
              <label>Сумма</label>
              <input v-model="form.amount" type="number" step="0.01" inputmode="decimal" />
            </div>
            <div>
              <label>Валюта</label>
              <input v-model="form.currency" placeholder="amd" />
            </div>
          </div>
          <label>Дата</label>
          <input v-model="form.spent_on" type="date" />
          <template v-if="form.kind !== 'transfer'">
            <label>Категория</label>
            <CategoryPicker v-model="form.category_id" :categories="refs?.categories ?? []"
                            :kind="form.kind === 'income' ? 'income' : 'expense'" />
          </template>
          <label>{{ form.kind === 'transfer' ? 'Откуда' : form.kind === 'income' ? 'Куда' : 'Счёт' }}</label>
          <select v-model.number="form.account_id">
            <option :value="0">не указан</option>
            <option v-for="a in refs?.accounts ?? []" :key="a.id" :value="a.id">
              {{ a.name }} ({{ a.currency.toUpperCase() }})
            </option>
          </select>
          <template v-if="form.kind === 'transfer'">
            <label>Куда</label>
            <select v-model.number="form.to_account_id">
              <option :value="0">не указан</option>
              <option v-for="a in (refs?.accounts ?? []).filter((x) => x.id !== form!.account_id)"
                      :key="a.id" :value="a.id">
                {{ a.name }} ({{ a.currency.toUpperCase() }})
              </option>
            </select>
            <p class="hint">Перевод — не расход: в отчёт он не попадает.</p>
          </template>
          <template v-else>
            <label>Магазин / место</label>
            <input v-model="form.merchant" placeholder="Ереван Сити" />
          </template>
          <label>Заметка</label>
          <input v-model="form.note" />
          <div class="modal-acts">
            <button class="btn" @click="form = null">Отмена</button>
            <button class="btn primary" :disabled="busy" @click="save">Сохранить</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.head {
  display: flex;
  gap: 8px;
}

.grow {
  flex: 1;
}

.filters {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(130px, 1fr));
  gap: 6px;
  margin: 8px 0 6px;
}

.filters select,
.search {
  width: 100%;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 13px;
  padding: 8px 9px;
  backdrop-filter: var(--card-blur);
}

.search {
  margin-bottom: 8px;
}

.day {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 12px 2px 5px;
}

.day-sum {
  font-variant-numeric: tabular-nums;
}

.row {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  background: var(--card-color);
  border-radius: 10px;
  padding: 9px 12px;
  margin-bottom: 6px;
  cursor: pointer;
  backdrop-filter: var(--card-blur);
}

.row-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.name {
  font-size: 14px;
  overflow-wrap: anywhere;
}

.meta {
  font-size: 11px;
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}

.row-right {
  display: flex;
  align-items: center;
  gap: 8px;
  white-space: nowrap;
}

.amount {
  font-size: 14px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.amount.income {
  color: #22c55e;
}

.amount.transfer {
  color: var(--text-secondary);
}

.kinds {
  display: flex;
  gap: 6px;
  margin-bottom: 6px;
}

.kinds button {
  flex: 1;
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 13px;
  padding: 8px;
  cursor: pointer;
}

.kinds button.on {
  background: var(--accent-color);
  color: #fff;
}

.hint {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 10px 0;
}

.mini {
  background: var(--bg-color);
  border: none;
  border-radius: 6px;
  color: var(--text-color);
  font-size: 12px;
  padding: 4px 7px;
  cursor: pointer;
}

.mini.danger {
  color: #ef4444;
}

.btn {
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 14px;
  padding: 10px 14px;
  cursor: pointer;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn.wide {
  width: 100%;
  margin-top: 8px;
}

.modal {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: 20px 12px;
  overflow-y: auto;
  z-index: 1300;
}

.modal-box {
  background: var(--bg-color);
  border-radius: 12px;
  padding: 14px;
  width: 100%;
  max-width: 460px;
}

.modal-box h3 {
  margin: 0 0 10px;
  font-size: 16px;
}

.modal-box label {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 8px 0 4px;
}

.modal-box input,
.modal-box :deep(select) {
  width: 100%;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 15px;
  padding: 9px 10px;
}

.two {
  display: flex;
  gap: 8px;
}

.two > div {
  flex: 1;
}

.modal-acts {
  display: flex;
  gap: 8px;
  margin-top: 14px;
}

.modal-acts .btn {
  flex: 1;
}
</style>
