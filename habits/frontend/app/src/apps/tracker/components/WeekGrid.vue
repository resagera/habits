<script setup lang="ts">
import { computed } from 'vue'
import { addDays, lastWeekStarts, toISODate, weekLabel, WEEKS_TO_SHOW } from '../dates'
import { cellView, pressHandlers } from '../display'
import type { Category, MarkInfo } from '../types'

const props = defineProps<{
  category: Category
  markInfo: (day: string) => MarkInfo | undefined
}>()

const emit = defineEmits<{
  toggle: [day: string]
  increment: [day: string, delta: 1 | -1]
}>()

const dayNames = ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс']
const today = toISODate(new Date())

const weeks = computed(() =>
  lastWeekStarts(WEEKS_TO_SHOW).map((start) => ({
    label: weekLabel(start),
    days: Array.from({ length: 7 }, (_, d) => toISODate(addDays(start, d))),
  })),
)

const isCounter = computed(() => props.category.kind === 'counter')

function view(day: string) {
  return cellView(props.category, props.markInfo(day))
}

function handlers(day: string) {
  if (isCounter.value) {
    return pressHandlers(
      () => emit('increment', day, 1),
      () => emit('increment', day, -1),
    )
  }
  return { click: () => emit('toggle', day) }
}
</script>

<template>
  <div class="weeks">
    <div v-for="week in weeks" :key="week.label" class="week">
      <div class="week-label">{{ week.label }}</div>
      <div class="week-cells">
        <div
          v-for="day in week.days"
          :key="day"
          class="cell"
          :class="{
            active: view(day).marked && !view(day).emoji,
            'current-date': day === today,
            circle: category.style === 'circle' && !isCounter,
            'emoji-cell': category.style === 'emoji' && !isCounter,
            counter: isCounter,
          }"
          :style="view(day).color && view(day).marked ? { backgroundColor: view(day).color! } : {}"
          v-on="handlers(day)"
        >
          <span v-if="isCounter && view(day).count > 0" class="count">{{ view(day).count }}</span>
          <span v-else-if="view(day).emoji" class="emoji">{{ view(day).emoji }}</span>
        </div>
      </div>
    </div>
    <div class="day-labels">
      <div v-for="name in dayNames" :key="name">{{ name }}</div>
    </div>
  </div>
</template>

<style scoped>
.weeks {
  display: flex;
  gap: 4px;
  align-items: flex-start;
  overflow-x: auto;
  padding-bottom: 6px;
}

.week {
  display: flex;
  flex-direction: column;
  align-items: center;
  min-width: 36px;
}

.week-label {
  font-size: 12px;
  margin-bottom: 6px;
  color: var(--text-secondary);
  white-space: pre;
}

.week-cells,
.day-labels {
  display: grid;
  grid-template-rows: repeat(7, 20px);
  gap: 6px;
}

.cell {
  width: 28px;
  height: 20px;
  border-radius: 4px;
  background-color: var(--cell-bg-color);
  cursor: pointer;
  box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.02);
  display: flex;
  align-items: center;
  justify-content: center;
  user-select: none;
  -webkit-user-select: none;
  touch-action: manipulation;
}

.cell.circle {
  border-radius: 50%;
  width: 20px;
  height: 20px;
}

.cell.active {
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.18);
}

.cell.current-date {
  border-bottom: 3px solid red;
}

.cell .emoji {
  font-size: 15px;
  line-height: 1;
}

.cell .count {
  font-size: 12px;
  font-weight: 700;
  color: #fff;
  line-height: 1;
}

.day-labels {
  color: var(--text-secondary);
  font-size: 12px;
  padding-left: 4px;
  margin-top: 24px;
}

@media (max-width: 420px) {
  .cell {
    width: 22px;
    height: 18px;
  }
  .cell.circle {
    width: 18px;
    height: 18px;
  }
  .week {
    min-width: 30px;
  }
  .week-label {
    font-size: 11px;
  }
}
</style>
