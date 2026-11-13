<script setup lang="ts">
// Отчёт: сколько ушло по месяцам и по группам.
//
// Суммы приводит к базовой валюте сервер — курсом, зафиксированным на дату
// траты, иначе прошлые месяцы «плыли» бы при каждом изменении курса.
import { computed, ref, watch } from 'vue'
import { showToast } from '../../../shared/toast'
import { fetchStats } from '../api'
import { fmtMoney, monthLabel, type CatStat, type FinanceStats } from '../types'
import PieChart, { type Slice } from './PieChart.vue'

const props = defineProps<{ hide: boolean; reloadKey: number }>()
const emit = defineEmits<{ picked: [key: string] }>()

const months = ref(6)
const scope = ref(0) // 0 — все категории, иначе смотрим внутрь одной
const month = ref('') // выбранный на графике месяц
const data = ref<FinanceStats | null>(null)
const loading = ref(true)

async function load() {
  loading.value = true
  try {
    const range = month.value ? monthRange(month.value) : {}
    data.value = await fetchStats({
      months: months.value, category_id: scope.value, ...range,
    })
  } catch {
    showToast('Не удалось загрузить отчёт')
  } finally {
    loading.value = false
  }
}
void load()
watch([months, scope, month, () => props.reloadKey], () => void load())

function monthRange(m: string): { from: string; to: string } {
  const [y, mm] = m.split('-').map(Number)
  const last = new Date(y, mm, 0).getDate()
  return { from: `${m}-01`, to: `${m}-${String(last).padStart(2, '0')}` }
}

const base = computed(() => data.value?.base_currency ?? 'amd')

function money(v: number): string {
  return props.hide ? '•••' : fmtMoney(v, base.value)
}

// Масштаб по самому дорогому месяцу РАСХОДОВ, а не по доходам: зарплата обычно
// в разы больше трат и сплющила бы столбики в полоску. Линия дохода при этом
// упирается в потолок — так и читается «пришло сильно больше, чем ушло».
const maxMonth = computed(() =>
  Math.max(1, ...(data.value?.months ?? []).map((m) => m.expense)))

function incomeTop(v: number): number {
  return Math.min(100, (v / maxMonth.value) * 100)
}

/** Насколько отличается от прошлого периода той же длины. */
const diff = computed(() => {
  const d = data.value
  if (!d || !d.prev_expense) return null
  const pct = ((d.total_expense - d.prev_expense) / d.prev_expense) * 100
  return { pct: Math.round(pct), up: pct > 0 }
})

const scopeName = computed(() =>
  data.value?.categories.find((c) => c.id === scope.value)?.name ?? '')

function catDiff(c: CatStat): number | null {
  if (!c.prev) return null
  return Math.round(((c.total - c.prev) / c.prev) * 100)
}

/**
 * Куски диаграммы — верхний уровень дерева плюс «Не разобрано». Данные те же,
 * что и в списке ниже: разбивка траты уже учтена сервером, поэтому вторая
 * правда об одних деньгах не появляется.
 */
const pie = computed<Slice[]>(() => {
  const d = data.value
  if (!d) return []
  const out: Slice[] = d.categories
    .filter((c) => c.depth === 0 && c.total > 0)
    .map((c) => ({ key: String(c.id), name: `${c.icon ? c.icon + ' ' : ''}${c.name}`, value: c.total }))
  if (d.uncategorized > 0) {
    out.push({ key: 'none', name: 'Не разобрано', value: d.uncategorized })
  }
  return out
})

function onPie(key: string) {
  if (key === 'none') {
    emit('picked', 'unclassified') // «Не разобрано» — повод пойти разметить
    return
  }
  const id = Number(key)
  const hasKids = (data.value?.categories ?? []).some((x) => x.parent_id === id)
  if (hasKids) scope.value = id
}

function drill(c: CatStat) {
  // внутрь имеет смысл проваливаться, только если есть подкатегории
  const hasKids = (data.value?.categories ?? []).some((x) => x.parent_id === c.id)
  if (hasKids && scope.value !== c.id) scope.value = c.id
}
</script>

<template>
  <div>
    <div class="periods">
      <button v-for="n in [3, 6, 12]" :key="n" :class="{ on: months === n && !month }"
              @click="month = ''; months = n">
        {{ n }} мес.
      </button>
      <button v-if="month" class="on" @click="month = ''">за {{ month }} ✕</button>
    </div>

    <p v-if="loading" class="hint">Загрузка…</p>

    <template v-else-if="data">
      <div class="cards">
        <div class="card">
          <span class="lbl">Расходы за период</span>
          <b>{{ money(data.total_expense) }}</b>
          <span v-if="diff" class="sub" :class="{ up: diff.up, down: !diff.up }">
            {{ diff.up ? '▲' : '▼' }} {{ Math.abs(diff.pct) }}% к прошлому периоду
          </span>
        </div>
        <div class="card">
          <span class="lbl">В среднем в месяц</span>
          <b>{{ money(data.avg_month) }}</b>
          <span v-if="data.total_income" class="sub">
            доходы: {{ money(data.total_income) }}
          </span>
        </div>
      </div>

      <!-- по месяцам -->
      <div class="chart">
        <div v-for="m in data.months" :key="m.month" class="bar-col"
             :class="{ on: month === m.month }" @click="month = month === m.month ? '' : m.month">
          <span class="bar-val">{{ hide ? '•' : Math.round(m.expense / 1000) || '' }}</span>
          <div class="bar">
            <i class="exp" :style="{ height: (m.expense / maxMonth) * 100 + '%' }" />
            <i v-if="m.income" class="inc" :style="{ height: incomeTop(m.income) + '%' }" />
          </div>
          <span class="bar-lbl">{{ monthLabel(m.month) }}</span>
        </div>
      </div>
      <p class="hint small">
        Столбик — расходы месяца в тысячах, тонкая полоса — доходы. Нажмите на
        месяц, чтобы посмотреть только его.
      </p>

      <!-- круговая диаграмма -->
      <PieChart v-if="pie.length" :slices="pie" :hide="hide"
                :format="(v: number) => fmtMoney(v, base)" @pick="onPie" />
      <p v-if="pie.length" class="hint small">
        Доли считаются по разбивке трат: чек из магазина разложен по группам
        товаров, а не свален в одну категорию.
      </p>

      <!-- по группам -->
      <div class="sect-head">
        <h3 class="sect">По группам</h3>
        <button v-if="scope" class="link" @click="scope = 0">← ко всем категориям</button>
      </div>
      <p v-if="scope" class="hint small">Внутри «{{ scopeName }}»</p>

      <div v-for="c in data.categories" :key="c.id" class="crow"
           :style="{ paddingLeft: 10 + c.depth * 14 + 'px' }" @click="drill(c)">
        <div class="crow-main">
          <span class="cname">
            <span v-if="c.icon">{{ c.icon }} </span>{{ c.name }}
            <span v-if="c.own && c.own !== c.total" class="meta">· своих {{ money(c.own) }}</span>
          </span>
          <div class="share"><i :style="{ width: c.share + '%' }" /></div>
        </div>
        <div class="crow-right">
          <span class="amount">{{ money(c.total) }}</span>
          <span class="meta">
            {{ c.share }}%
            <span v-if="catDiff(c) !== null" :class="{ up: catDiff(c)! > 0, down: catDiff(c)! < 0 }">
              · {{ catDiff(c)! > 0 ? '▲' : '▼' }}{{ Math.abs(catDiff(c)!) }}%
            </span>
          </span>
        </div>
      </div>

      <div v-if="data.uncategorized" class="crow muted">
        <div class="crow-main"><span class="cname">Без категории</span></div>
        <div class="crow-right"><span class="amount">{{ money(data.uncategorized) }}</span></div>
      </div>

      <p v-if="!data.categories.length && !data.uncategorized" class="hint">
        За период трат нет. Записи появляются на вкладке «Траты» и при оплате
        плановых платежей.
      </p>
    </template>
  </div>
</template>

<style scoped>
.periods {
  display: flex;
  gap: 6px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}

.periods button {
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 13px;
  padding: 7px 12px;
  cursor: pointer;
}

.periods button.on {
  background: var(--accent-color);
  color: #fff;
}

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

.card b {
  font-size: 17px;
}

.lbl,
.sub,
.meta {
  font-size: 11px;
  color: var(--text-secondary);
}

.lbl {
  font-size: 12px;
}

.up {
  color: #ef4444;
}

.down {
  color: #22c55e;
}

.chart {
  display: flex;
  align-items: flex-end;
  gap: 4px;
  height: 130px;
  margin: 14px 0 4px;
}

.bar-col {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  height: 100%;
  cursor: pointer;
}

.bar-col.on .bar {
  outline: 2px solid var(--accent-color);
  outline-offset: 1px;
}

.bar-val {
  font-size: 10px;
  color: var(--text-secondary);
  height: 12px;
}

.bar {
  position: relative;
  flex: 1;
  width: 100%;
  display: flex;
  align-items: flex-end;
  background: var(--card-color);
  border-radius: 5px 5px 0 0;
  overflow: hidden;
}

.bar i {
  display: block;
  width: 100%;
}

.bar .exp {
  background: var(--accent-color);
}

/* доход — тонкая полоса поверх столбика: сравнение «сколько пришло и ушло» */
.bar .inc {
  position: absolute;
  left: 0;
  bottom: 0;
  width: 100%;
  border-top: 2px solid #22c55e;
  background: transparent;
}

.bar-lbl {
  font-size: 10px;
  color: var(--text-secondary);
  padding-top: 3px;
}

.sect-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.sect {
  font-size: 14px;
  margin: 16px 0 6px;
  color: var(--text-secondary);
}

.link {
  background: none;
  border: none;
  color: var(--accent-color);
  font-size: 12px;
  cursor: pointer;
}

.hint {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 10px 0;
}

.hint.small {
  font-size: 11px;
  margin: 4px 0 0;
}

.crow {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
  background: var(--card-color);
  border-radius: 10px;
  padding: 9px 12px;
  margin-bottom: 5px;
  cursor: pointer;
  backdrop-filter: var(--card-blur);
}

.crow.muted {
  opacity: 0.7;
  cursor: default;
}

.crow-main {
  min-width: 0;
  flex: 1;
}

.cname {
  font-size: 14px;
  overflow-wrap: anywhere;
}

.share {
  height: 4px;
  border-radius: 2px;
  background: var(--bg-color);
  margin-top: 6px;
  overflow: hidden;
}

.share i {
  display: block;
  height: 100%;
  background: var(--accent-color);
}

.crow-right {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  white-space: nowrap;
}

.amount {
  font-size: 14px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
</style>
