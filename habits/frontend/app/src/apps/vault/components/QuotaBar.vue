<script setup lang="ts">
import { computed } from 'vue'
import type { VaultQuota } from '../types'

const props = defineProps<{ quota: VaultQuota }>()

export interface Sizes {
  used: string
  total: string
}

function fmt(bytes: number): string {
  if (bytes >= 1 << 30) return (bytes / (1 << 30)).toFixed(1) + ' ГБ'
  if (bytes >= 1 << 20) return (bytes / (1 << 20)).toFixed(1) + ' МБ'
  if (bytes >= 1 << 10) return Math.round(bytes / (1 << 10)) + ' КБ'
  return bytes + ' Б'
}

const pct = computed(() =>
  props.quota.total_limit ? Math.min(100, (props.quota.used / props.quota.total_limit) * 100) : 0,
)
// 90% — предупреждение: место в сейфе маленькое, упереться в потолок легко
const tight = computed(() => pct.value >= 90)
</script>

<template>
  <div class="quota">
    <div class="bar"><div class="fill" :class="{ tight }" :style="{ width: pct + '%' }"></div></div>
    <div class="text">
      {{ fmt(quota.used) }} из {{ fmt(quota.total_limit) }} · файл до {{ fmt(quota.file_limit) }}
    </div>
  </div>
</template>

<style scoped>
.quota {
  margin-bottom: 10px;
}

.bar {
  height: 4px;
  border-radius: 3px;
  background: var(--bg-secondary);
  overflow: hidden;
}

.fill {
  height: 100%;
  background: var(--accent-color);
  border-radius: 3px;
  transition: width 0.2s;
}

.fill.tight {
  background: #f59e0b;
}

.text {
  margin-top: 4px;
  font-size: 11px;
  color: var(--text-secondary);
}
</style>
