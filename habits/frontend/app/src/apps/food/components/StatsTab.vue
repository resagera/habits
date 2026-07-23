<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import * as foodApi from '../api'
import FoodChart from './FoodChart.vue'
import { addDays, r0, todayStr, type FoodDayStat, type FoodGoal } from '../types'

// Вкладка «Статистика»: периоды 7/14/30 дней и произвольный, карточки, графики.
const period = ref<7 | 14 | 30 | 0>(7) // 0 — произвольный
const from = ref(addDays(todayStr(), -6))
const to = ref(todayStr())

const days = ref<FoodDayStat[]>([])
const goals = ref<FoodGoal[]>([])
const loading = ref(true)
const failed = ref(false)

function setPeriod(p: 7 | 14 | 30 | 0) {
  period.value = p
  if (p !== 0) {
    to.value = todayStr()
    from.value = addDays(todayStr(), -(p - 1))
    load()
  }
}

async function load() {
  if (from.value > to.value) return
  loading.value = true
  failed.value = false
  try {
    const res = await foodApi.fetchStats(from.value, to.value)
    days.value = res.days
    goals.value = res.goals
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
}

onMounted(load)

/** Полный ряд дат периода (дни без записей — нули). */
const range = computed(() => {
  const out: string[] = []
  let d = from.value
  let guard = 0
  while (d <= to.value && guard++ < 400) {
    out.push(d)
    d = addDays(d, 1)
  }
  return out
})

const byDay = computed(() => {
  const m = new Map<string, FoodDayStat>()
  for (const d of days.value) m.set(d.day, d)
  return m
})

function goalFor(day: string): FoodGoal | null {
  for (const g of goals.value) {
    if (g.date_from <= day && (!g.date_to || g.date_to >= day)) return g
  }
  return null
}

const filled = computed(() => days.value.filter((d) => d.meals > 0))

const cards = computed(() => {
  const n = filled.value.length
  const sum = filled.value.reduce(
    (a, d) => ({ c: a.c + d.calories, p: a.p + d.protein, f: a.f + d.fat, cb: a.cb + d.carbs }),
    { c: 0, p: 0, f: 0, cb: 0 },
  )
  // выполнение целей — по заполненным дням с целью
  let calPct = 0
  let calN = 0
  let protPct = 0
  let protN = 0
  let overDays = 0
  let underDays = 0
  for (const d of filled.value) {
    const g = goalFor(d.day)
    if (!g) continue
    if (g.calories > 0) {
      calPct += (d.calories / g.calories) * 100
      calN++
      if (d.calories > g.calories) overDays++
      else underDays++
    }
    if (g.protein > 0) {
      protPct += (d.protein / g.protein) * 100
      protN++
    }
  }
  return {
    n,
    avgCal: n ? sum.c / n : 0,
    avgProt: n ? sum.p / n : 0,
    avgFat: n ? sum.f / n : 0,
    avgCarbs: n ? sum.cb / n : 0,
    calCompletion: calN ? calPct / calN : 0,
    protCompletion: protN ? protPct / protN : 0,
    overDays,
    underDays,
  }
})

function seriesOf(pick: (d: FoodDayStat) => number): number[] {
  return range.value.map((day) => {
    const d = byDay.value.get(day)
    return d ? pick(d) : 0
  })
}

const calValues = computed(() => seriesOf((d) => d.calories))
const calGoal = computed(() => range.value.map((day) => goalFor(day)?.calories ?? 0))
const protGoal = computed(() => range.value.map((day) => goalFor(day)?.protein ?? 0))

/** Скользящее среднее калорий за 7 дней (по имеющимся значениям). */
const cal7avg = computed(() => {
  const vals = calValues.value
  return vals.map((_, i) => {
    let s = 0
    let n = 0
    for (let j = Math.max(0, i - 6); j <= i; j++) {
      if (vals[j]! > 0) {
        s += vals[j]!
        n++
      }
    }
    return n ? s / n : 0
  })
})
</script>

<template>
  <!-- период -->
  <div class="periods">
    <button v-for="p in ([7, 14, 30] as const)" :key="p" :class="{ on: period === p }" @click="setPeriod(p)">
      {{ p }} дней
    </button>
    <button :class="{ on: period === 0 }" @click="setPeriod(0)">Период</button>
  </div>
  <div v-if="period === 0" class="custom">
    <input v-model="from" type="date" />
    <span>—</span>
    <input v-model="to" type="date" />
    <button class="go" @click="load">ОК</button>
  </div>

  <div v-if="loading" class="hint">Загрузка…</div>
  <p v-else-if="failed" class="hint">
    Не удалось загрузить <button class="retry" @click="load">повторить</button>
  </p>

  <template v-else>
    <p v-if="filled.length === 0" class="hint">За этот период записей о питании нет.</p>
    <template v-else>
      <!-- карточки -->
      <div class="cards">
        <div class="card-glass stat"><b>{{ r0(cards.avgCal) }}</b><span>ккал в среднем</span></div>
        <div class="card-glass stat"><b>{{ r0(cards.avgProt) }} г</b><span>белок в среднем</span></div>
        <div class="card-glass stat"><b>{{ r0(cards.avgFat) }} г</b><span>жиры в среднем</span></div>
        <div class="card-glass stat"><b>{{ r0(cards.avgCarbs) }} г</b><span>углеводы в среднем</span></div>
        <div class="card-glass stat"><b>{{ cards.n }}</b><span>заполненных дней</span></div>
        <div class="card-glass stat"><b>{{ r0(cards.calCompletion) }}%</b><span>цель по калориям</span></div>
        <div class="card-glass stat"><b>{{ r0(cards.protCompletion) }}%</b><span>цель по белку</span></div>
        <div class="card-glass stat">
          <b>{{ cards.overDays }} / {{ cards.underDays }}</b><span>дней сверх / в рамках цели</span>
        </div>
      </div>

      <!-- графики -->
      <FoodChart
        title="🔥 Калории по дням"
        :labels="range"
        :series="[{ label: 'ккал', color: 'var(--accent-color)', values: calValues }]"
        :goal="calGoal"
        :avg="cal7avg"
        :over-color="true"
      />
      <FoodChart
        title="🥩 Белки по дням"
        :labels="range"
        :series="[{ label: 'белки, г', color: '#22c55e', values: seriesOf((d) => d.protein) }]"
        :goal="protGoal"
        :over-color="false"
      />
      <FoodChart
        title="🧈 Жиры по дням"
        :labels="range"
        :series="[{ label: 'жиры, г', color: '#eab308', values: seriesOf((d) => d.fat) }]"
      />
      <FoodChart
        title="🍞 Углеводы по дням"
        :labels="range"
        :series="[{ label: 'углеводы, г', color: '#3b82f6', values: seriesOf((d) => d.carbs) }]"
      />
      <FoodChart
        title="🍽 Приёмы пищи по дням"
        :labels="range"
        :series="[{ label: 'приёмов', color: '#94a3b8', values: seriesOf((d) => d.meals) }]"
      />
      <FoodChart
        title="📊 Распределение калорий по приёмам"
        :labels="range"
        :stacked="true"
        :series="[
          { label: 'завтрак', color: '#f97316', values: seriesOf((d) => d.breakfast) },
          { label: 'обед', color: '#22c55e', values: seriesOf((d) => d.lunch) },
          { label: 'ужин', color: '#3b82f6', values: seriesOf((d) => d.dinner) },
          { label: 'перекусы', color: '#a855f7', values: seriesOf((d) => d.snack) },
          { label: 'прочее', color: '#94a3b8', values: seriesOf((d) => d.other) },
        ]"
      />
    </template>
  </template>
</template>

<style scoped>
.periods {
  display: flex;
  gap: 6px;
  margin-bottom: 8px;
}

.periods button {
  flex: 1;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  padding: 7px 0;
  font-size: 13px;
  color: var(--text-color);
}

.periods button.on {
  background: var(--accent-color);
  color: #fff;
}

.custom {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 8px;
}

.custom input {
  flex: 1;
  min-width: 0;
}

.custom .go {
  background: var(--accent-color);
  color: #fff;
  border: none;
  border-radius: 8px;
  padding: 7px 14px;
}

.cards {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 8px;
  margin-bottom: 10px;
}

.stat {
  background: var(--card-color);
  border-radius: 8px;
  padding: 10px 12px;
}

.stat b {
  display: block;
  font-size: 17px;
}

.stat span {
  font-size: 11px;
  color: var(--text-secondary);
}

.hint {
  text-align: center;
  color: var(--text-secondary);
  padding: 20px 0;
}

.retry {
  background: none;
  border: none;
  color: var(--accent-color);
  text-decoration: underline;
}
</style>
