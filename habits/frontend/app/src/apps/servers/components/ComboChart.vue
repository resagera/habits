<script setup lang="ts">
import { computed } from 'vue'

// Компактный совмещённый график: CPU (0–100%) и RAM (доля от общего) на одной
// оси 0–100 без подписей. Две линии наложены, две текущие цифры справа сверху.
const props = defineProps<{
  cpu: number[]
  ram: number[]
  ramMax: number
  cpuCurrent: string
  ramCurrent: string
}>()

const W = 320
const H = 40

function line(values: number[], max: number): string {
  if (values.length < 2) return ''
  const step = W / (values.length - 1)
  return values
    .map((v, i) => `${i === 0 ? 'M' : 'L'}${(i * step).toFixed(1)},${(H - (v / max) * (H - 3) - 1.5).toFixed(1)}`)
    .join(' ')
}

const cpuPath = computed(() => line(props.cpu, 100))
const ramPath = computed(() => line(props.ram, Math.max(props.ramMax, 1)))
const ready = computed(() => props.cpu.length >= 2)
</script>

<template>
  <div class="combo">
    <svg v-if="ready" :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" class="combo-svg">
      <path :d="ramPath" fill="none" stroke="#60a5fa" stroke-width="1.4" stroke-linejoin="round" />
      <path :d="cpuPath" fill="none" stroke="#f59e0b" stroke-width="1.4" stroke-linejoin="round" />
    </svg>
    <div v-else class="combo-empty">история копится…</div>
    <div class="combo-legend">
      <span style="color: #f59e0b">CPU {{ cpuCurrent }}</span>
      <span style="color: #60a5fa">RAM {{ ramCurrent }}</span>
    </div>
  </div>
</template>

<style scoped>
.combo {
  margin-top: 6px;
  position: relative;
}

.combo-svg {
  width: 100%;
  height: 40px;
  display: block;
  background: var(--bg-secondary);
  border-radius: 6px;
}

.combo-empty {
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-secondary);
  border-radius: 6px;
  font-size: 11px;
  color: var(--text-secondary);
}

.combo-legend {
  display: flex;
  gap: 12px;
  justify-content: flex-end;
  font-size: 11px;
  font-weight: 700;
  margin-top: 2px;
}
</style>
