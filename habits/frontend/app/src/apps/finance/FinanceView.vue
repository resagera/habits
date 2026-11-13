<script setup lang="ts">
// Finance — плановые траты с оплатой и напоминаниями + долги в обе стороны.
//
// Будущие платежи не хранятся: сервер раскладывает план по правилу повтора и
// отдаёт готовые вхождения по корзинам (просрочено / этот месяц / следующий /
// позже). Суммы в сводке приведены к базовой валюте курсом с сервера, поэтому
// цифра в приложении и в уведомлении бота совпадают.
import { computed, ref } from 'vue'
import { confirmAction } from '../../shared/telegram'
import { showToast } from '../../shared/toast'
import {
  addDebtPayment, archiveDebt, archivePlan, createDebt, createPlan, deleteDebt,
  deletePlan, fetchOverview, fetchPayments, fetchPlans, fetchRefs, payPlan,
  saveFinanceSettings, setDebtStatus, skipPlan, updateDebt, updatePlan,
} from './api'
import {
  fmtDate, fmtMoney, REPEAT_LABELS, repeatShort, todayStr,
  type FinanceDebt, type FinanceOverview, type FinancePayment, type FinancePlan,
  type FinanceRefs, type Occurrence, type RepeatKind,
} from './types'
import AccountsTab from './components/AccountsTab.vue'
import ItemsTab from './components/ItemsTab.vue'
import CategoryPicker from './components/CategoryPicker.vue'
import StatsTab from './components/StatsTab.vue'
import TxTab from './components/TxTab.vue'

const tab = ref<'plans' | 'tx' | 'accounts' | 'debts' | 'stats' | 'items'>('plans')
// справочники (категории, счета, цели) нужны формам всех вкладок
const refs = ref<FinanceRefs | null>(null)
// счётчик перезагрузки отчёта: он тянет свои данные и о правках не знает
const statsKey = ref(0)
const loading = ref(true)
const busy = ref(false)
const data = ref<FinanceOverview | null>(null)
// вхождение в списке — это не план целиком: в нём нет напоминания, паузы и
// признака «включён». Для формы редактирования держим сами планы.
const plans = ref<FinancePlan[]>([])
const archivedPlans = ref<FinancePlan[] | null>(null)
const archiveOpen = ref(false)
const hide = ref(false)

const BUCKETS: { key: keyof FinanceOverview['buckets']; title: string }[] = [
  { key: 'overdue', title: '⚠️ Просрочено' },
  { key: 'this_month', title: 'Этот месяц' },
  { key: 'next_month', title: 'Следующий месяц' },
  { key: 'later', title: 'Позже' },
]

const base = computed(() => data.value?.base_currency ?? 'amd')

async function load() {
  try {
    const [ov, pl, rf] = await Promise.all([fetchOverview(), fetchPlans(), fetchRefs()])
    data.value = ov
    plans.value = pl.plans
    refs.value = rf
    hide.value = ov.hide_amounts
  } catch {
    showToast('Не удалось загрузить финансы')
  } finally {
    loading.value = false
  }
}
void load()

/** Данные изменились где-то во вкладке: обновляем сводку, справочники и отчёт. */
async function reloadAll() {
  await load()
  statsKey.value++
}

/** Скрыть суммы — на случай, когда экран видит кто-то ещё. */
async function toggleHide() {
  hide.value = !hide.value
  try {
    await saveFinanceSettings({ hide_amounts: hide.value })
  } catch {
    /* не критично: следующий заход просто покажет прежнее состояние */
  }
}

function money(v: number, cur: string): string {
  return hide.value ? '•••' : fmtMoney(v, cur)
}

// --- планы ---

const planForm = ref<{
  id: number | null
  name: string
  amount: string
  currency: string
  is_estimate: boolean
  next_due_date: string
  repeat_kind: RepeatKind
  interval_n: number
  category_id: number | null
  account_id: number
  note: string
  autopay: boolean
  notify_days_before: number
} | null>(null)

function openPlan(p?: FinancePlan) {
  planForm.value = {
    id: p?.id ?? null,
    name: p?.name ?? '',
    amount: p ? String(p.amount) : '',
    currency: p?.currency ?? base.value,
    is_estimate: p?.is_estimate ?? false,
    next_due_date: p?.next_due_date?.slice(0, 10) ?? todayStr(),
    repeat_kind: p?.repeat_kind ?? 'monthly',
    interval_n: p?.interval_n ?? 1,
    category_id: p?.category_id ?? null,
    account_id: p?.account_id ?? 0,
    note: p?.note ?? '',
    autopay: p?.autopay ?? false,
    notify_days_before: p?.notify_days_before ?? 1,
  }
}

/** Открыть форму по id: берём план целиком, а не вхождение из списка. */
function editPlan(planId: number) {
  const p = plans.value.find((x) => x.id === planId)
  if (p) openPlan(p)
}

async function savePlan() {
  const f = planForm.value
  if (!f) return
  if (!f.name.trim()) {
    showToast('Впишите название')
    return
  }
  const { id, ...rest } = f
  const body = {
    ...rest, amount: Number(f.amount) || 0, account_id: f.account_id || null,
  }
  busy.value = true
  try {
    if (id) await updatePlan(id, body)
    else await createPlan(body)
    planForm.value = null
    await reloadAll()
    showToast('Сохранено ✅')
  } catch {
    showToast('Не удалось сохранить')
  } finally {
    busy.value = false
  }
}

// --- оплата ---

const payForm = ref<{ occ: Occurrence; date: string; amount: string; periods: number } | null>(null)
const payHistory = ref<FinancePayment[]>([])

async function openPay(occ: Occurrence) {
  payForm.value = { occ, date: todayStr(), amount: String(occ.amount), periods: 1 }
  payHistory.value = []
  try {
    const res = await fetchPayments(occ.plan_id)
    payHistory.value = res.payments.slice(0, 5)
    // у примерных трат подставляем среднее последних платежей: ЖКХ каждый
    // раз приходит другой суммой, и план — лишь ориентир
    if (occ.is_estimate && res.average && payForm.value) {
      payForm.value.amount = String(res.average)
    }
  } catch {
    /* история не критична для оплаты */
  }
}

async function confirmPay() {
  const f = payForm.value
  if (!f) return
  busy.value = true
  try {
    await payPlan(f.occ.plan_id, {
      paid_on: f.date,
      amount: Number(f.amount) || 0,
      periods: f.periods,
    })
    payForm.value = null
    await reloadAll()
    showToast('Оплачено ✅')
  } catch {
    showToast('Не удалось отметить оплату')
  } finally {
    busy.value = false
  }
}

async function doSkip(occ: Occurrence) {
  if (!(await confirmAction(`Пропустить платёж «${occ.name}»? Дата сдвинется, деньги не учтутся.`))) return
  try {
    await skipPlan(occ.plan_id)
    await load()
    showToast('Пропущено')
  } catch {
    showToast('Не удалось пропустить')
  }
}

async function toArchive(planId: number, name: string) {
  if (!(await confirmAction(`Убрать «${name}» в архив?`))) return
  await archivePlan(planId, true)
  archivedPlans.value = null
  await load()
}

async function fromArchive(p: FinancePlan) {
  await archivePlan(p.id, false)
  archivedPlans.value = null
  await load()
  await loadArchive()
}

async function removePlan(p: FinancePlan) {
  if (!(await confirmAction(`Удалить «${p.name}» вместе с историей платежей?`))) return
  await deletePlan(p.id)
  archivedPlans.value = null
  await load()
  await loadArchive()
}

async function loadArchive() {
  if (!archiveOpen.value) return
  try {
    archivedPlans.value = (await fetchPlans(true)).plans
  } catch {
    archivedPlans.value = []
  }
}

async function toggleArchive() {
  archiveOpen.value = !archiveOpen.value
  if (archiveOpen.value && !archivedPlans.value) await loadArchive()
}

// --- долги ---

const debts = computed(() => data.value?.debts ?? [])
const debtForm = ref<{
  id: number | null
  direction: 'owed_to_me' | 'i_owe'
  person: string
  subject: string
  amount: string
  currency: string
  expected_on: string
  note: string
} | null>(null)

function openDebt(d?: FinanceDebt) {
  debtForm.value = {
    id: d?.id ?? null,
    direction: d?.direction ?? 'owed_to_me',
    person: d?.person ?? '',
    subject: d?.subject ?? '',
    amount: d ? String(d.amount) : '',
    currency: d?.currency ?? base.value,
    expected_on: d?.expected_on?.slice(0, 10) ?? '',
    note: d?.note ?? '',
  }
}

async function saveDebt() {
  const f = debtForm.value
  if (!f) return
  if (!f.person.trim() || !Number(f.amount)) {
    showToast('Нужны имя и сумма')
    return
  }
  busy.value = true
  try {
    const { id, ...rest } = f
    const body = { ...rest, amount: Number(f.amount) }
    if (id) await updateDebt(id, body)
    else await createDebt(body)
    debtForm.value = null
    await load()
    showToast('Сохранено ✅')
  } catch {
    showToast('Не удалось сохранить')
  } finally {
    busy.value = false
  }
}

const returnForm = ref<{ debt: FinanceDebt; amount: string; date: string } | null>(null)

function openReturn(d: FinanceDebt) {
  returnForm.value = { debt: d, amount: String(d.remaining), date: todayStr() }
}

async function confirmReturn() {
  const f = returnForm.value
  if (!f) return
  const amount = Number(f.amount)
  if (!amount || amount <= 0) {
    showToast('Введите сумму')
    return
  }
  busy.value = true
  try {
    await addDebtPayment(f.debt.id, { amount, paid_on: f.date })
    returnForm.value = null
    await load()
    showToast('Записано ✅')
  } catch {
    showToast('Не удалось записать')
  } finally {
    busy.value = false
  }
}

async function forgive(d: FinanceDebt) {
  if (!(await confirmAction(`Простить остаток долга «${d.person}»?`))) return
  await setDebtStatus(d.id, 'forgiven')
  await load()
}

async function reopen(d: FinanceDebt) {
  await setDebtStatus(d.id, 'open')
  await load()
}

async function hideDebt(d: FinanceDebt) {
  await archiveDebt(d.id, true)
  await load()
}

async function removeDebt(d: FinanceDebt) {
  if (!(await confirmAction(`Удалить долг «${d.person}» вместе с историей возвратов?`))) return
  await deleteDebt(d.id)
  await load()
}

const DEBT_STATUS: Record<string, string> = {
  open: 'открыт',
  partial: 'частично',
  paid: 'закрыт',
  forgiven: 'прощён',
}
</script>

<template>
  <div class="fin">
    <p v-if="loading" class="hint">Загрузка…</p>

    <template v-else-if="data">
      <!-- сводка -->
      <div class="cards">
        <div class="card">
          <span class="lbl">Этот месяц</span>
          <b>{{ money(data.summary.this_month, base) }}</b>
          <span v-if="data.summary.autopay_this_month" class="sub">
            из них спишется само: {{ money(data.summary.autopay_this_month, base) }}
          </span>
        </div>
        <div class="card">
          <span class="lbl">Потрачено (факт)</span>
          <b>{{ money(data.summary.spent_this_month, base) }}</b>
          <span v-if="data.summary.earned_this_month" class="sub">
            получено: {{ money(data.summary.earned_this_month, base) }}
          </span>
        </div>
        <div class="card" :class="{ warn: data.summary.overdue > 0 }">
          <span class="lbl">Просрочено</span>
          <b>{{ money(data.summary.overdue, base) }}</b>
        </div>
        <div class="card">
          <span class="lbl">На счетах</span>
          <b>{{ money(data.summary.accounts_total, base) }}</b>
          <span class="sub">следующий месяц: {{ money(data.summary.next_month, base) }}</span>
        </div>
        <div v-if="data.summary.debts_owed_to_me || data.summary.debts_i_owe" class="card">
          <span class="lbl">Нам должны</span>
          <b>{{ money(data.summary.debts_owed_to_me, base) }}</b>
          <span v-if="data.summary.debts_i_owe" class="sub">
            я должен: {{ money(data.summary.debts_i_owe, base) }}
          </span>
        </div>
      </div>
      <p class="hint mono">
        В среднем в месяц: {{ money(data.summary.monthly_normalized, base) }}
        <button class="eye" :title="hide ? 'Показать суммы' : 'Скрыть суммы'" @click="toggleHide">
          {{ hide ? '🙈' : '👁' }}
        </button>
      </p>

      <div class="tabs">
        <button :class="{ on: tab === 'plans' }" @click="tab = 'plans'">Платежи</button>
        <button :class="{ on: tab === 'tx' }" @click="tab = 'tx'">Траты</button>
        <button :class="{ on: tab === 'accounts' }" @click="tab = 'accounts'">Счета</button>
        <button :class="{ on: tab === 'debts' }" @click="tab = 'debts'">
          Долги<span v-if="debts.length"> ({{ debts.length }})</span>
        </button>
        <button :class="{ on: tab === 'stats' }" @click="tab = 'stats'">Отчёт</button>
        <button :class="{ on: tab === 'items' }" @click="tab = 'items'">Товары</button>
      </div>

      <!-- ТРАТЫ / СЧЕТА / ОТЧЁТ -->
      <TxTab v-if="tab === 'tx'" :refs="refs" :hide="hide"
             @changed="reloadAll" @refs-changed="reloadAll" />
      <AccountsTab v-else-if="tab === 'accounts'" :refs="refs" :hide="hide" @changed="reloadAll" />
      <StatsTab v-else-if="tab === 'stats'" :hide="hide" :reload-key="statsKey"
                @picked="tab = 'items'" />
      <ItemsTab v-else-if="tab === 'items'" :refs="refs" :hide="hide" @changed="reloadAll" />

      <!-- ПЛАТЕЖИ -->
      <template v-else-if="tab === 'plans'">
        <button class="btn primary wide" @click="openPlan()">＋ Плановая трата</button>

        <template v-for="b in BUCKETS" :key="b.key">
          <template v-if="data.buckets[b.key].length">
            <h3 class="sect">{{ b.title }}</h3>
            <div v-for="(o, i) in data.buckets[b.key]" :key="`${o.plan_id}-${o.due_date}-${i}`"
                 class="row" :class="{ overdue: o.overdue }">
              <div class="row-main">
                <span class="name">{{ o.name }}</span>
                <span class="meta">
                  {{ fmtDate(o.due_date) }} · {{ repeatShort(o.repeat_kind, o.interval_n) }}
                  <span v-if="o.autopay" title="Автоплатёж">· 🔄</span>
                  <span v-if="o.category">· {{ o.category }}</span>
                </span>
              </div>
              <div class="row-right">
                <span class="amount">
                  <span v-if="o.is_estimate" title="Примерная сумма">≈</span>{{ money(o.amount, o.currency) }}
                </span>
                <div v-if="o.first" class="acts">
                  <button class="mini primary" @click="openPay(o)">Оплатил</button>
                  <button class="mini" title="Пропустить" @click="doSkip(o)">⤼</button>
                  <button class="mini" title="Изменить" @click="editPlan(o.plan_id)">✎</button>
                </div>
              </div>
            </div>
          </template>
        </template>

        <p v-if="!data.buckets.overdue.length && !data.buckets.this_month.length
                 && !data.buckets.next_month.length && !data.buckets.later.length" class="hint">
          Плановых трат пока нет. Добавьте подписки, аренду, ЖКХ — приложение напомнит заранее.
        </p>

        <button class="link" @click="toggleArchive">
          {{ archiveOpen ? '▾' : '▸' }} Архив и выключенные
        </button>
        <template v-if="archiveOpen">
          <div v-for="p in archivedPlans ?? []" :key="p.id" class="row muted">
            <div class="row-main">
              <span class="name">{{ p.name }}</span>
              <span class="meta">{{ money(p.amount, p.currency) }} · {{ repeatShort(p.repeat_kind, p.interval_n) }}</span>
            </div>
            <div class="acts">
              <button class="mini" @click="fromArchive(p)">Вернуть</button>
              <button class="mini danger" @click="removePlan(p)">Удалить</button>
            </div>
          </div>
          <p v-if="archivedPlans && !archivedPlans.length" class="hint">Архив пуст.</p>
        </template>
      </template>

      <!-- ДОЛГИ -->
      <template v-else>
        <button class="btn primary wide" @click="openDebt()">＋ Долг</button>
        <p v-if="data.summary.debts_overdue" class="hint warn-text">
          Просрочено возвратов: {{ money(data.summary.debts_overdue, base) }}
        </p>

        <div v-for="d in debts" :key="d.id" class="row"
             :class="{ overdue: d.expected_on && d.expected_on.slice(0, 10) < data.today && d.remaining > 0 }">
          <div class="row-main">
            <span class="name">
              {{ d.direction === 'i_owe' ? '↗' : '↘' }} {{ d.person }}
              <span v-if="d.subject" class="meta">— {{ d.subject }}</span>
            </span>
            <span class="meta">
              {{ DEBT_STATUS[d.status] }}
              <span v-if="d.paid"> · вернули {{ money(d.paid, d.currency) }}</span>
              <span v-if="d.expected_on"> · до {{ fmtDate(d.expected_on) }}</span>
            </span>
          </div>
          <div class="row-right">
            <span class="amount">
              {{ money(d.remaining, d.currency) }}
              <span v-if="d.remaining !== d.amount" class="meta">из {{ money(d.amount, d.currency) }}</span>
            </span>
            <div class="acts">
              <button v-if="d.remaining > 0" class="mini primary" @click="openReturn(d)">
                {{ d.direction === 'i_owe' ? 'Вернул' : 'Вернули' }}
              </button>
              <button v-if="d.remaining > 0" class="mini" title="Простить остаток" @click="forgive(d)">🙏</button>
              <button v-else class="mini" title="Вернуть в работу" @click="reopen(d)">↺</button>
              <button class="mini" title="Изменить" @click="openDebt(d)">✎</button>
              <button class="mini" title="В архив" @click="hideDebt(d)">🗄</button>
              <button class="mini danger" title="Удалить" @click="removeDebt(d)">✕</button>
            </div>
          </div>
        </div>
        <p v-if="!debts.length" class="hint">Долгов нет — ни нам, ни мы.</p>
      </template>
    </template>

    <!-- модалка плана -->
    <div v-if="planForm" class="modal" @click.self="planForm = null">
      <div class="modal-box">
        <h3>{{ planForm.id ? 'Плановая трата' : 'Новая плановая трата' }}</h3>
        <label>Название</label>
        <input v-model="planForm.name" placeholder="Netflix, ЖКХ, аренда" />
        <div class="two">
          <div>
            <label>Сумма</label>
            <input v-model="planForm.amount" type="number" step="0.01" inputmode="decimal" />
          </div>
          <div>
            <label>Валюта</label>
            <input v-model="planForm.currency" placeholder="amd" />
          </div>
        </div>
        <label class="chk">
          <input v-model="planForm.is_estimate" type="checkbox" />
          <span>Сумма примерная (как ЖКХ) — при оплате подставим среднюю</span>
        </label>
        <label>Следующий платёж</label>
        <input v-model="planForm.next_due_date" type="date" />
        <label>Повтор</label>
        <select v-model="planForm.repeat_kind">
          <option v-for="(t, k) in REPEAT_LABELS" :key="k" :value="k">{{ t }}</option>
        </select>
        <div v-if="planForm.repeat_kind === 'interval_months' || planForm.repeat_kind === 'interval_days'">
          <label>Интервал</label>
          <input v-model.number="planForm.interval_n" type="number" min="1" max="365" />
        </div>
        <label>Категория</label>
        <CategoryPicker v-model="planForm.category_id" :categories="refs?.categories ?? []"
                        kind="expense" />
        <label>Счёт списания</label>
        <select v-model.number="planForm.account_id">
          <option :value="0">не указан</option>
          <option v-for="a in refs?.accounts ?? []" :key="a.id" :value="a.id">
            {{ a.name }} ({{ a.currency.toUpperCase() }})
          </option>
        </select>
        <p class="hint">
          Оплата этого платежа попадёт в «Траты» с этой категорией и счётом —
          отчёт считается по всем деньгам сразу.
        </p>
        <label class="chk">
          <input v-model="planForm.autopay" type="checkbox" />
          <span>Автоплатёж — не напоминать заранее</span>
        </label>
        <div v-if="!planForm.autopay">
          <label>Напомнить за (дней)</label>
          <input v-model.number="planForm.notify_days_before" type="number" min="0" max="30" />
        </div>
        <label>Заметка</label>
        <input v-model="planForm.note" />
        <div class="modal-acts">
          <button class="btn" @click="planForm = null">Отмена</button>
          <button v-if="planForm.id" class="btn" @click="toArchive(planForm.id, planForm.name)">В архив</button>
          <button class="btn primary" :disabled="busy" @click="savePlan">Сохранить</button>
        </div>
      </div>
    </div>

    <!-- модалка оплаты -->
    <div v-if="payForm" class="modal" @click.self="payForm = null">
      <div class="modal-box">
        <h3>Оплата · {{ payForm.occ.name }}</h3>
        <label>Дата платежа</label>
        <input v-model="payForm.date" type="date" />
        <label>Сумма ({{ payForm.occ.currency.toUpperCase() }})</label>
        <input v-model="payForm.amount" type="number" step="0.01" inputmode="decimal" />
        <p v-if="payForm.occ.is_estimate" class="hint">
          Сумма примерная — подставлено среднее последних платежей, поправьте по факту.
        </p>
        <template v-if="payForm.occ.repeat_kind !== 'once'">
          <label>Оплачено периодов вперёд</label>
          <input v-model.number="payForm.periods" type="number" min="1" max="120" />
          <p class="hint">
            Например, заплатили за 3 месяца вперёд при ежемесячном платеже — поставьте 3.
          </p>
        </template>
        <div v-if="payHistory.length" class="hist">
          <span class="lbl">Последние платежи</span>
          <div v-for="p in payHistory" :key="p.id" class="hist-row">
            <span>{{ fmtDate(p.paid_on) }}</span>
            <span>{{ p.skipped ? 'пропущен' : money(p.amount, p.currency) }}</span>
          </div>
        </div>
        <div class="modal-acts">
          <button class="btn" @click="payForm = null">Отмена</button>
          <button class="btn primary" :disabled="busy" @click="confirmPay">Оплатил</button>
        </div>
      </div>
    </div>

    <!-- модалка долга -->
    <div v-if="debtForm" class="modal" @click.self="debtForm = null">
      <div class="modal-box">
        <h3>{{ debtForm.id ? 'Долг' : 'Новый долг' }}</h3>
        <label>Направление</label>
        <select v-model="debtForm.direction">
          <option value="owed_to_me">Мне должны</option>
          <option value="i_owe">Я должен</option>
        </select>
        <label>Кто</label>
        <input v-model="debtForm.person" placeholder="Имя" />
        <label>За что</label>
        <input v-model="debtForm.subject" placeholder="За билеты" />
        <div class="two">
          <div>
            <label>Сумма</label>
            <input v-model="debtForm.amount" type="number" step="0.01" inputmode="decimal" />
          </div>
          <div>
            <label>Валюта</label>
            <input v-model="debtForm.currency" placeholder="amd" />
          </div>
        </div>
        <label>Когда ждём возврат</label>
        <input v-model="debtForm.expected_on" type="date" />
        <label>Заметка</label>
        <input v-model="debtForm.note" />
        <div class="modal-acts">
          <button class="btn" @click="debtForm = null">Отмена</button>
          <button class="btn primary" :disabled="busy" @click="saveDebt">Сохранить</button>
        </div>
      </div>
    </div>

    <!-- модалка возврата -->
    <div v-if="returnForm" class="modal" @click.self="returnForm = null">
      <div class="modal-box">
        <h3>Возврат · {{ returnForm.debt.person }}</h3>
        <label>Сумма ({{ returnForm.debt.currency.toUpperCase() }})</label>
        <input v-model="returnForm.amount" type="number" step="0.01" inputmode="decimal" />
        <p class="hint">Остаток: {{ money(returnForm.debt.remaining, returnForm.debt.currency) }}</p>
        <label>Дата</label>
        <input v-model="returnForm.date" type="date" />
        <div class="modal-acts">
          <button class="btn" @click="returnForm = null">Отмена</button>
          <button class="btn primary" :disabled="busy" @click="confirmReturn">Записать</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 8px;
}

.card {
  background: var(--card-color);
  border-radius: 10px;
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  backdrop-filter: var(--card-blur);
}

.card.warn b {
  color: #ef4444;
}

.card b {
  font-size: 17px;
}

.lbl {
  font-size: 12px;
  color: var(--text-secondary);
}

.sub {
  font-size: 11px;
  color: var(--text-secondary);
}

.hint {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 8px 0;
}

.hint.mono {
  display: flex;
  align-items: center;
  gap: 8px;
}

.warn-text {
  color: #ef4444;
}

.eye {
  background: none;
  border: none;
  cursor: pointer;
  font-size: 15px;
  padding: 0;
}

.tabs {
  display: flex;
  gap: 8px;
  margin: 10px 0;
}

.tabs button {
  flex: 1;
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 14px;
  padding: 8px;
  cursor: pointer;
}

.tabs button.on {
  background: var(--accent-color);
  color: #fff;
}

.sect {
  font-size: 14px;
  margin: 14px 0 6px;
  color: var(--text-secondary);
}

.row {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  background: var(--card-color);
  border-radius: 10px;
  padding: 10px 12px;
  margin-bottom: 8px;
  backdrop-filter: var(--card-blur);
}

.row.overdue {
  box-shadow: inset 3px 0 0 #ef4444;
}

.row.muted {
  opacity: 0.65;
}

.row-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.name {
  font-size: 15px;
  overflow-wrap: anywhere;
}

.meta {
  font-size: 12px;
  color: var(--text-secondary);
}

.row-right {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 6px;
  white-space: nowrap;
}

.amount {
  font-size: 15px;
  font-weight: 600;
}

.acts {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.mini {
  background: var(--bg-color);
  border: none;
  border-radius: 6px;
  color: var(--text-color);
  font-size: 12px;
  padding: 5px 8px;
  cursor: pointer;
}

.mini.primary {
  background: var(--accent-color);
  color: #fff;
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
  margin-bottom: 6px;
}

.link {
  background: none;
  border: none;
  color: var(--accent-color);
  font-size: 13px;
  padding: 10px 0;
  cursor: pointer;
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
  z-index: 1200;
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

.modal-box input:not([type='checkbox']),
.modal-box select {
  width: 100%;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 15px;
  padding: 9px 10px;
}

.chk {
  display: flex !important;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--text-color) !important;
  margin: 10px 0 !important;
}

.two {
  display: flex;
  gap: 8px;
}

.two > div {
  flex: 1;
}

.hist {
  margin-top: 12px;
  background: var(--card-color);
  border-radius: 8px;
  padding: 8px 10px;
}

.hist-row {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  color: var(--text-secondary);
  padding: 2px 0;
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
