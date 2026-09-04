<script setup lang="ts">
// Календарь — агрегатор по дням: слои из трекеров (несколько, с видимыми
// пересечениями), напоминаний, дневника, задач, чек-листов, еды и
// AI-расписаний. Клик на день — всё содержимое дня со ссылками на страницы.
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { showToast } from '../../shared/toast'
import { colorCss } from '../tracker/display'
import { fetchCalendar, fetchPrefs, savePrefs } from './api'
import { DEFAULT_PREFS, type CalendarPayload, type CalPrefs } from './types'

const router = useRouter()

// --- текущий месяц ---
const now = new Date()
const year = ref(now.getFullYear())
const month = ref(now.getMonth()) // 0-11

const MONTHS = ['Январь', 'Февраль', 'Март', 'Апрель', 'Май', 'Июнь',
  'Июль', 'Август', 'Сентябрь', 'Октябрь', 'Ноябрь', 'Декабрь']
const DOW = ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс']

function pad(n: number): string {
  return String(n).padStart(2, '0')
}

function dstr(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

const todayStr = dstr(now)

// сетка: с понедельника недели 1-го числа по воскресенье недели последнего
const gridDays = computed(() => {
  const first = new Date(year.value, month.value, 1)
  const start = new Date(first)
  start.setDate(1 - ((first.getDay() + 6) % 7))
  const days: { date: string; inMonth: boolean; dom: number }[] = []
  const d = new Date(start)
  for (let i = 0; i < 42; i++) {
    days.push({ date: dstr(d), inMonth: d.getMonth() === month.value, dom: d.getDate() })
    d.setDate(d.getDate() + 1)
    if (d.getMonth() !== month.value && d.getDay() === 1 && i >= 27) break
  }
  return days
})

function prevMonth() {
  if (month.value === 0) {
    month.value = 11
    year.value--
  } else month.value--
}
function nextMonth() {
  if (month.value === 11) {
    month.value = 0
    year.value++
  } else month.value++
}
function goToday() {
  year.value = now.getFullYear()
  month.value = now.getMonth()
}

// --- данные месяца (кэш по диапазону сетки) ---
const cache = new Map<string, CalendarPayload>()
const data = ref<CalendarPayload | null>(null)
const loading = ref(false)

async function loadMonth() {
  const days = gridDays.value
  const key = `${days[0].date}_${days[days.length - 1].date}`
  const cached = cache.get(key)
  if (cached) {
    data.value = cached
    return
  }
  loading.value = true
  try {
    const payload = await fetchCalendar(days[0].date, days[days.length - 1].date)
    cache.set(key, payload)
    data.value = payload
  } catch {
    showToast('Не удалось загрузить календарь')
  } finally {
    loading.value = false
  }
}
watch([year, month], loadMonth, { immediate: true })

// --- слои ---
const prefs = ref<CalPrefs>(structuredClone(DEFAULT_PREFS))
const prefsLoaded = ref(false)
let saveTimer: ReturnType<typeof setTimeout> | undefined

fetchPrefs()
  .then(({ prefs: p }) => {
    prefs.value = {
      trackers: Array.isArray(p.trackers) ? p.trackers : null,
      layers: { ...DEFAULT_PREFS.layers, ...(p.layers ?? {}) },
    }
  })
  .catch(() => {})
  .finally(() => (prefsLoaded.value = true))

watch(
  prefs,
  () => {
    if (!prefsLoaded.value) return
    clearTimeout(saveTimer)
    saveTimer = setTimeout(() => savePrefs(prefs.value).catch(() => {}), 600)
  },
  { deep: true },
)

const categories = computed(() => data.value?.categories ?? [])

/** id выбранных трекеров (null в prefs = все доступные). */
const selectedTrackers = computed<Set<number>>(() => {
  if (prefs.value.trackers === null) return new Set(categories.value.map((c) => c.id))
  return new Set(prefs.value.trackers)
})

function toggleTracker(id: number) {
  const sel = new Set(selectedTrackers.value)
  if (sel.has(id)) sel.delete(id)
  else sel.add(id)
  prefs.value.trackers = [...sel]
}

// --- индексы по дням ---
const marksByDay = computed(() => {
  const m = new Map<string, { catId: number; color: string; emoji: string }[]>()
  if (!data.value) return m
  const catById = new Map(categories.value.map((c) => [c.id, c]))
  for (const cm of data.value.marks) {
    const cat = catById.get(cm.category_id)
    if (!cat || !selectedTrackers.value.has(cm.category_id)) continue
    for (const d of cm.days) {
      const list = m.get(d.day) ?? []
      list.push({
        catId: cm.category_id,
        // цвет отметки может быть парой '#a,#b' (градиент из набора трекера),
        // а здесь он уезжает прямо в CSS background — переводим сразу
        color: colorCss(d.color || cat.color),
        emoji: cat.style === 'emoji' ? d.emoji || cat.emoji || '⭐' : '',
      })
      m.set(d.day, list)
    }
  }
  return m
})

/** Дни «полного пересечения»: отмечены ВСЕ выбранные трекеры (если их ≥2). */
const allHitDays = computed<Set<string>>(() => {
  const res = new Set<string>()
  const need = selectedTrackers.value.size
  if (need < 2) return res
  for (const [day, list] of marksByDay.value) {
    if (new Set(list.map((x) => x.catId)).size === need) res.add(day)
  }
  return res
})

function groupByDay<T extends { day: string }>(items: T[] | undefined): Map<string, T[]> {
  const m = new Map<string, T[]>()
  for (const it of items ?? []) {
    const list = m.get(it.day) ?? []
    list.push(it)
    m.set(it.day, list)
  }
  return m
}

const remByDay = computed(() => groupByDay(prefs.value.layers.reminders ? data.value?.reminders : []))
const diaryByDay = computed(() => groupByDay(prefs.value.layers.diary ? data.value?.diary : []))
const tasksByDay = computed(() => groupByDay(prefs.value.layers.tasks ? data.value?.tasks : []))
const checkerByDay = computed(() => groupByDay(prefs.value.layers.checker ? data.value?.checker_days : []))
const deadlinesByDay = computed(() => groupByDay(prefs.value.layers.checker ? data.value?.deadlines : []))
const foodByDay = computed(() => groupByDay(prefs.value.layers.food ? data.value?.food : []))
const aiByDay = computed(() => groupByDay(prefs.value.layers.ai ? data.value?.ai : []))

function dayIcons(day: string): { icon: string; cls?: string }[] {
  const icons: { icon: string; cls?: string }[] = []
  if (remByDay.value.has(day)) icons.push({ icon: '🔔' })
  if (diaryByDay.value.has(day)) icons.push({ icon: '📔' })
  const tasks = tasksByDay.value.get(day)
  if (tasks) icons.push({ icon: '🗂', cls: tasks.some((t) => !t.done && day < todayStr) ? 'overdue' : '' })
  const snaps = checkerByDay.value.get(day)
  if (snaps) {
    const full = snaps.every((s) => s.total > 0 && s.done >= s.total)
    icons.push({ icon: '✅', cls: full ? '' : 'partial' })
  }
  if (deadlinesByDay.value.has(day)) icons.push({ icon: '⏰' })
  const food = foodByDay.value.get(day)?.[0]
  if (food) icons.push({ icon: '🍽', cls: food.goal_kcal > 0 && food.kcal > food.goal_kcal ? 'overdue' : '' })
  if (aiByDay.value.has(day)) icons.push({ icon: '🕒' })
  return icons.slice(0, 4)
}

// --- модалка дня ---
const openDay = ref<string | null>(null)

const openDayTitle = computed(() => {
  if (!openDay.value) return ''
  const [y, m, d] = openDay.value.split('-').map(Number)
  return `${d} ${MONTHS[m - 1].toLowerCase()} ${y}`
})

const openDayMarks = computed(() => {
  if (!openDay.value) return []
  const catById = new Map(categories.value.map((c) => [c.id, c]))
  return (marksByDay.value.get(openDay.value) ?? []).map((x) => ({
    ...x,
    name: catById.get(x.catId)?.name ?? '',
  }))
})

function go(path: string) {
  openDay.value = null
  router.push(path)
}

function fmtKcal(v: number): string {
  return String(Math.round(v))
}
</script>

<template>
  <!-- шапка месяца -->
  <div class="cal-head">
    <button class="nav" @click="prevMonth">‹</button>
    <button class="title" @click="goToday">
      {{ MONTHS[month] }} {{ year }}
      <span v-if="year !== now.getFullYear() || month !== now.getMonth()" class="today-hint">· сегодня</span>
    </button>
    <button class="nav" @click="nextMonth">›</button>
  </div>

  <!-- слои -->
  <details class="layers">
    <summary>Слои <span class="l-sub">({{ selectedTrackers.size }} трекеров)</span></summary>
    <div class="l-group">
      <label v-for="c in categories" :key="c.id" class="l-item">
        <input type="checkbox" :checked="selectedTrackers.has(c.id)" @change="toggleTracker(c.id)" />
        <span class="dot" :style="{ background: c.color }"></span>
        <span class="l-name">{{ c.name }}<span v-if="c.shared && !c.mine"> 👥</span></span>
      </label>
      <p v-if="!categories.length" class="l-hint">Трекеров нет — создайте на странице Tracker.</p>
    </div>
    <div class="l-group toggles">
      <label class="l-item"><input v-model="prefs.layers.reminders" type="checkbox" /><span>🔔 Напоминания</span></label>
      <label class="l-item"><input v-model="prefs.layers.diary" type="checkbox" /><span>📔 Дневник</span></label>
      <label class="l-item"><input v-model="prefs.layers.tasks" type="checkbox" /><span>🗂 Задачи</span></label>
      <label class="l-item"><input v-model="prefs.layers.checker" type="checkbox" /><span>✅ Чек-листы</span></label>
      <label class="l-item"><input v-model="prefs.layers.food" type="checkbox" /><span>🍽 Еда</span></label>
      <label class="l-item"><input v-model="prefs.layers.ai" type="checkbox" /><span>🕒 AI</span></label>
    </div>
    <p v-if="selectedTrackers.size >= 2" class="l-hint">
      Дни, где отмечены все выбранные трекеры, подсвечены рамкой.
    </p>
  </details>

  <!-- сетка -->
  <div class="grid dow-row">
    <div v-for="d in DOW" :key="d" class="dow">{{ d }}</div>
  </div>
  <div class="grid" :class="{ dim: loading }">
    <button
      v-for="cell in gridDays"
      :key="cell.date"
      class="cell"
      :class="{ out: !cell.inMonth, today: cell.date === todayStr, allhit: allHitDays.has(cell.date) }"
      @click="openDay = cell.date"
    >
      <span class="dom">{{ cell.dom }}</span>
      <span class="dots">
        <template v-for="(m, i) in marksByDay.get(cell.date) ?? []" :key="i">
          <span v-if="i < 4" class="mark">
            <span v-if="m.emoji" class="memoji">{{ m.emoji }}</span>
            <span v-else class="mdot" :style="{ background: m.color }"></span>
          </span>
        </template>
        <span v-if="(marksByDay.get(cell.date)?.length ?? 0) > 4" class="more">+{{ (marksByDay.get(cell.date)?.length ?? 0) - 4 }}</span>
      </span>
      <span class="icons">
        <span v-for="(ic, i) in dayIcons(cell.date)" :key="i" class="ic" :class="ic.cls">{{ ic.icon }}</span>
      </span>
    </button>
  </div>

  <!-- модалка дня -->
  <div v-if="openDay" class="modal" @click.self="openDay = null">
    <div class="modal-content day-modal">
      <h3>{{ openDayTitle }}</h3>

      <template v-if="openDayMarks.length">
        <h4>📊 Трекеры</h4>
        <button v-for="(m, i) in openDayMarks" :key="i" class="row-item" @click="go('/tracker')">
          <span v-if="m.emoji" class="memoji">{{ m.emoji }}</span>
          <span v-else class="mdot big" :style="{ background: m.color }"></span>
          <span>{{ m.name }}</span>
        </button>
      </template>

      <template v-if="remByDay.get(openDay)?.length">
        <h4>🔔 Напоминания</h4>
        <button v-for="r in remByDay.get(openDay)" :key="'r' + r.id + r.time" class="row-item" @click="go('/reminders')">
          <span class="t">{{ r.time }}</span><span>{{ r.title }}</span>
        </button>
      </template>

      <template v-if="diaryByDay.get(openDay)?.length">
        <h4>📔 Дневник</h4>
        <button v-for="d in diaryByDay.get(openDay)" :key="'d' + d.id" class="row-item" @click="go('/diary')">
          <span class="t">{{ d.time }}</span><span class="snip">{{ d.snippet }}</span>
        </button>
      </template>

      <template v-if="tasksByDay.get(openDay)?.length">
        <h4>🗂 Задачи (срок)</h4>
        <button v-for="t in tasksByDay.get(openDay)" :key="'t' + t.id" class="row-item" @click="go('/tasks')">
          <span class="t">{{ t.time }}</span>
          <span :class="{ done: t.done, overdue: !t.done && openDay < todayStr }">{{ t.title }}</span>
        </button>
      </template>

      <template v-if="checkerByDay.get(openDay)?.length">
        <h4>✅ Чек-листы</h4>
        <button v-for="s in checkerByDay.get(openDay)" :key="'s' + s.root_id" class="row-item" @click="go('/checker')">
          <span class="t">{{ s.done }}/{{ s.total }}</span><span>{{ s.name }}</span>
        </button>
      </template>

      <template v-if="deadlinesByDay.get(openDay)?.length">
        <h4>⏰ Дедлайны</h4>
        <button v-for="(dl, i) in deadlinesByDay.get(openDay)" :key="'dl' + i" class="row-item" @click="go('/checker')">
          <span class="t">{{ dl.time }}</span><span>{{ dl.title }}</span>
        </button>
      </template>

      <template v-if="foodByDay.get(openDay)?.length">
        <h4>🍽 Еда</h4>
        <button v-for="f in foodByDay.get(openDay)" :key="'f' + f.day" class="row-item" @click="go('/food')">
          <span :class="{ overdue: f.goal_kcal > 0 && f.kcal > f.goal_kcal }">
            {{ fmtKcal(f.kcal) }} ккал<template v-if="f.goal_kcal > 0"> из {{ fmtKcal(f.goal_kcal) }}</template>
            · приёмов: {{ f.meals }}
          </span>
        </button>
      </template>

      <template v-if="aiByDay.get(openDay)?.length">
        <h4>🕒 AI-расписания</h4>
        <button v-for="a in aiByDay.get(openDay)" :key="'a' + a.id" class="row-item" @click="go('/ai')">
          <span class="t">{{ a.time }}</span><span class="snip">{{ a.prompt }}</span>
        </button>
      </template>

      <p
        v-if="!openDayMarks.length && !remByDay.get(openDay)?.length && !diaryByDay.get(openDay)?.length &&
          !tasksByDay.get(openDay)?.length && !checkerByDay.get(openDay)?.length &&
          !deadlinesByDay.get(openDay)?.length && !foodByDay.get(openDay)?.length && !aiByDay.get(openDay)?.length"
        class="l-hint"
      >
        В этот день пока пусто.
      </p>

      <button class="btn" @click="openDay = null">Закрыть</button>
    </div>
  </div>
</template>

<style scoped>
.cal-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.nav {
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 20px;
  padding: 4px 16px;
}

.title {
  background: none;
  border: none;
  color: var(--text-color);
  font-size: 17px;
  font-weight: 600;
}

.today-hint {
  font-size: 12px;
  color: var(--accent-color);
  font-weight: 400;
}

.layers {
  background: var(--card-color);
  border-radius: 8px;
  padding: 8px 12px;
  margin-bottom: 10px;
  font-size: 13px;
}

.layers summary {
  cursor: pointer;
  color: var(--accent-color);
}

.l-sub {
  color: var(--text-secondary);
}

.l-group {
  display: flex;
  flex-wrap: wrap;
  gap: 2px 14px;
  margin-top: 8px;
}

.l-group.toggles {
  border-top: 1px solid var(--hover-bg-color);
  padding-top: 8px;
}

.l-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 3px 0;
  cursor: pointer;
}

.l-name {
  max-width: 150px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.l-hint {
  color: var(--text-secondary);
  font-size: 12px;
  margin: 6px 0 2px;
}

.dot {
  width: 10px;
  height: 10px;
  border-radius: 3px;
  flex: none;
}

.grid {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  gap: 3px;
}

.grid.dim {
  opacity: 0.6;
}

.dow-row {
  margin-bottom: 3px;
}

.dow {
  text-align: center;
  font-size: 11px;
  color: var(--text-secondary);
  padding: 2px 0;
}

.cell {
  background: var(--card-color);
  border: 1.5px solid transparent;
  border-radius: 8px;
  min-height: 58px;
  padding: 3px 2px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  color: var(--text-color);
}

.cell.out {
  opacity: 0.38;
}

.cell.today {
  border-color: var(--accent-color);
}

.cell.allhit {
  border-color: #22c55e;
  background: #16653418;
}

.dom {
  font-size: 12px;
  line-height: 1;
}

.dots {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 2px;
  min-height: 10px;
}

.mdot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.mdot.big {
  width: 12px;
  height: 12px;
  border-radius: 4px;
}

.memoji {
  font-size: 10px;
  line-height: 1;
}

.more {
  font-size: 9px;
  color: var(--text-secondary);
}

.icons {
  display: flex;
  gap: 1px;
  font-size: 9px;
  line-height: 1;
  flex-wrap: wrap;
  justify-content: center;
}

.ic.overdue {
  filter: hue-rotate(140deg) saturate(3);
}

.ic.partial {
  opacity: 0.55;
}

.day-modal {
  text-align: left;
  max-width: 480px;
}

.day-modal h4 {
  margin: 12px 0 4px;
  font-size: 13px;
  color: var(--text-secondary);
}

.row-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  padding: 8px 10px;
  margin-bottom: 4px;
  color: var(--text-color);
  text-align: left;
  font-size: 14px;
}

.row-item .t {
  flex: none;
  color: var(--text-secondary);
  font-size: 12px;
  min-width: 38px;
}

.snip {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.done {
  text-decoration: line-through;
  opacity: 0.6;
}

.overdue {
  color: #ef4444;
}

.btn {
  width: 100%;
  padding: 10px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
  margin-top: 12px;
}
</style>
