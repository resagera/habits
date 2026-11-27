<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { showToast } from '../../../shared/toast'
import * as metricsApi from '../api'
import { groupValues, itemComponents, type ChartPoint, type MetricItem } from '../types'
import MetricChart from './MetricChart.vue'

const props = defineProps<{ item: MetricItem; collapsed: boolean }>()
const emit = defineEmits<{ toggle: []; openSettings: [] }>()

const points = ref<ChartPoint[]>([])
const loading = ref(true)

const comps = computed(() => itemComponents(props.item.config))

// --- модалка значения (добавление/редактирование) ---
const valueModal = ref(false)
const editingPoint = ref<ChartPoint | null>(null)
const valueAt = ref('')
const valueInputs = ref<Record<string, string>>({})
const confirmDeletePoint = ref(false)
const saving = ref(false)

onMounted(load)

async function load() {
  loading.value = true
  try {
    points.value = groupValues((await metricsApi.fetchValues(props.item.id)).values)
  } catch {
    showToast('Не удалось загрузить значения')
  } finally {
    loading.value = false
  }
}

function toLocalInput(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function fmtAt(iso: string): string {
  return new Date(iso).toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    year: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function openAddValue() {
  editingPoint.value = null
  valueAt.value = toLocalInput(new Date()) // по умолчанию — сейчас
  valueInputs.value = Object.fromEntries(comps.value.map((c) => [c.key, '']))
  confirmDeletePoint.value = false
  valueModal.value = true
}

function openEditValue(p: ChartPoint) {
  editingPoint.value = p
  valueAt.value = toLocalInput(new Date(p.at))
  valueInputs.value = Object.fromEntries(
    comps.value.map((c) => [c.key, c.key in p.values ? String(p.values[c.key]) : '']),
  )
  confirmDeletePoint.value = false
  valueModal.value = true
}

function parseInputs(): Record<string, number> | null {
  const out: Record<string, number> = {}
  for (const c of comps.value) {
    const raw = (valueInputs.value[c.key] ?? '').trim().replace(',', '.')
    if (raw === '') continue
    const num = parseFloat(raw)
    if (isNaN(num)) return null
    out[c.key] = num
  }
  return Object.keys(out).length ? out : null
}

async function saveValue() {
  const values = parseInputs()
  if (!values) {
    showToast('Введите хотя бы одно числовое значение')
    return
  }
  const at = new Date(valueAt.value).toISOString()
  saving.value = true
  try {
    if (editingPoint.value) {
      const p = editingPoint.value
      for (const [key, num] of Object.entries(values)) {
        if (key in p.ids) {
          await metricsApi.updateValue(p.ids[key], { at, value: num })
        } else {
          await metricsApi.addValues(props.item.id, at, { [key]: num })
        }
      }
      // компоненты, очищенные при редактировании — удаляем
      for (const [key, id] of Object.entries(p.ids)) {
        if (!(key in values)) await metricsApi.deleteValue(id)
      }
    } else {
      await metricsApi.addValues(props.item.id, at, values)
    }
    valueModal.value = false
    await load()
  } catch {
    showToast('Не удалось сохранить')
  } finally {
    saving.value = false
  }
}

async function deletePoint() {
  const p = editingPoint.value
  if (!p) return
  if (!confirmDeletePoint.value) {
    confirmDeletePoint.value = true
    setTimeout(() => (confirmDeletePoint.value = false), 3000)
    return
  }
  saving.value = true
  try {
    for (const id of Object.values(p.ids)) await metricsApi.deleteValue(id)
    valueModal.value = false
    await load()
  } catch {
    showToast('Не удалось удалить')
  } finally {
    saving.value = false
  }
}

function pointSummary(p: ChartPoint): string {
  return comps.value
    .filter((c) => c.key in p.values)
    .map((c) => (c.label ? `${c.label}: ` : '') + p.values[c.key])
    .join(' · ')
}

const recentFirst = computed(() => [...points.value].reverse())
</script>

<template>
  <div class="metric-item">
    <div class="item-head">
      <button class="item-toggle" @click="emit('toggle')">
        <span class="chevron" :class="{ open: !collapsed }">▸</span>
        {{ item.name }}
        <span class="count">{{ points.length }}</span>
      </button>
      <span>
        <button class="icon-btn" title="Добавить значение" @click="openAddValue">＋</button>
        <button class="icon-btn" title="Настройки графика" @click="emit('openSettings')">⚙️</button>
      </span>
    </div>

    <template v-if="!collapsed">
      <div v-if="loading" class="hint">Загрузка…</div>
      <template v-else>
        <MetricChart :uid="item.id" :chart-type="item.chart_type" :config="item.config" :points="points" />

        <!-- легенда для мультикомпонентных -->
        <div v-if="comps.length > 1" class="legend">
          <span v-for="c in comps" :key="c.key" class="legend-item">
            <span class="legend-dot" :style="{ background: c.color }"></span>{{ c.label || c.key }}
          </span>
        </div>

        <!-- список метрик под графиком -->
        <div v-if="points.length" class="values-list">
          <button v-for="p in recentFirst" :key="p.at" class="value-row" @click="openEditValue(p)">
            <span class="value-at">{{ fmtAt(p.at) }}</span>
            <span class="value-nums">{{ pointSummary(p) }}</span>
          </button>
        </div>
      </template>
    </template>

    <!-- Модалка значения -->
    <div v-if="valueModal" class="modal" @click.self="valueModal = false">
      <div class="modal-content">
        <h3>{{ editingPoint ? 'Редактирование точки' : 'Новая точка' }}</h3>
        <input v-model="valueAt" type="datetime-local" />
        <label v-for="c in comps" :key="c.key" class="value-input">
          <span v-if="c.label" class="value-label">
            <span class="legend-dot" :style="{ background: c.color }"></span>{{ c.label }}
          </span>
          <input v-model="valueInputs[c.key]" inputmode="decimal" placeholder="значение" />
        </label>
        <button class="btn primary" :disabled="saving" @click="saveValue">💾 Сохранить</button>
        <button v-if="editingPoint" class="btn danger" :disabled="saving" @click="deletePoint">
          {{ confirmDeletePoint ? 'Точно удалить точку?' : '🗑 Удалить' }}
        </button>
        <button class="btn" @click="valueModal = false">Отмена</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.metric-item {
  border-top: 1px solid var(--hover-bg-color);
  padding: 6px 0;
}

.item-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.item-toggle {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 6px;
  background: none;
  border: none;
  color: var(--text-color);
  font-weight: 600;
  text-align: left;
  padding: 4px 0;
}

.chevron {
  display: inline-block;
  transition: transform 0.15s;
  color: var(--text-secondary);
}

.chevron.open {
  transform: rotate(90deg);
}

.count {
  font-size: 11px;
  color: var(--text-secondary);
}

.icon-btn {
  background: none;
  border: none;
  padding: 3px 6px;
}

.hint {
  color: var(--text-secondary);
  font-size: 13px;
  padding: 8px 0;
}

.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 2px 0 6px;
}

.legend-item {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}

.legend-dot {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  border: 1px solid var(--hover-bg-color);
}

.values-list {
  max-height: 180px;
  overflow-y: auto;
}

.value-row {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  background: none;
  border: none;
  border-top: 1px solid var(--hover-bg-color);
  color: var(--text-color);
  padding: 6px 2px;
  font-size: 13px;
  text-align: left;
}

.value-at {
  color: var(--text-secondary);
  flex: none;
}

.value-nums {
  overflow-wrap: anywhere;
  text-align: right;
}

.modal-content input {
  width: 100%;
  margin-top: 8px;
}

.value-input {
  display: block;
  text-align: left;
}

.value-label {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 8px;
}

.btn {
  display: block;
  width: 100%;
  margin-top: 10px;
  padding: 10px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn.danger {
  background: #b91c1c;
  color: #fff;
}
</style>
