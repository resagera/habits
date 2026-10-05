<script setup lang="ts">
/**
 * График курса за период.
 *
 * Своим, а не общим Sparkline: тот считает ось от нуля, и курс, который весь
 * месяц держится около 385, выглядел бы прямой линией у верхнего края.
 * Здесь масштаб от минимума до максимума с запасом — видно именно колебание.
 */
import { computed } from 'vue'

const props = defineProps<{
  code: string
  base: string
  days: string[]
  rates: number[]
}>()

const W = 320
const H = 64
const PAD = 3

const range = computed(() => {
  const v = props.rates
  if (!v.length) return { min: 0, max: 1 }
  let min = Math.min(...v)
  let max = Math.max(...v)
  if (min === max) {
    // ровная линия: раздвигаем, иначе делили бы на ноль
    min -= Math.abs(min) * 0.001 || 1
    max += Math.abs(max) * 0.001 || 1
  }
  const pad = (max - min) * 0.1
  return { min: min - pad, max: max + pad }
})

function y(value: number): number {
  const { min, max } = range.value
  return H - PAD - ((value - min) / (max - min)) * (H - PAD * 2)
}

const path = computed(() => {
  const v = props.rates
  if (v.length < 2) return ''
  const step = W / (v.length - 1)
  return v.map((val, i) => `${i ? 'L' : 'M'}${(i * step).toFixed(1)},${y(val).toFixed(1)}`).join(' ')
})

const fillPath = computed(() => (path.value ? `${path.value} L${W},${H} L0,${H} Z` : ''))

const first = computed(() => props.rates[0] ?? 0)
const last = computed(() => props.rates[props.rates.length - 1] ?? 0)

/** Изменение за период — то, ради чего на график и смотрят. */
const change = computed(() => {
  if (!first.value || !last.value) return 0
  return ((last.value - first.value) / first.value) * 100
})

// Растёт — зелёный, падает — красный. Цвет один на линию и на подпись.
const color = computed(() => (change.value >= 0 ? '#46c46b' : '#e5534b'))

function fmt(v: number): string {
  if (!v) return '—'
  const digits = v >= 100 ? 2 : v >= 1 ? 4 : 8
  return v.toFixed(digits).replace(/\.?0+$/, '')
}

const period = computed(() =>
  props.days.length ? `${props.days[0]} → ${props.days[props.days.length - 1]}` : '',
)
</script>

<template>
  <div class="chart">
    <div class="head">
      <span class="code">1 {{ base.toUpperCase() }} = {{ fmt(last) }} {{ code.toUpperCase() }}</span>
      <span class="change" :style="{ color }">
        {{ change >= 0 ? '+' : '' }}{{ change.toFixed(2) }}%
      </span>
    </div>
    <svg v-if="rates.length >= 2" :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" class="svg">
      <path :d="fillPath" :fill="color" opacity="0.15" />
      <path :d="path" fill="none" :stroke="color" stroke-width="1.6" stroke-linejoin="round" />
    </svg>
    <div v-else class="empty">данных за период нет</div>
    <div class="foot">
      <span>{{ period }}</span>
      <span>{{ fmt(first) }} → {{ fmt(last) }}</span>
    </div>
  </div>
</template>

<style scoped>
.chart {
  background: var(--card-color);
  border-radius: 10px;
  padding: 10px 12px;
}

.head {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 8px;
  font-size: 13px;
  margin-bottom: 4px;
}

.code {
  font-weight: 600;
}

.change {
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}

.svg {
  width: 100%;
  height: 64px;
  display: block;
  background: var(--bg-secondary);
  border-radius: 6px;
}

.empty {
  height: 64px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-secondary);
  border-radius: 6px;
  font-size: 12px;
  color: var(--text-secondary);
}

.foot {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  color: var(--text-secondary);
  margin-top: 4px;
}
</style>
