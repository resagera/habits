<script setup lang="ts">
// Счета («откуда деньги») и цели «отложено на».
//
// Остаток счёта не хранится полем: он равен стартовому плюс движения — иначе
// неизбежно разъедется с историей. Цель — конверт поверх счёта: деньги
// физически лежат на счёте, поэтому «отложил» его баланс не меняет.
import { computed, ref } from 'vue'
import { confirmAction } from '../../../shared/telegram'
import { showToast } from '../../../shared/toast'
import {
  addGoalMove, archiveAccount, createAccount, createGoal, deleteAccount,
  deleteGoal, deleteGoalMove, fetchGoalMoves, updateAccount, updateGoal,
} from '../api'
import {
  ACCOUNT_ICONS, ACCOUNT_KINDS, fmtDate, fmtMoney, todayStr,
  type AccountKind, type FinanceAccount, type FinanceGoal, type FinanceGoalMove,
  type FinanceRefs,
} from '../types'

const props = defineProps<{ refs: FinanceRefs | null; hide: boolean }>()
const emit = defineEmits<{ changed: [] }>()

const busy = ref(false)
const accounts = computed(() => props.refs?.accounts ?? [])
const goals = computed(() => props.refs?.goals ?? [])
const base = computed(() => props.refs?.base_currency ?? 'amd')

function money(v: number, cur: string): string {
  return props.hide ? '•••' : fmtMoney(v, cur)
}

// --- счета ---

const accForm = ref<{
  id: number | null
  name: string
  kind: AccountKind
  currency: string
  start_balance: string
  include_in_total: boolean
  note: string
} | null>(null)

function openAccount(a?: FinanceAccount) {
  accForm.value = {
    id: a?.id ?? null,
    name: a?.name ?? '',
    kind: a?.kind ?? 'card',
    currency: a?.currency ?? base.value,
    start_balance: a ? String(a.start_balance) : '',
    include_in_total: a?.include_in_total ?? true,
    note: a?.note ?? '',
  }
}

async function saveAccount() {
  const f = accForm.value
  if (!f) return
  if (!f.name.trim()) {
    showToast('Впишите название')
    return
  }
  busy.value = true
  try {
    const { id, ...rest } = f
    const body = { ...rest, start_balance: Number(f.start_balance) || 0 }
    if (id) await updateAccount(id, body)
    else await createAccount(body)
    accForm.value = null
    emit('changed')
    showToast('Сохранено ✅')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось сохранить')
  } finally {
    busy.value = false
  }
}

async function removeAccount(a: FinanceAccount) {
  if (!(await confirmAction(`Удалить счёт «${a.name}»?`))) return
  try {
    await deleteAccount(a.id)
    accForm.value = null
    emit('changed')
  } catch (e) {
    // счёт с движениями удалить нельзя — предлагаем архив
    if (await confirmAction(`${e instanceof Error ? e.message : 'Не удалось удалить'}. Убрать в архив?`)) {
      await archiveAccount(a.id, true)
      accForm.value = null
      emit('changed')
    }
  }
}

// --- цели ---

const goalForm = ref<{
  id: number | null
  name: string
  target_amount: string
  currency: string
  account_id: number
  due_date: string
  note: string
} | null>(null)

function openGoal(g?: FinanceGoal) {
  goalForm.value = {
    id: g?.id ?? null,
    name: g?.name ?? '',
    target_amount: g ? String(g.target_amount) : '',
    currency: g?.currency ?? base.value,
    account_id: g?.account_id ?? 0,
    due_date: g?.due_date?.slice(0, 10) ?? '',
    note: g?.note ?? '',
  }
}

async function saveGoal() {
  const f = goalForm.value
  if (!f) return
  if (!f.name.trim() || !Number(f.target_amount)) {
    showToast('Нужны название и сумма')
    return
  }
  busy.value = true
  try {
    const { id, ...rest } = f
    const body = { ...rest, target_amount: Number(f.target_amount) }
    if (id) await updateGoal(id, body)
    else await createGoal(body)
    goalForm.value = null
    emit('changed')
    showToast('Сохранено ✅')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось сохранить')
  } finally {
    busy.value = false
  }
}

async function removeGoal(g: FinanceGoal) {
  if (!(await confirmAction(`Удалить цель «${g.name}» вместе с историей?`))) return
  await deleteGoal(g.id)
  goalForm.value = null
  emit('changed')
}

const moveForm = ref<{ goal: FinanceGoal; amount: string; date: string; sign: 1 | -1 } | null>(null)
const moves = ref<FinanceGoalMove[]>([])

async function openMove(g: FinanceGoal, sign: 1 | -1) {
  moveForm.value = { goal: g, amount: '', date: todayStr(), sign }
  moves.value = []
  try {
    moves.value = (await fetchGoalMoves(g.id)).moves.slice(0, 5)
  } catch {
    /* история не критична для записи */
  }
}

async function saveMove() {
  const f = moveForm.value
  if (!f) return
  const amount = Number(f.amount)
  if (!amount || amount <= 0) {
    showToast('Введите сумму')
    return
  }
  busy.value = true
  try {
    await addGoalMove(f.goal.id, { amount: amount * f.sign, moved_on: f.date })
    moveForm.value = null
    emit('changed')
  } catch {
    showToast('Не удалось записать')
  } finally {
    busy.value = false
  }
}

async function removeMove(m: FinanceGoalMove) {
  if (!moveForm.value) return
  try {
    await deleteGoalMove(moveForm.value.goal.id, m.id)
    moves.value = moves.value.filter((x) => x.id !== m.id)
    emit('changed')
  } catch {
    showToast('Не удалось удалить')
  }
}

function progress(g: FinanceGoal): number {
  return Math.max(0, Math.min(100, (g.saved / g.target_amount) * 100))
}

const savedTotal = computed(() => goals.value.reduce((s, g) => s + g.saved, 0))
</script>

<template>
  <div>
    <div class="total">
      <span class="lbl">Всего на счетах</span>
      <b>{{ money(refs?.totals.balance_base ?? 0, base) }}</b>
      <span v-if="savedTotal" class="sub">из них отложено: {{ money(savedTotal, base) }}</span>
    </div>

    <button class="btn primary wide" @click="openAccount()">＋ Счёт</button>

    <div v-for="a in accounts" :key="a.id" class="row" @click="openAccount(a)">
      <div class="row-main">
        <span class="name">{{ ACCOUNT_ICONS[a.kind] }} {{ a.name }}</span>
        <span class="meta">
          {{ ACCOUNT_KINDS[a.kind] }}
          <span v-if="!a.include_in_total"> · не в общем итоге</span>
          <span v-if="a.note"> · {{ a.note }}</span>
        </span>
      </div>
      <div class="row-right">
        <span class="amount" :class="{ minus: a.balance < 0 }">{{ money(a.balance, a.currency) }}</span>
        <span v-if="a.currency !== base" class="meta">≈ {{ money(a.balance_base, base) }}</span>
      </div>
    </div>
    <p v-if="!accounts.length" class="hint">
      Счетов нет. Заведите «Карта», «Наличные» — тогда у трат появится «откуда
      деньги», а здесь будут остатки.
    </p>

    <h3 class="sect">🎯 Отложено на</h3>
    <button class="btn wide" @click="openGoal()">＋ Цель</button>

    <div v-for="g in goals" :key="g.id" class="goal">
      <div class="goal-head">
        <span class="name">{{ g.name }}</span>
        <span class="amount">{{ money(g.saved, g.currency) }} / {{ money(g.target_amount, g.currency) }}</span>
      </div>
      <div class="bar"><i :style="{ width: progress(g) + '%' }" /></div>
      <div class="goal-foot">
        <span class="meta">
          {{ Math.round(progress(g)) }}%
          <span v-if="g.due_date"> · до {{ fmtDate(g.due_date) }}</span>
          <span v-if="g.saved >= g.target_amount"> · собрано 🎉</span>
        </span>
        <span class="acts">
          <button class="mini primary" @click="openMove(g, 1)">Отложить</button>
          <button class="mini" title="Снять" @click="openMove(g, -1)">Снять</button>
          <button class="mini" title="Изменить" @click="openGoal(g)">✎</button>
        </span>
      </div>
    </div>
    <p v-if="!goals.length" class="hint">
      Целей нет. Цель — конверт поверх счёта: деньги остаются на счёте, но видно,
      сколько из них уже занято.
    </p>

    <Teleport to="body">
      <!-- счёт -->
      <div v-if="accForm" class="modal" @click.self="accForm = null">
        <div class="modal-box">
          <h3>{{ accForm.id ? 'Счёт' : 'Новый счёт' }}</h3>
          <label>Название</label>
          <input v-model="accForm.name" placeholder="Карта Ameria" />
          <div class="two">
            <div>
              <label>Тип</label>
              <select v-model="accForm.kind">
                <option v-for="(t, k) in ACCOUNT_KINDS" :key="k" :value="k">{{ t }}</option>
              </select>
            </div>
            <div>
              <label>Валюта</label>
              <input v-model="accForm.currency" placeholder="amd" />
            </div>
          </div>
          <label>Стартовый остаток</label>
          <input v-model="accForm.start_balance" type="number" step="0.01" inputmode="decimal" />
          <p class="hint">
            Сколько лежит на счёте сейчас. Дальше остаток считается по записям.
          </p>
          <label class="chk">
            <input v-model="accForm.include_in_total" type="checkbox" />
            <span>Учитывать в общем итоге</span>
          </label>
          <label>Заметка</label>
          <input v-model="accForm.note" />
          <div class="modal-acts">
            <button class="btn" @click="accForm = null">Отмена</button>
            <button v-if="accForm.id" class="btn danger"
                    @click="removeAccount(accounts.find((x) => x.id === accForm!.id)!)">Удалить</button>
            <button class="btn primary" :disabled="busy" @click="saveAccount">Сохранить</button>
          </div>
        </div>
      </div>

      <!-- цель -->
      <div v-if="goalForm" class="modal" @click.self="goalForm = null">
        <div class="modal-box">
          <h3>{{ goalForm.id ? 'Цель' : 'Новая цель' }}</h3>
          <label>На что</label>
          <input v-model="goalForm.name" placeholder="Ноутбук, отпуск, подушка" />
          <div class="two">
            <div>
              <label>Сколько нужно</label>
              <input v-model="goalForm.target_amount" type="number" step="0.01" inputmode="decimal" />
            </div>
            <div>
              <label>Валюта</label>
              <input v-model="goalForm.currency" placeholder="amd" />
            </div>
          </div>
          <label>Где лежат</label>
          <select v-model.number="goalForm.account_id">
            <option :value="0">не указан</option>
            <option v-for="a in accounts" :key="a.id" :value="a.id">{{ a.name }}</option>
          </select>
          <label>К дате</label>
          <input v-model="goalForm.due_date" type="date" />
          <label>Заметка</label>
          <input v-model="goalForm.note" />
          <div class="modal-acts">
            <button class="btn" @click="goalForm = null">Отмена</button>
            <button v-if="goalForm.id" class="btn danger"
                    @click="removeGoal(goals.find((x) => x.id === goalForm!.id)!)">Удалить</button>
            <button class="btn primary" :disabled="busy" @click="saveGoal">Сохранить</button>
          </div>
        </div>
      </div>

      <!-- отложить / снять -->
      <div v-if="moveForm" class="modal" @click.self="moveForm = null">
        <div class="modal-box">
          <h3>{{ moveForm.sign > 0 ? 'Отложить' : 'Снять' }} · {{ moveForm.goal.name }}</h3>
          <label>Сумма ({{ moveForm.goal.currency.toUpperCase() }})</label>
          <input v-model="moveForm.amount" type="number" step="0.01" inputmode="decimal" />
          <p class="hint">
            Уже отложено: {{ money(moveForm.goal.saved, moveForm.goal.currency) }} из
            {{ money(moveForm.goal.target_amount, moveForm.goal.currency) }}
          </p>
          <label>Дата</label>
          <input v-model="moveForm.date" type="date" />
          <div v-if="moves.length" class="hist">
            <span class="lbl">Последние движения</span>
            <div v-for="m in moves" :key="m.id" class="hist-row">
              <span>{{ fmtDate(m.moved_on) }}</span>
              <span>
                {{ m.amount > 0 ? '+' : '−' }}{{ money(Math.abs(m.amount), moveForm.goal.currency) }}
                <button class="mini danger" title="Удалить" @click="removeMove(m)">✕</button>
              </span>
            </div>
          </div>
          <div class="modal-acts">
            <button class="btn" @click="moveForm = null">Отмена</button>
            <button class="btn primary" :disabled="busy" @click="saveMove">Записать</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.total {
  background: var(--card-color);
  border-radius: 10px;
  padding: 10px 12px;
  margin-bottom: 10px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  backdrop-filter: var(--card-blur);
}

.total b {
  font-size: 19px;
}

.lbl,
.sub {
  font-size: 12px;
  color: var(--text-secondary);
}

.sect {
  font-size: 14px;
  margin: 18px 0 6px;
  color: var(--text-secondary);
}

.row {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  background: var(--card-color);
  border-radius: 10px;
  padding: 10px 12px;
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

.row-right {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  white-space: nowrap;
}

.name {
  font-size: 15px;
  overflow-wrap: anywhere;
}

.meta {
  font-size: 11px;
  color: var(--text-secondary);
}

.amount {
  font-size: 15px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.amount.minus {
  color: #ef4444;
}

.goal {
  background: var(--card-color);
  border-radius: 10px;
  padding: 10px 12px;
  margin-bottom: 6px;
  backdrop-filter: var(--card-blur);
}

.goal-head,
.goal-foot {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
}

.goal-foot {
  margin-top: 6px;
}

.bar {
  height: 6px;
  border-radius: 3px;
  background: var(--bg-color);
  margin-top: 8px;
  overflow: hidden;
}

.bar i {
  display: block;
  height: 100%;
  background: var(--accent-color);
}

.acts {
  display: flex;
  gap: 4px;
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

.hint {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 10px 0;
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

.btn.danger {
  color: #ef4444;
}

.btn.wide {
  width: 100%;
  margin-bottom: 8px;
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
  align-items: center;
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
