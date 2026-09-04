<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { showToast } from '../../../shared/toast'
import * as checkerApi from '../api'
import type { SnapshotDay, SnapshotNode } from '../api'

const props = defineProps<{ groupId: number }>()
const emit = defineEmits<{ close: [] }>()

const stats = ref<Record<string, SnapshotDay>>({})
const loading = ref(true)
// текущий отображаемый месяц (первое число)
const month = ref(startOfMonth(new Date()))
const openDay = ref<string | null>(null)
const dayTree = ref<SnapshotNode | null>(null)

function startOfMonth(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), 1)
}
function ymd(d: Date): string {
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${d.getFullYear()}-${m}-${day}`
}

onMounted(async () => {
  try {
    const { days } = await checkerApi.listSnapshots(props.groupId)
    const map: Record<string, SnapshotDay> = {}
    for (const d of days) map[d.day] = d
    stats.value = map
  } catch {
    showToast('Не удалось загрузить историю')
  } finally {
    loading.value = false
  }
})

const monthLabel = computed(() =>
  month.value.toLocaleDateString('ru-RU', { month: 'long', year: 'numeric' }),
)

// сетка недель (понедельник — первый день)
const weeks = computed(() => {
  const first = month.value
  const start = new Date(first)
  const dow = (first.getDay() + 6) % 7 // 0=пн
  start.setDate(first.getDate() - dow)
  const out: { date: Date; inMonth: boolean; key: string }[][] = []
  const cur = new Date(start)
  for (let w = 0; w < 6; w++) {
    const row: { date: Date; inMonth: boolean; key: string }[] = []
    for (let i = 0; i < 7; i++) {
      row.push({ date: new Date(cur), inMonth: cur.getMonth() === first.getMonth(), key: ymd(cur) })
      cur.setDate(cur.getDate() + 1)
    }
    out.push(row)
    if (cur.getMonth() !== first.getMonth() && cur > first) break
  }
  return out
})

function shiftMonth(delta: number) {
  month.value = new Date(month.value.getFullYear(), month.value.getMonth() + delta, 1)
}

function ratio(key: string): number {
  const s = stats.value[key]
  return s && s.total ? s.done / s.total : 0
}

async function selectDay(key: string) {
  if (!stats.value[key]) return
  openDay.value = key
  dayTree.value = null
  try {
    dayTree.value = (await checkerApi.getSnapshot(props.groupId, key)).data
  } catch {
    showToast('Снимок не найден')
    openDay.value = null
  }
}

// плоское дерево снимка для показа (отступ + пункты)
interface FlatRow {
  depth: number
  group?: string
  item?: string
  done?: boolean
}
function flatten(node: SnapshotNode, depth: number, out: FlatRow[]) {
  out.push({ depth, group: node.name })
  for (const it of node.items) out.push({ depth: depth + 1, item: it.name, done: it.done })
  for (const sub of node.subgroups) flatten(sub, depth + 1, out)
}
const dayRows = computed<FlatRow[]>(() => {
  if (!dayTree.value) return []
  const out: FlatRow[] = []
  flatten(dayTree.value, 0, out)
  return out
})
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content">
      <h3>История по дням</h3>
      <p v-if="loading" class="hint">Загрузка…</p>

      <template v-else-if="openDay">
        <div class="day-head">
          <button class="link-btn" @click="openDay = null">← Календарь</button>
          <span class="day-title">{{ openDay }}</span>
        </div>
        <div v-if="!dayTree" class="hint">Загрузка…</div>
        <div v-else class="snap-tree">
          <div v-for="(r, i) in dayRows" :key="i" :style="{ paddingLeft: r.depth * 12 + 'px' }">
            <div v-if="r.group" class="snap-group">{{ r.group }}</div>
            <div v-else class="snap-item" :class="{ done: r.done }">{{ r.done ? '☑' : '☐' }} {{ r.item }}</div>
          </div>
        </div>
      </template>

      <template v-else>
        <div class="cal-head">
          <button class="link-btn" @click="shiftMonth(-1)">‹</button>
          <span class="cal-month">{{ monthLabel }}</span>
          <button class="link-btn" @click="shiftMonth(1)">›</button>
        </div>
        <div class="cal-dow">
          <span v-for="d in ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс']" :key="d">{{ d }}</span>
        </div>
        <div v-for="(week, wi) in weeks" :key="wi" class="cal-week">
          <button
            v-for="c in week"
            :key="c.key"
            class="cal-cell"
            :class="{ out: !c.inMonth, has: !!stats[c.key] }"
            :style="stats[c.key] ? { '--r': ratio(c.key) } : {}"
            :title="stats[c.key] ? `${stats[c.key].done}/${stats[c.key].total}` : ''"
            @click="selectDay(c.key)"
          >
            {{ c.date.getDate() }}
          </button>
        </div>
        <p class="hint-small">Отмеченные дни — со снимком; насыщенность = доля выполненного. Тап — открыть.</p>
      </template>

      <button class="btn" @click="emit('close')">Закрыть</button>
    </div>
  </div>
</template>

<style scoped>
.hint,
.hint-small {
  color: var(--text-secondary);
  text-align: center;
}
.hint {
  padding: 12px 0;
}
.hint-small {
  font-size: 11px;
  margin: 8px 0 0;
}
.cal-head,
.day-head {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  margin-bottom: 8px;
}
.cal-month {
  font-weight: 600;
  text-transform: capitalize;
}
.link-btn {
  background: none;
  border: none;
  color: var(--accent-color);
  font-size: 18px;
  padding: 2px 8px;
}
.cal-dow {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  font-size: 11px;
  color: var(--text-secondary);
}
.cal-dow span {
  text-align: center;
}
.cal-week {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  gap: 3px;
  margin-top: 3px;
}
.cal-cell {
  aspect-ratio: 1;
  border: none;
  border-radius: 6px;
  background: var(--bg-secondary);
  color: var(--text-color);
  font-size: 12px;
}
.cal-cell.out {
  opacity: 0.35;
}
.cal-cell.has {
  background: color-mix(in srgb, var(--accent-color) calc(30% + var(--r, 0) * 70%), var(--bg-secondary));
  color: #fff;
  font-weight: 600;
}
.day-title {
  font-weight: 600;
}
.snap-tree {
  text-align: left;
  max-height: 50vh;
  overflow: auto;
}
.snap-group {
  font-weight: 700;
  margin-top: 6px;
}
.snap-item {
  font-size: 14px;
  padding: 2px 0;
}
.snap-item.done {
  color: var(--text-secondary);
  text-decoration: line-through;
}
.btn {
  display: block;
  width: 100%;
  margin-top: 12px;
  padding: 10px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
}
</style>
