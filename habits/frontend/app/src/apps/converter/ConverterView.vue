<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { showToast } from '../../shared/toast'
import * as converterApi from './api'
import type { AvailableCurrency, RateSeries } from './api'
import RateChart from './components/RateChart.vue'

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

// --- справочник источника: фиат и криптовалюты ---
const available = ref<AvailableCurrency[]>([])
const availableLoading = ref(false)
const pickerQ = ref('')
const pickerKind = ref<'all' | 'fiat' | 'crypto'>('all')

const pickerHits = computed(() => {
  const q = pickerQ.value.trim().toLowerCase()
  const have = new Set(currencies.value)
  return available.value
    .filter((c) => pickerKind.value === 'all'
      || (pickerKind.value === 'crypto' ? c.crypto : !c.crypto))
    .filter((c) => !have.has(c.code) && c.code !== base.value)
    .filter((c) => !q || c.code.includes(q) || c.name.toLowerCase().includes(q))
    // список источника — полторы тысячи позиций, показываем первые попавшие
    .slice(0, 60)
})

async function openPicker() {
  addModal.value = true
  if (available.value.length || availableLoading.value) return
  availableLoading.value = true
  try {
    available.value = (await converterApi.fetchAvailable()).currencies
  } catch {
    showToast('Справочник валют недоступен — код можно ввести руками')
  } finally {
    availableLoading.value = false
  }
}

// --- графики за месяц ---
// Сразу не грузим: сервер докачивает недостающие дни из источника, а это
// десятки запросов. Пусть их вызывает нажатие, а не открытие страницы.
const chartsOpen = ref(false)
const chartsLoading = ref(false)
const series = ref<RateSeries[]>([])

async function toggleCharts() {
  chartsOpen.value = !chartsOpen.value
  if (!chartsOpen.value || series.value.length) return
  await loadCharts()
}

async function loadCharts() {
  const targets = currencies.value.filter((c) => c !== base.value)
  if (!targets.length) return
  chartsLoading.value = true
  try {
    series.value = (await converterApi.fetchHistory(base.value, targets, 30)).series
  } catch {
    showToast('Не удалось загрузить историю курсов')
  } finally {
    chartsLoading.value = false
  }
}

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
  // графики построены к прежней базе — сбрасываем, иначе покажут не тот курс
  series.value = []
  if (chartsOpen.value) void loadCharts()
  await refreshRates()
  recompute(base.value, amounts.value[base.value] || '1')
}

/** Добавление из списка — тот же путь, что и ручной ввод кода. */
async function pick(code: string) {
  newCode.value = code
  await addCurrency()
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
    pickerQ.value = ''
    if (chartsOpen.value) series.value = []
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
    series.value = series.value.filter((s) => s.code !== code)
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
    <button class="add-btn" @click="openPicker">➕</button>
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

    <button
      v-if="currencies.filter((c) => c !== base).length > 0"
      class="charts-btn"
      @click="toggleCharts"
    >
      {{ chartsOpen ? '▾' : '▸' }} График за месяц
    </button>

    <div v-if="chartsOpen" class="charts">
      <p v-if="chartsLoading" class="hint">
        Собираю историю… первый раз это дольше: сервер догружает недостающие дни.
      </p>
      <RateChart
        v-for="s in series"
        :key="s.code"
        :code="s.code"
        :base="base"
        :days="s.days"
        :rates="s.rates"
      />
      <p v-if="!chartsLoading && series.length === 0" class="hint">Истории пока нет</p>
    </div>
  </template>

  <!-- Модалка добавления валюты -->
  <div v-if="addModal" class="modal" @click.self="addModal = false">
    <div class="modal-content">
      <h3>Добавить валюту</h3>
      <input
        v-model="pickerQ"
        placeholder="Поиск: доллар, btc, tether…"
        @keyup.enter="pickerHits.length === 1 && pick(pickerHits[0].code)"
      />
      <div class="kinds">
        <button :class="{ on: pickerKind === 'all' }" @click="pickerKind = 'all'">Все</button>
        <button :class="{ on: pickerKind === 'fiat' }" @click="pickerKind = 'fiat'">Обычные</button>
        <button :class="{ on: pickerKind === 'crypto' }" @click="pickerKind = 'crypto'">Крипто</button>
      </div>
      <p v-if="availableLoading" class="hint">Загружаю справочник…</p>
      <div v-else class="picker">
        <button v-for="c in pickerHits" :key="c.code" class="pick-row" @click="pick(c.code)">
          <span class="pick-code">{{ flag(c.code) }} {{ c.code.toUpperCase() }}</span>
          <span class="pick-name">{{ c.name }}</span>
          <span v-if="c.crypto" class="pick-tag">крипто</span>
        </button>
        <p v-if="pickerHits.length === 0" class="hint">Ничего не нашлось</p>
      </div>
      <input
        v-model="newCode"
        placeholder="…или код вручную: eur, rub, btc"
        maxlength="10"
        @keyup.enter="addCurrency"
      />
      <button class="btn primary" @click="addCurrency">Добавить по коду</button>
      <button class="btn" @click="addModal = false">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.charts-btn {
  width: 100%;
  margin-top: 12px;
  padding: 10px;
  background: var(--card-color);
  color: var(--text-color);
  border: 0;
  border-radius: 10px;
  font: inherit;
  text-align: left;
}

.charts {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 8px;
}

.kinds {
  display: flex;
  gap: 6px;
  margin: 8px 0;
}

.kinds button {
  flex: 1;
  padding: 7px;
  border-radius: 8px;
  border: 1px solid var(--border-color);
  background: var(--bg-secondary);
  color: var(--text-color);
  font: inherit;
  font-size: 13px;
}

.kinds button.on {
  background: var(--accent-color);
  border-color: var(--accent-color);
  color: #fff;
}

/* Справочник источника — полторы тысячи позиций: список скроллится,
   а показываем только первые совпадения. */
.picker {
  max-height: 44vh;
  overflow-y: auto;
  margin-bottom: 8px;
}

.pick-row {
  display: flex;
  align-items: baseline;
  gap: 8px;
  width: 100%;
  padding: 9px 6px;
  background: none;
  border: 0;
  border-bottom: 1px solid var(--border-color);
  color: var(--text-color);
  font: inherit;
  text-align: left;
}

.pick-code {
  flex: none;
  font-weight: 600;
  min-width: 84px;
}

.pick-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
  color: var(--text-secondary);
}

.pick-tag {
  flex: none;
  font-size: 11px;
  color: var(--accent-color);
}

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
