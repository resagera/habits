<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { showToast } from '../../shared/toast'
import * as converterApi from './api'

const FLAGS: Record<string, string> = {
  usd: '🇺🇸', eur: '🇪🇺', rub: '🇷🇺', gbp: '🇬🇧', cny: '🇨🇳', jpy: '🇯🇵',
  try: '🇹🇷', kzt: '🇰🇿', uah: '🇺🇦', byn: '🇧🇾', amd: '🇦🇲', gel: '🇬🇪',
  thb: '🇹🇭', aed: '🇦🇪', inr: '🇮🇳', krw: '🇰🇷', chf: '🇨🇭', pln: '🇵🇱',
  czk: '🇨🇿', rsd: '🇷🇸', btc: '₿', eth: 'Ξ', usdt: '₮',
}

const COMMON_BASES = ['usd', 'eur', 'rub', 'gbp', 'cny', 'try', 'kzt', 'uah', 'amd', 'gel', 'thb', 'aed', 'btc']

const BASE_KEY = 'converter_base'

const base = ref(localStorage.getItem(BASE_KEY) || 'usd')
const currencies = ref<string[]>([])
const rates = ref<Record<string, number>>({})
const rateDate = ref('')
const amounts = ref<Record<string, string>>({})
const loading = ref(true)

const addModal = ref(false)
const newCode = ref('')
const confirmRemove = ref<string | null>(null)

const rows = computed(() => [base.value, ...currencies.value.filter((c) => c !== base.value)])

const baseOptions = computed(() => [...new Set([...COMMON_BASES, ...currencies.value])])

function flag(code: string): string {
  return FLAGS[code] ?? '💱'
}

function rate(code: string): number {
  return code === base.value ? 1 : (rates.value[code] ?? 0)
}

onMounted(load)

async function load() {
  loading.value = true
  try {
    currencies.value = (await converterApi.fetchCurrencies()).currencies
    await refreshRates()
    recompute(base.value, amounts.value[base.value] || '1')
  } catch {
    showToast('Не удалось загрузить валюты')
  } finally {
    loading.value = false
  }
}

async function refreshRates() {
  const targets = currencies.value.filter((c) => c !== base.value)
  if (targets.length === 0) {
    rates.value = {}
    return
  }
  try {
    const res = await converterApi.fetchRates(base.value, targets)
    rates.value = res.rates
    rateDate.value = res.date
  } catch {
    showToast('Не удалось получить курсы')
  }
}

/** Пересчёт всех строк от валюты from со значением raw. */
function recompute(from: string, raw: string) {
  amounts.value[from] = raw
  const value = parseFloat(raw.replace(',', '.'))
  if (isNaN(value) || rate(from) === 0) return
  const inBase = value / rate(from)
  for (const code of rows.value) {
    if (code === from) continue
    const r = rate(code)
    amounts.value[code] = r === 0 ? '' : format(inBase * r)
  }
}

function format(n: number): string {
  if (n === 0) return '0'
  if (Math.abs(n) >= 1000) return n.toFixed(0)
  if (Math.abs(n) >= 1) return n.toFixed(2)
  return n.toPrecision(4)
}

async function onBaseChange() {
  localStorage.setItem(BASE_KEY, base.value)
  await refreshRates()
  recompute(base.value, amounts.value[base.value] || '1')
}

async function addCurrency() {
  const code = newCode.value.trim().toLowerCase()
  if (!/^[a-z0-9]{2,10}$/.test(code)) {
    showToast('Код валюты: 2-10 латинских символов')
    return
  }
  try {
    await converterApi.addCurrency(code)
    if (!currencies.value.includes(code)) currencies.value.push(code)
    newCode.value = ''
    addModal.value = false
    await refreshRates()
    if (!(code in rates.value) && code !== base.value) {
      showToast(`Курс для «${code}» не найден`)
    } else {
      recompute(base.value, amounts.value[base.value] || '1')
    }
  } catch {
    showToast('Не удалось добавить валюту')
  }
}

async function removeCurrencyRow(code: string) {
  if (confirmRemove.value !== code) {
    confirmRemove.value = code
    setTimeout(() => {
      if (confirmRemove.value === code) confirmRemove.value = null
    }, 3000)
    return
  }
  confirmRemove.value = null
  try {
    await converterApi.removeCurrency(code)
    currencies.value = currencies.value.filter((c) => c !== code)
  } catch {
    showToast('Не удалось удалить')
  }
}
</script>

<template>
  <div class="controls">
    <select v-model="base" class="base-select" @change="onBaseChange">
      <option v-for="code in baseOptions" :key="code" :value="code">
        {{ flag(code) }} {{ code.toUpperCase() }} — базовая
      </option>
    </select>
    <button class="add-btn" @click="addModal = true">➕</button>
  </div>

  <p v-if="rateDate" class="rate-date">Курсы на {{ rateDate }} · обновляются раз в час</p>

  <div v-if="loading" class="hint">Загрузка…</div>

  <template v-else>
    <div v-for="code in rows" :key="code" class="currency-row" :class="{ base: code === base }">
      <span class="currency-code">{{ flag(code) }} {{ code.toUpperCase() }}</span>
      <input
        :value="amounts[code] ?? ''"
        inputmode="decimal"
        class="amount"
        :placeholder="rate(code) === 0 && code !== base ? 'нет курса' : '0'"
        @input="recompute(code, ($event.target as HTMLInputElement).value)"
      />
      <button
        v-if="code !== base"
        class="icon-btn"
        :class="{ confirming: confirmRemove === code }"
        @click="removeCurrencyRow(code)"
      >
        {{ confirmRemove === code ? 'точно?' : '✕' }}
      </button>
      <span v-else class="icon-btn base-mark" title="Базовая валюта">★</span>
    </div>

    <p v-if="currencies.filter((c) => c !== base).length === 0" class="hint">
      Добавьте валюты кнопкой ➕ — и конвертируйте в обе стороны
    </p>
  </template>

  <!-- Модалка добавления валюты -->
  <div v-if="addModal" class="modal" @click.self="addModal = false">
    <div class="modal-content">
      <h3>Добавить валюту</h3>
      <input
        v-model="newCode"
        placeholder="Код: eur, rub, btc…"
        maxlength="10"
        @keyup.enter="addCurrency"
      />
      <button class="btn primary" @click="addCurrency">Добавить</button>
      <button class="btn" @click="addModal = false">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.controls {
  display: flex;
  gap: 8px;
  margin-bottom: 8px;
}

.base-select {
  flex: 1;
  min-width: 0;
}

.add-btn {
  flex: none;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  padding: 0 14px;
  color: var(--text-color);
}

.rate-date {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 12px;
}

.hint {
  text-align: center;
  color: var(--text-secondary);
  padding: 24px 0;
}

.currency-row {
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--card-color);
  border-radius: 8px;
  padding: 8px 10px;
  margin-bottom: 8px;
}

.currency-row.base {
  border: 1px solid var(--accent-color);
}

.currency-code {
  flex: none;
  width: 92px;
  font-weight: 600;
}

.amount {
  flex: 1;
  min-width: 0;
  text-align: right;
  font-size: 16px;
}

.icon-btn {
  flex: none;
  background: none;
  border: none;
  color: var(--text-secondary);
  padding: 4px 6px;
}

.icon-btn.confirming {
  color: #ef4444;
  font-weight: 600;
  font-size: 12px;
}

.base-mark {
  color: var(--accent-color);
}

.modal-content input {
  width: 100%;
  margin-top: 8px;
}

.btn {
  display: block;
  width: 100%;
  margin-top: 10px;
  padding: 10px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}
</style>
