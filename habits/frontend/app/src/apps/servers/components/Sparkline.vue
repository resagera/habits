<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  values: number[]
  /** максимум оси Y; если не задан — максимум данных */
  max?: number
  color: string
  label: string
  current: string
}>()

const W = 320
const H = 56

const path = computed(() => {
  const v = props.values
  if (v.length < 2) return ''
  const max = Math.max(props.max ?? 0, ...v, 1)
  const step = W / (v.length - 1)
  return v
    .map((val, i) => `${i === 0 ? 'M' : 'L'}${(i * step).toFixed(1)},${(H - (val / max) * (H - 4) - 2).toFixed(1)}`)
    .join(' ')
})

const fillPath = computed(() => (path.value ? `${path.value} L${W},${H} L0,${H} Z` : ''))
</script>

<template>
  <div class="spark">
    <div class="spark-head">
      <span class="spark-label">{{ label }}</span>
      <span class="spark-current" :style="{ color }">{{ current }}</span>
    </div>
    <svg v-if="values.length >= 2" :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" class="spark-svg">
      <path :d="fillPath" :fill="color" opacity="0.15" />
      <path :d="path" fill="none" :stroke="color" stroke-width="1.6" stroke-linejoin="round" />
    </svg>
    <div v-else class="spark-empty">история копится…</div>
  </div>
</template>

<style scoped>
.spark {
  margin-top: 8px;
}

.spark-head {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  margin-bottom: 2px;
}

.spark-label {
  color: var(--text-secondary);
}

.spark-current {
  font-weight: 700;
}

.spark-svg {
  width: 100%;
  height: 56px;
  display: block;
  background: var(--bg-secondary);
  border-radius: 6px;
}

.spark-empty {
  height: 56px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-secondary);
  border-radius: 6px;
  font-size: 11px;
  color: var(--text-secondary);
}
</style>
