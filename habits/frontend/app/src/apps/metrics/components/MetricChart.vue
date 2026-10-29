<script setup lang="ts">
import { computed } from 'vue'
import { itemComponents, type ChartPoint, type ItemConfig } from '../types'

const props = defineProps<{
  uid: number // для уникальных id градиентов
  chartType: string
  config: ItemConfig
  points: ChartPoint[]
}>()

const W = 360
const H = 160
const PAD = { left: 8, right: 8, top: 12, bottom: 20 }
const innerW = W - PAD.left - PAD.right
const innerH = H - PAD.top - PAD.bottom

const comps = computed(() => itemComponents(props.config))

/** Диапазон значений по всем компонентам с запасом 8%. */
const range = computed(() => {
  let min = Infinity
  let max = -Infinity
  for (const p of props.points) {
    for (const v of Object.values(p.values)) {
      if (v < min) min = v
      if (v > max) max = v
    }
  }
  if (!isFinite(min)) return { min: 0, max: 1 }
  if (min === max) {
    min -= 1
    max += 1
  }
  const padY = (max - min) * 0.08
  return { min: min - padY, max: max + padY }
})

function y(value: number): number {
  const { min, max } = range.value
  return PAD.top + innerH - ((value - min) / (max - min)) * innerH
}

/** Для столбиков/трубок ось Y начинается с нуля (или min, если он < 0). */
const barBase = computed(() => Math.min(0, range.value.min))

function yBar(value: number): number {
  const max = range.value.max
  const base = barBase.value
  return PAD.top + innerH - ((value - base) / (max - base)) * innerH
}

function x(i: number): number {
  const n = props.points.length
  if (n === 1) return PAD.left + innerW / 2
  return PAD.left + (i / (n - 1)) * innerW
}

/** Центры столбиков/трубок: равномерная сетка с настраиваемой шириной/зазором. */
const barLayout = computed(() => {
  const n = props.points.length
  if (n === 0) return { centers: [], width: 0 }
  const cfg = props.config.bars ?? {}
  const step = innerW / n
  let width = cfg.width ?? Math.max(4, step * 0.55)
  if (cfg.gap !== undefined) {
    // если заданы ширина+зазор и всё помещается — размещаем от центра
    const total = n * width + (n - 1) * cfg.gap
    if (total <= innerW) {
      const start = PAD.left + (innerW - total) / 2 + width / 2
      return {
        centers: Array.from({ length: n }, (_, i) => start + i * (width + cfg.gap!)),
        width,
      }
    }
  }
  width = Math.min(width, step * 0.9)
  return {
    centers: Array.from({ length: n }, (_, i) => PAD.left + step * (i + 0.5)),
    width,
  }
})

function linePath(key: string): string {
  return props.points
    .filter((p) => key in p.values)
    .map((p, i, arr) => {
      const idx = props.points.indexOf(p)
      return `${i === 0 && arr.length >= 0 ? 'M' : 'L'}${x(idx).toFixed(1)},${y(p.values[key]).toFixed(1)}`
    })
    .join(' ')
}

function fillPath(key: string): string {
  const pts = props.points.filter((p) => key in p.values)
  if (pts.length < 2) return ''
  const first = props.points.indexOf(pts[0])
  const last = props.points.indexOf(pts[pts.length - 1])
  return `${linePath(key)} L${x(last).toFixed(1)},${PAD.top + innerH} L${x(first).toFixed(1)},${PAD.top + innerH} Z`
}

/** Сегменты стопки для столбика: [0] — весь столбец, дальше — снизу вверх. */
function stackSegments(p: ChartPoint): { color: string; from: number; to: number }[] {
  const list = comps.value.filter((c) => c.key in p.values)
  if (list.length === 0) return []
  const segments: { color: string; from: number; to: number }[] = []
  const base = barBase.value
  // первый компонент — полный столбец
  segments.push({ color: list[0].color, from: base, to: p.values[list[0].key] })
  let acc = base
  for (const c of list.slice(1)) {
    const v = p.values[c.key]
    segments.push({ color: c.color, from: acc, to: acc + v })
    acc += v
  }
  return segments
}

/** Внутренняя трубка: min/max, если есть, иначе короткий сегмент вокруг значения. */
function tubeInner(p: ChartPoint): { from: number; to: number } | null {
  const vals = p.values
  if ('min' in vals && 'max' in vals) return { from: vals.min, to: vals.max }
  const keys = Object.keys(vals)
  if (keys.length === 0) return null
  const v = vals[keys[0]]
  const span = (range.value.max - range.value.min) * 0.04
  return { from: v - span, to: v + span }
}

const bgGradient = computed(() => {
  const bg = props.config.background
  if (!bg?.from) return null
  return { from: bg.from, to: bg.to || bg.from }
})

function fmtDate(iso: string): string {
  const d = new Date(iso)
  return `${String(d.getDate()).padStart(2, '0')}.${String(d.getMonth() + 1).padStart(2, '0')}`
}

const fmtVal = (v: number) => (Math.abs(v) >= 100 ? v.toFixed(0) : v.toFixed(1))
</script>

<template>
  <svg :viewBox="`0 0 ${W} ${H}`" class="chart" preserveAspectRatio="xMidYMid meet">
    <defs>
      <linearGradient v-if="bgGradient" :id="`bg-${uid}`" x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" :stop-color="bgGradient.from" />
        <stop offset="1" :stop-color="bgGradient.to" />
      </linearGradient>
      <linearGradient
        v-for="c in comps"
        :id="`fill-${uid}-${c.key || 'd'}`"
        :key="c.key"
        x1="0"
        y1="0"
        x2="0"
        y2="1"
      >
        <stop offset="0" :stop-color="c.color" stop-opacity="0.4" />
        <stop offset="1" :stop-color="c.color" stop-opacity="0" />
      </linearGradient>
    </defs>

    <!-- фон (прозрачный, если не задан) -->
    <rect v-if="bgGradient" x="0" y="0" :width="W" :height="H" rx="8" :fill="`url(#bg-${uid})`" />

    <template v-if="points.length === 0">
      <text :x="W / 2" :y="H / 2" text-anchor="middle" class="empty-text">нет данных</text>
    </template>

    <!-- ЛИНЕЙНЫЙ -->
    <template v-else-if="chartType === 'line'">
      <template v-for="c in comps" :key="c.key">
        <path
          v-if="config.line?.fill"
          :d="fillPath(c.key)"
          :fill="`url(#fill-${uid}-${c.key || 'd'})`"
        />
        <path :d="linePath(c.key)" fill="none" :stroke="c.color" stroke-width="2" stroke-linejoin="round" />
        <template v-for="(p, i) in points" :key="p.at">
          <circle
            v-if="c.key in p.values"
            :cx="x(i)"
            :cy="y(p.values[c.key])"
            r="3.2"
            :fill="config.line?.nodeColor || c.color"
          />
        </template>
      </template>
    </template>

    <!-- СТОЛБИКИ (с сегментами-компонентами) -->
    <template v-else-if="chartType === 'bars'">
      <template v-for="(p, i) in points" :key="p.at">
        <rect
          v-for="(seg, si) in stackSegments(p)"
          :key="si"
          :x="barLayout.centers[i] - barLayout.width / 2"
          :y="Math.min(yBar(seg.from), yBar(seg.to))"
          :width="barLayout.width"
          :height="Math.abs(yBar(seg.from) - yBar(seg.to))"
          :fill="seg.color"
          rx="2"
        />
      </template>
    </template>

    <!-- ТРУБКИ (MiFit-style) -->
    <template v-else-if="chartType === 'tubes'">
      <template v-for="(p, i) in points" :key="p.at">
        <rect
          :x="barLayout.centers[i] - 3"
          :y="PAD.top"
          width="6"
          :height="innerH"
          rx="3"
          :fill="config.tubes?.tubeColor || 'rgba(128,128,128,0.25)'"
        />
        <rect
          v-if="tubeInner(p)"
          :x="barLayout.centers[i] - 3"
          :y="Math.min(y(tubeInner(p)!.from), y(tubeInner(p)!.to))"
          width="6"
          :height="Math.max(6, Math.abs(y(tubeInner(p)!.from) - y(tubeInner(p)!.to)))"
          rx="3"
          :fill="comps[0].color"
        />
      </template>
    </template>

    <!-- подписи -->
    <template v-if="points.length > 0">
      <text :x="PAD.left" :y="H - 6" class="axis-text">{{ fmtDate(points[0].at) }}</text>
      <text v-if="points.length > 1" :x="W - PAD.right" :y="H - 6" text-anchor="end" class="axis-text">
        {{ fmtDate(points[points.length - 1].at) }}
      </text>
      <text v-if="chartType === 'line'" :x="PAD.left" :y="PAD.top - 2" class="axis-text">
        {{ fmtVal(range.max) }}
      </text>
    </template>
  </svg>
</template>

<style scoped>
.chart {
  width: 100%;
  display: block;
}

.axis-text {
  font-size: 9px;
  fill: var(--text-secondary);
}

.empty-text {
  font-size: 12px;
  fill: var(--text-secondary);
}
</style>
