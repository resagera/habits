<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { showToast } from '../../../shared/toast'
import { fetchMarks } from '../api'
import type { Tracker } from '../composables/useTracker'
import { errorText } from '../composables/useTracker'
import { monthGrid, toISODate } from '../dates'
import { cellView, pressHandlers } from '../display'
import type { Category } from '../types'

const props = defineProps<{
  category: Category
  /* общий стейт из useTracker: сетка 8 недель остаётся консистентной */
  tracker: Tracker
}>()

const emit = defineEmits<{ close: [] }>()

const weekdays = ['ПН', 'ВТ', 'СР', 'ЧТ', 'ПТ', 'СБ', 'ВС']
const cursor = ref(new Date())
const today = toISODate(new Date())
const isCounter = computed(() => props.category.kind === 'counter')

const title = computed(() =>
  cursor.value.toLocaleDateString('ru-RU', { month: 'long', year: 'numeric' }),
)

const grid = computed(() => monthGrid(cursor.value.getFullYear(), cursor.value.getMonth()))

function dayISO(day: number): string {
  return toISODate(new Date(cursor.value.getFullYear(), cursor.value.getMonth(), day))
}

async function loadMonth() {
  const year = cursor.value.getFullYear()
  const month = cursor.value.getMonth()
  const from = toISODate(new Date(year, month, 1))
  const to = toISODate(new Date(year, month + 1, 0))
  try {
    const { marks } = await fetchMarks(from, to, props.category.id)
    props.tracker.mergeRange(marks, from, to, props.category.id)
  } catch (e) {
    showToast(errorText(e))
  }
}

watch(cursor, loadMonth, { immediate: true })

function shiftMonth(delta: number) {
  const c = cursor.value
  cursor.value = new Date(c.getFullYear(), c.getMonth() + delta, 1)
}

function view(day: number) {
  return cellView(props.category, props.tracker.markInfo(props.category.id, dayISO(day)))
}

function handlers(day: number) {
  if (isCounter.value) {
    return pressHandlers(
      () => props.tracker.increment(props.category, dayISO(day), 1),
      () => props.tracker.increment(props.category, dayISO(day), -1),
    )
  }
  return { click: () => props.tracker.toggle(props.category, dayISO(day)) }
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content">
      <h3>{{ category.name }} — {{ title }}</h3>
      <div class="calendar-weekdays">
        <div v-for="w in weekdays" :key="w">{{ w }}</div>
      </div>
      <div class="calendar-grid">
        <div v-for="i in grid.leadingEmpty" :key="'e' + i" class="calendar-day empty"></div>
        <div
          v-for="day in grid.daysInMonth"
          :key="day"
          class="calendar-day"
          :class="{
            marked: view(day).marked && !view(day).emoji,
            today: dayISO(day) === today,
            circle: category.style === 'circle' && !isCounter,
          }"
          :style="view(day).marked && view(day).color ? { backgroundColor: view(day).color! } : {}"
          v-on="handlers(day)"
        >
          <template v-if="isCounter && view(day).count > 0">
            <span class="day-num">{{ day }}</span>
            <span class="day-count">{{ view(day).count }}</span>
          </template>
          <template v-else-if="view(day).emoji">
            <span class="day-num">{{ day }}</span>
            <span class="day-emoji">{{ view(day).emoji }}</span>
          </template>
          <template v-else>{{ day }}</template>
        </div>
      </div>
      <div class="calendar-nav">
        <button @click="shiftMonth(-1)">←</button>
        <button class="close-btn" @click="emit('close')">✕ Закрыть</button>
        <button @click="shiftMonth(1)">→</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.calendar-weekdays {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  font-size: 12px;
  color: var(--text-secondary);
  margin-bottom: 6px;
}

.calendar-grid {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  gap: 6px;
}

.calendar-day {
  aspect-ratio: 1 / 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  border-radius: 12px;
  cursor: pointer;
  font-size: 15px;
  font-weight: 500;
  background-color: var(--bg-secondary);
  transition: all 0.2s ease;
  user-select: none;
  -webkit-user-select: none;
  touch-action: manipulation;
}

.calendar-day.circle {
  border-radius: 50%;
}

.calendar-day.empty {
  background: none;
  cursor: default;
}

.calendar-day.marked {
  color: #fff;
  font-weight: 600;
}

.calendar-day.today {
  border-bottom: 3px solid red;
}

.day-num {
  font-size: 10px;
  line-height: 1;
  opacity: 0.75;
}

.day-emoji {
  font-size: 17px;
  line-height: 1.2;
}

.day-count {
  font-size: 15px;
  font-weight: 700;
  line-height: 1.1;
}

.calendar-nav {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 10px;
}

.calendar-nav button {
  background: none;
  border: none;
  color: var(--text-color);
  font-size: 20px;
  padding: 6px 12px;
  border-radius: 12px;
}

.calendar-nav .close-btn {
  font-size: 14px;
  color: var(--text-secondary);
}
</style>
