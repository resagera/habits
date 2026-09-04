<script setup lang="ts">
// Круговая диаграмма с легендой. Рисуется вручную на SVG: ради одной диаграммы
// тянуть библиотеку графиков незачем, а обводка окружности через
// stroke-dasharray даёт кликабельные сегменты бесплатно.
import { computed } from 'vue'

export interface Slice {
  key: string
  name: string
  value: number
  color?: string
}

const props = withDefaults(defineProps<{
  slices: Slice[]
  hide?: boolean
  format?: (v: number) => string
}>(), { hide: false })

const emit = defineEmits<{ pick: [key: string] }>()

// Палитра различима и в светлой, и в тёмной теме; «Не разобрано» отдельным
// серым цветом — это не группа, а повод пойти разметить.
const PALETTE = [
  '#3b82f6', '#22c55e', '#f59e0b', '#ef4444', '#a855f7',
  '#06b6d4', '#eab308', '#ec4899', '#14b8a6', '#f97316',
]

const total = computed(() => props.slices.reduce((s, x) => s + Math.max(0, x.value), 0))

const R = 60
const C = 2 * Math.PI * R

/** Сегменты как обводка одной окружности: длина дуги = доля от периметра. */
const arcs = computed(() => {
  let offset = 0
  return props.slices.map((s, i) => {
    const share = total.value > 0 ? Math.max(0, s.value) / total.value : 0
    const arc = {
      key: s.key,
      name: s.name,
      value: s.value,
      share: share * 100,
      color: s.color || (s.key === 'none' ? '#9ca3af' : PALETTE[i % PALETTE.length]),
      dash: `${(share * C).toFixed(2)} ${(C - share * C).toFixed(2)}`,
      offset: (-offset * C).toFixed(2),
    }
    offset += share
    return arc
  })
})

function fmt(v: number): string {
  if (props.hide) return '•••'
  return props.format ? props.format(v) : String(Math.round(v))
}
</script>

<template>
  <div class="pie-wrap">
    <svg v-if="total > 0" class="pie" viewBox="0 0 160 160" role="img">
      <g transform="translate(80,80) rotate(-90)">
        <circle v-for="a in arcs" :key="a.key" :r="R" fill="none" :stroke="a.color"
                stroke-width="26" :stroke-dasharray="a.dash" :stroke-dashoffset="a.offset"
                class="arc" @click="emit('pick', a.key)">
          <title>{{ a.name }}: {{ fmt(a.value) }} ({{ a.share.toFixed(1) }}%)</title>
        </circle>
      </g>
    </svg>
    <p v-else class="hint">За период трат нет.</p>

    <div class="legend">
      <button v-for="a in arcs" :key="a.key" class="item" @click="emit('pick', a.key)">
        <i :style="{ background: a.color }" />
        <span class="nm">{{ a.name }}</span>
        <span class="val">{{ fmt(a.value) }}</span>
        <span class="pct">{{ a.share.toFixed(1) }}%</span>
      </button>
    </div>
  </div>
</template>

<style scoped>
.pie-wrap {
  display: flex;
  gap: 12px;
  align-items: center;
  flex-wrap: wrap;
}

.pie {
  width: 160px;
  height: 160px;
  flex: 0 0 auto;
}

.arc {
  cursor: pointer;
  transition: opacity 0.15s;
}

.arc:hover {
  opacity: 0.75;
}

.legend {
  flex: 1;
  min-width: 180px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.item {
  display: grid;
  grid-template-columns: 10px 1fr auto auto;
  align-items: center;
  gap: 8px;
  background: none;
  border: none;
  color: var(--text-color);
  font-size: 12px;
  padding: 3px 0;
  cursor: pointer;
  text-align: left;
}

.item i {
  width: 10px;
  height: 10px;
  border-radius: 2px;
}

.nm {
  overflow-wrap: anywhere;
}

.val {
  font-variant-numeric: tabular-nums;
}

.pct {
  color: var(--text-secondary);
  min-width: 42px;
  text-align: right;
}

.hint {
  font-size: 13px;
  color: var(--text-secondary);
}
</style>
