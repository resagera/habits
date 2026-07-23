<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { showToast } from '../../../shared/toast'
import { fetchHistory } from '../api'
import type { Tracker } from '../composables/useTracker'
import { errorText } from '../composables/useTracker'
import { monthGrid, toISODate } from '../dates'
import { cellView, pressHandlers } from '../display'
import type { Category } from '../types'

// Полноэкранный вид всех отметок: месяцы с самой старой отметки до текущего.
const props = defineProps<{
  category: Category
  tracker: Tracker
}>()

const emit = defineEmits<{ close: [] }>()

const weekdays = ['ПН', 'ВТ', 'СР', 'ЧТ', 'ПТ', 'СБ', 'ВС']
const today = toISODate(new Date())
const loading = ref(true)
const firstDay = ref<string | null>(null)
const isCounter = computed(() => props.category.kind === 'counter')

onMounted(async () => {
  try {
    const { days } = await fetchHistory(props.category.id)
    firstDay.value = days[0]?.day ?? null
    // вся история — в общий кэш отметок
    const from = firstDay.value ?? today
    props.tracker.mergeRange(
      [{ category_id: props.category.id, days }],
      from,
      '9999-12-31',
      props.category.id,
    )
  } catch (e) {
    showToast(errorText(e))
  } finally {
    loading.value = false
  }
})

interface MonthBlock {
  year: number
  month: number // 0-based
  title: string
  leadingEmpty: number
  daysInMonth: number
}

const months = computed<MonthBlock[]>(() => {
  const now = new Date()
  let start = new Date(now.getFullYear(), now.getMonth(), 1)
  if (firstDay.value) {
    const [y, m] = firstDay.value.split('-').map(Number)
    start = new Date(y, m - 1, 1)
  }
  const result: MonthBlock[] = []
  const cur = new Date(start)
  const end = new Date(now.getFullYear(), now.getMonth(), 1)
  while (cur <= end) {
    const g = monthGrid(cur.getFullYear(), cur.getMonth())
    result.push({
      year: cur.getFullYear(),
      month: cur.getMonth(),
      title: cur.toLocaleDateString('ru-RU', { month: 'long', year: 'numeric' }),
      leadingEmpty: g.leadingEmpty,
      daysInMonth: g.daysInMonth,
    })
    cur.setMonth(cur.getMonth() + 1)
  }
  return result.reverse() // свежие месяцы сверху
})

/** Итог: всего отметок / сумма счётчика. */
const total = computed(() => {
  const m = props.tracker.marks.get(props.category.id)
  if (!m) return 0
  let sum = 0
  for (const info of m.values()) sum += isCounter.value ? info.count : 1
  return sum
})

function dayISO(b: MonthBlock, day: number): string {
  return toISODate(new Date(b.year, b.month, day))
}

function view(b: MonthBlock, day: number) {
  return cellView(props.category, props.tracker.markInfo(props.category.id, dayISO(b, day)))
}

function handlers(b: MonthBlock, day: number) {
  if (isCounter.value) {
    return pressHandlers(
      () => props.tracker.increment(props.category, dayISO(b, day), 1),
      () => props.tracker.increment(props.category, dayISO(b, day), -1),
    )
  }
  return { click: () => props.tracker.toggle(props.category, dayISO(b, day)) }
}
</script>

<template>
  <div class="history-screen">
    <div class="history-header">
      <div class="history-title">
        {{ category.name }}
        <span class="history-total">{{ isCounter ? 'сумма' : 'отметок' }}: {{ total }}</span>
      </div>
      <button class="close-x" @click="emit('close')">✕</button>
    </div>

    <div class="history-body">
      <div v-if="loading" class="loading">Загрузка…</div>
      <template v-else>
        <div v-for="b in months" :key="b.title" class="month">
          <div class="month-title">{{ b.title }}</div>
          <div class="month-weekdays">
            <div v-for="w in weekdays" :key="w">{{ w }}</div>
          </div>
          <div class="month-grid">
            <div v-for="i in b.leadingEmpty" :key="'e' + i" class="mday empty"></div>
            <div
              v-for="day in b.daysInMonth"
              :key="day"
              class="mday"
              :class="{
                marked: view(b, day).marked && !view(b, day).emoji,
                today: dayISO(b, day) === today,
                circle: category.style === 'circle' && !isCounter,
              }"
              :style="
                view(b, day).marked && view(b, day).color
                  ? { backgroundColor: view(b, day).color! }
                  : {}
              "
              v-on="handlers(b, day)"
            >
              <template v-if="isCounter && view(b, day).count > 0">
                <span class="day-num">{{ day }}</span>
                <span class="day-count">{{ view(b, day).count }}</span>
              </template>
              <template v-else-if="view(b, day).emoji">
                <span class="day-num">{{ day }}</span>
                <span class="day-emoji">{{ view(b, day).emoji }}</span>
              </template>
              <template v-else>{{ day }}</template>
            </div>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.history-screen {
  position: fixed;
  inset: 0;
  z-index: 60;
  background: var(--bg-color);
  display: flex;
  flex-direction: column;
}

.history-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--bg-secondary);
  flex: none;
}

.history-title {
  font-weight: 700;
  font-size: 17px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.history-total {
  font-weight: 400;
  font-size: 12px;
  color: var(--text-secondary);
  margin-left: 8px;
}

.close-x {
  background: none;
  border: none;
  font-size: 20px;
  color: var(--text-secondary);
  padding: 4px 8px;
  flex: none;
}

.history-body {
  flex: 1;
  overflow-y: auto;
  padding: 12px 16px 32px;
}

.loading {
  text-align: center;
  color: var(--text-secondary);
  padding: 32px 0;
}

.month {
  margin-bottom: 20px;
}

.month-title {
  font-weight: 600;
  margin-bottom: 6px;
  text-transform: capitalize;
}

.month-weekdays {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  font-size: 11px;
  color: var(--text-secondary);
  margin-bottom: 4px;
}

.month-grid {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  gap: 4px;
}

.mday {
  aspect-ratio: 1 / 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  border-radius: 10px;
  cursor: pointer;
  font-size: 13px;
  background-color: var(--bg-secondary);
  user-select: none;
  -webkit-user-select: none;
  touch-action: manipulation;
}

.mday.circle {
  border-radius: 50%;
}

.mday.empty {
  background: none;
  cursor: default;
}

.mday.marked {
  color: #fff;
  font-weight: 600;
}

.mday.today {
  border-bottom: 3px solid red;
}

.day-num {
  font-size: 9px;
  line-height: 1;
  opacity: 0.75;
}

.day-emoji {
  font-size: 15px;
  line-height: 1.2;
}

.day-count {
  font-size: 13px;
  font-weight: 700;
  line-height: 1.1;
}
</style>
