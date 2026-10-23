<script setup lang="ts">
import { computed } from 'vue'

// Простой SVG-график: столбики по дням + линия цели + скользящее среднее.
// series: либо один ряд (bars), либо стек (stacked) для распределения по приёмам.
export interface ChartSeries {
  label: string
  color: string
  values: number[]
}

const props = defineProps<{
  title: string
  labels: string[] // даты (YYYY-MM-DD)
  series: ChartSeries[]
  stacked?: boolean
  goal?: number[] // линия цели по дням (0 — нет)
  avg?: number[] // скользящее среднее
  overColor?: boolean // подсветка столбиков выше цели
}>()

const W = 360
const H = 150
const PAD = { l: 6, r: 6, t: 16, b: 18 }
const innerW = W - PAD.l - PAD.r
const innerH = H - PAD.t - PAD.b

const maxV = computed(() => {
  let m = 0
  for (let i = 0; i < props.labels.length; i++) {
    let v = 0
    for (const s of props.series) v = props.stacked ? v + (s.values[i] ?? 0) : Math.max(v, s.values[i] ?? 0)
    if (v > m) m = v
    if (props.goal && (props.goal[i] ?? 0) > m) m = props.goal[i]!
  }
  return m > 0 ? m * 1.08 : 1
})

function y(v: number): number {
  return PAD.t + innerH - (v / maxV.value) * innerH
}

const slot = computed(() => innerW / Math.max(1, props.labels.length))
const barW = computed(() => Math.max(2, Math.min(22, slot.value * 0.66)))

function x(i: number): number {
  return PAD.l + slot.value * i + (slot.value - barW.value) / 2
}

/** Сегменты стека для дня i (снизу вверх). */
function stackSegments(i: number): { y: number; h: number; color: string }[] {
  let acc = 0
  const out: { y: number; h: number; color: string }[] = []
  for (const s of props.series) {
    const v = s.values[i] ?? 0
    if (v <= 0) continue
    const y0 = y(acc + v)
    const h = y(acc) - y0
    out.push({ y: y0, h, color: s.color })
    acc += v
  }
  return out
}

const goalPath = computed(() => {
  if (!props.goal) return ''
  let d = ''
  for (let i = 0; i < props.labels.length; i++) {
    const g = props.goal[i] ?? 0
    if (g <= 0) continue
    const px = PAD.l + slot.value * i
    const py = y(g)
    d += `${d ? 'L' : 'M'}${px.toFixed(1)} ${py.toFixed(1)} L${(px + slot.value).toFixed(1)} ${py.toFixed(1)} `
  }
  return d
})

const avgPath = computed(() => {
  if (!props.avg) return ''
  let d = ''
  for (let i = 0; i < props.labels.length; i++) {
    const v = props.avg[i]
    if (v === undefined || v <= 0) continue
    const px = PAD.l + slot.value * i + slot.value / 2
    const py = y(v)
    d += `${d ? 'L' : 'M'}${px.toFixed(1)} ${py.toFixed(1)} `
  }
  return d
})

// подписи дат — первая, последняя и середина
const ticks = computed(() => {
  const n = props.labels.length
  if (n === 0) return []
  const idx = n <= 4 ? props.labels.map((_, i) => i) : [0, Math.floor(n / 2), n - 1]
  return idx.map((i) => ({
    x: PAD.l + slot.value * i + slot.value / 2,
    text: props.labels[i]!.slice(5).replace('-', '.'),
  }))
})

function barColor(i: number): string {
  const base = props.series[0]?.color ?? 'var(--accent-color)'
  if (props.overColor && props.goal) {
    const g = props.goal[i] ?? 0
    const v = props.series[0]?.values[i] ?? 0
    if (g > 0 && v > g) return '#ef4444'
  }
  return base
}
</script>

<template>
  <div class="chart card-glass">
    <div class="c-title">{{ title }}</div>
    <svg :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" class="c-svg">
      <template v-for="(_, i) in labels" :key="i">
        <template v-if="stacked">
          <rect
            v-for="(seg, j) in stackSegments(i)"
            :key="j"
            :x="x(i)"
            :y="seg.y"
            :width="barW"
            :height="Math.max(0, seg.h)"
            :fill="seg.color"
            rx="1.5"
          />
        </template>
        <rect
          v-else-if="(series[0]?.values[i] ?? 0) > 0"
          :x="x(i)"
          :y="y(series[0]!.values[i]!)"
          :width="barW"
          :height="Math.max(1, PAD.t + innerH - y(series[0]!.values[i]!))"
          :fill="barColor(i)"
          rx="1.5"
        />
      </template>
      <path v-if="goalPath" :d="goalPath" fill="none" stroke="#f59e0b" stroke-width="1.6" stroke-dasharray="4 3" />
      <path v-if="avgPath" :d="avgPath" fill="none" stroke="#a78bfa" stroke-width="1.6" />
      <text v-for="t in ticks" :key="t.x" :x="t.x" :y="H - 4" class="tick">{{ t.text }}</text>
    </svg>
    <div v-if="stacked || goal || avg" class="legend">
      <span v-for="s in (stacked ? series : series.slice(0, 1))" :key="s.label" class="lg">
        <i :style="{ background: s.color }"></i>{{ s.label }}
      </span>
      <span v-if="goal" class="lg"><i class="dash"></i>цель</span>
      <span v-if="avg" class="lg"><i style="background: #a78bfa"></i>среднее 7 дн</span>
    </div>
  </div>
</template>

<style scoped>
.chart {
  background: var(--card-color);
  border-radius: 8px;
  padding: 10px 12px;
  margin-bottom: 10px;
}

.c-title {
  font-size: 13px;
  font-weight: 600;
  margin-bottom: 4px;
}

.c-svg {
  width: 100%;
  height: auto;
  display: block;
}

.tick {
  font-size: 9px;
  fill: var(--text-secondary);
  text-anchor: middle;
}

.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 4px;
}

.lg {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 10px;
  color: var(--text-secondary);
}

.lg i {
  width: 10px;
  height: 6px;
  border-radius: 2px;
  display: inline-block;
}

.lg i.dash {
  background: repeating-linear-gradient(90deg, #f59e0b 0 3px, transparent 3px 5px);
}
</style>
