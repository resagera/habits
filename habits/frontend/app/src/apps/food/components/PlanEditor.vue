<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { loadCollapsed, saveCollapsed } from '../../../shared/collapsed'
import { showToast } from '../../../shared/toast'
import * as foodApi from '../api'
import PlanApplyModal from './PlanApplyModal.vue'
import PlanParticipantsModal from './PlanParticipantsModal.vue'
import PlanSettingsModal from './PlanSettingsModal.vue'
import PlanShareModal from './PlanShareModal.vue'
import PlanSlotModal from './PlanSlotModal.vue'
import {
  MEAL_TYPE_LABELS,
  MEAL_TYPES,
  planDayLabel,
  r0,
  type FoodPlan,
  type FoodPlanDaySummary,
  type FoodPlanSlot,
  type MealType,
} from '../types'

// Редактор плана. Две недели × четыре приёма в сетку на телефоне не влезают,
// поэтому — вертикальный список дней с переключателем недель; день
// раскрывается по тапу.
const props = defineProps<{ planId: number }>()
const emit = defineEmits<{ back: []; changed: [] }>()

const plan = ref<FoodPlan | null>(null)
const loading = ref(true)
const failed = ref(false)
const week = ref(0)
const openDays = ref<Set<number>>(new Set())

// Раскрытые дни живут на сервере (localStorage в Telegram-webview чистится).
// Общий на все планы ключ, поэтому день кодируется как planId * 100 + dayIndex
// — day_index заведомо меньше 100 (лимит плана — 90 дней).
const DAY_KEY = 100
function dayKey(d: number): number {
  return props.planId * DAY_KEY + d
}

async function load(keepOpen = true) {
  loading.value = !plan.value
  failed.value = false
  try {
    plan.value = (await foodApi.fetchPlan(props.planId)).plan
    if (!keepOpen) openDays.value = new Set()
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  const saved = await loadCollapsed('food_plan_open')
  openDays.value = new Set(
    [...saved].filter((k) => Math.floor(k / DAY_KEY) === props.planId).map((k) => k % DAY_KEY),
  )
  savedKeys = saved
  await load()
})

// ключи ЧУЖИХ планов сохраняем как есть — иначе открытие одного плана
// стирало бы состояние остальных
let savedKeys = new Set<number>()

const weeks = computed(() => (plan.value ? Math.ceil(plan.value.days / 7) : 1))

const weekDays = computed(() => {
  if (!plan.value) return []
  const out: number[] = []
  for (let d = week.value * 7; d < Math.min((week.value + 1) * 7, plan.value.days); d++) out.push(d)
  return out
})

/** Слоты дня, сгруппированные по приёмам пищи в привычном порядке. */
function daySlots(dayIndex: number): { type: MealType; slots: FoodPlanSlot[] }[] {
  const all = (plan.value?.slots ?? []).filter((s) => s.day_index === dayIndex)
  return MEAL_TYPES.map((type) => ({ type, slots: all.filter((s) => s.meal_type === type) })).filter(
    (g) => g.slots.length > 0,
  )
}

function daySummary(dayIndex: number): FoodPlanDaySummary | undefined {
  return plan.value?.summary[dayIndex]
}

function participantLabel(id: number | null): string {
  if (!id || !plan.value) return ''
  const p = plan.value.participants.find((x) => x.id === id)
  return p ? `${p.emoji} ${p.name}`.trim() : ''
}

function participantName(id: number): string {
  return participantLabel(id) || '—'
}

function toggleDay(d: number) {
  const s = new Set(openDays.value)
  if (s.has(d)) s.delete(d)
  else s.add(d)
  openDays.value = s
  const others = [...savedKeys].filter((k) => Math.floor(k / DAY_KEY) !== props.planId)
  savedKeys = new Set([...others, ...[...s].map(dayKey)])
  saveCollapsed('food_plan_open', savedKeys)
}

// --- модалки ---
const slotModal = ref<{ dayIndex: number; slot: FoodPlanSlot | null } | null>(null)
const applyDay = ref<number | null>(null)
const settingsOpen = ref(false)
const partsOpen = ref(false)
const shareOpen = ref(false)

function openSlot(dayIndex: number, slot: FoodPlanSlot | null) {
  if (!plan.value?.can_edit) return
  slotModal.value = { dayIndex, slot }
}

async function onSlotSaved() {
  slotModal.value = null
  await load()
  emit('changed')
}

// --- копирование дня ---
const copyFrom = ref<number | null>(null)
const copyTo = ref(0)
const copyBusy = ref(false)

function startCopy(d: number) {
  copyFrom.value = d
  // по умолчанию — тот же день следующей недели, если он есть
  const next = d + 7
  copyTo.value = plan.value && next < plan.value.days ? next : d === 0 ? 1 : 0
}

async function doCopy() {
  if (copyFrom.value === null || !plan.value) return
  copyBusy.value = true
  try {
    await foodApi.copyPlanDays(plan.value.id, copyFrom.value, copyTo.value, 1)
    showToast(`День скопирован в «День ${copyTo.value + 1}»`)
    copyFrom.value = null
    await load()
    emit('changed')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось скопировать')
  } finally {
    copyBusy.value = false
  }
}

// --- копирование недели целиком ---
const weekCopyOpen = ref(false)
const weekCopyTo = ref(0)

function startWeekCopy() {
  weekCopyOpen.value = !weekCopyOpen.value
  weekCopyTo.value = week.value === 0 && weeks.value > 1 ? 1 : 0
}

async function doWeekCopy() {
  if (!plan.value) return
  const count = weekDays.value.length
  const from = week.value * 7
  const to = weekCopyTo.value * 7
  // целевая неделя может быть короче исходной (хвост плана) — не выходим за края
  if (to + count > plan.value.days) {
    showToast(`В неделю ${weekCopyTo.value + 1} столько дней не помещается`)
    return
  }
  copyBusy.value = true
  try {
    const { copied } = await foodApi.copyPlanDays(plan.value.id, from, to, count)
    showToast(`Скопировано приёмов: ${copied}`)
    weekCopyOpen.value = false
    await load()
    emit('changed')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось скопировать неделю')
  } finally {
    copyBusy.value = false
  }
}

async function onPlanChanged(deleted = false) {
  settingsOpen.value = false
  if (deleted) {
    emit('changed')
    emit('back')
    return
  }
  await load()
  emit('changed')
}
</script>

<template>
  <div v-if="loading" class="hint">Загрузка…</div>
  <p v-else-if="failed || !plan" class="hint">
    Не удалось загрузить <button class="retry" @click="load()">повторить</button>
  </p>

  <template v-else>
    <div class="head">
      <button class="back" @click="emit('back')">‹</button>
      <div class="head-title">
        <b>{{ plan.name }}</b>
        <span class="head-sub">
          {{ plan.days }} дн.
          <template v-if="!plan.is_owner"> · от {{ plan.owner_name || 'другого пользователя' }}</template>
          <template v-if="!plan.can_edit"> · только чтение</template>
        </span>
      </div>
    </div>

    <p v-if="plan.description" class="desc">{{ plan.description }}</p>

    <!-- подписанные кнопки, а не мелкие иконки: участников иначе не найти -->
    <div v-if="plan.is_owner" class="plan-actions">
      <button class="pact" @click="partsOpen = true">
        👥 Участники<template v-if="plan.participants.length"> · {{ plan.participants.length }}</template>
      </button>
      <button class="pact" @click="shareOpen = true">📤 Поделиться</button>
      <button class="pact" @click="settingsOpen = true">⚙️ Настройки</button>
    </div>

    <div v-if="weeks > 1" class="weeks">
      <button
        v-for="w in weeks"
        :key="w"
        class="wtab"
        :class="{ on: week === w - 1 }"
        @click="week = w - 1"
      >
        Неделя {{ w }}
      </button>
      <button class="wtab copy-week" title="Скопировать эту неделю" @click="startWeekCopy">⧉</button>
    </div>

    <div v-if="weekCopyOpen" class="copy-box week-copy">
      <span>Неделю {{ week + 1 }} ({{ weekDays.length }} дн.) скопировать в</span>
      <select v-model.number="weekCopyTo">
        <option v-for="w in weeks" :key="w" :value="w - 1" :disabled="w - 1 === week">
          неделю {{ w }}
        </option>
      </select>
      <button class="act primary" :disabled="copyBusy" @click="doWeekCopy">ОК</button>
      <button class="act" @click="weekCopyOpen = false">✕</button>
    </div>

    <div v-for="d in weekDays" :key="d" class="day card-glass">
      <button class="day-head" @click="toggleDay(d)">
        <span class="day-name">{{ planDayLabel(plan, d) }}</span>
        <span class="day-sum">
          <template v-if="daySummary(d)?.slots">
            ≈{{ r0(daySummary(d)!.calories) }}<template
              v-if="plan.goal_calories > 0 && !plan.participants.length"
            >
              / {{ r0(plan.goal_calories) }}</template
            >
            ккал
            <span v-if="daySummary(d)!.approx" class="approx-note">
              · посчитано {{ daySummary(d)!.counted }} из
              {{ daySummary(d)!.counted + daySummary(d)!.approx }}
            </span>
          </template>
          <template v-else>пусто</template>
        </span>
        <span class="chev">{{ openDays.has(d) ? '⌃' : '⌄' }}</span>
      </button>

      <div v-if="plan.participants.length && daySummary(d)?.slots" class="chips">
        <span v-for="pt in daySummary(d)!.by_participant" :key="pt.participant_id" class="chip">
          {{ participantName(pt.participant_id) }} {{ r0(pt.calories) }}
          <template v-if="plan.participants.find((x) => x.id === pt.participant_id)?.calories_target">
            / {{ r0(plan.participants.find((x) => x.id === pt.participant_id)!.calories_target) }}
          </template>
        </span>
      </div>

      <div v-if="openDays.has(d)" class="day-body">
        <div v-for="g in daySlots(d)" :key="g.type" class="group">
          <p class="group-title">{{ MEAL_TYPE_LABELS[g.type] }}</p>
          <button v-for="s in g.slots" :key="s.id" class="slot" @click="openSlot(d, s)">
            <span class="slot-title">
              <span v-if="s.time" class="slot-time">{{ s.time }}</span>
              {{ s.title || 'Без названия' }}
              <span v-if="s.participant_id" class="slot-part">{{ participantLabel(s.participant_id) }}</span>
            </span>
            <span v-if="s.items.length" class="slot-items">
              <span v-for="(it, i) in s.items" :key="i" :class="{ dim: it.approx }">
                {{ it.approx ? '≈ ' : '' }}{{ it.name
                }}<template v-if="!it.approx"> {{ r0(it.calories) }}</template
                ><template v-if="i < s.items.length - 1">, </template>
              </span>
            </span>
            <span v-if="s.note" class="slot-note">{{ s.note }}</span>
          </button>
        </div>

        <p v-if="daySlots(d).length === 0" class="empty">На этот день ничего не запланировано.</p>

        <div class="day-actions">
          <button v-if="plan.can_edit" class="act" @click="openSlot(d, null)">＋ Приём</button>
          <button class="act" @click="applyDay = d">📋 В дневник</button>
          <button v-if="plan.can_edit" class="act" @click="startCopy(d)">⧉ Копировать</button>
        </div>

        <div v-if="copyFrom === d" class="copy-box">
          <span>Скопировать в</span>
          <select v-model.number="copyTo">
            <option v-for="n in plan.days" :key="n" :value="n - 1" :disabled="n - 1 === d">
              День {{ n }}
            </option>
          </select>
          <button class="act primary" :disabled="copyBusy" @click="doCopy">ОК</button>
          <button class="act" @click="copyFrom = null">✕</button>
        </div>
      </div>
    </div>

    <PlanSlotModal
      v-if="slotModal"
      :plan="plan"
      :slot="slotModal.slot"
      :day-index="slotModal.dayIndex"
      @saved="onSlotSaved"
      @close="slotModal = null"
    />
    <PlanApplyModal
      v-if="applyDay !== null"
      :plan="plan"
      :day-index="applyDay"
      @applied="applyDay = null"
      @close="applyDay = null"
    />
    <PlanParticipantsModal
      v-if="partsOpen"
      :plan="plan"
      @changed="load()"
      @close="partsOpen = false"
    />
    <PlanShareModal v-if="shareOpen" :plan="plan" @close="shareOpen = false" />
    <PlanSettingsModal
      v-if="settingsOpen"
      :plan="plan"
      @saved="onPlanChanged(false)"
      @deleted="onPlanChanged(true)"
      @close="settingsOpen = false"
    />
  </template>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-bottom: 8px;
}

.back {
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 18px;
  padding: 2px 12px;
  flex: none;
}

.head-title {
  flex: 1;
  min-width: 0;
}

.head-title b {
  display: block;
  font-size: 15px;
  overflow-wrap: anywhere;
}

.head-sub {
  font-size: 11px;
  color: var(--text-secondary);
}

.icon {
  background: none;
  border: none;
  font-size: 17px;
  padding: 4px 3px;
  flex: none;
}

.desc {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 10px;
  white-space: pre-wrap;
}

.weeks {
  display: flex;
  gap: 4px;
  overflow-x: auto;
  scrollbar-width: none;
  margin-bottom: 10px;
}

.weeks::-webkit-scrollbar {
  display: none;
}

.wtab {
  flex: none;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  padding: 6px 12px;
  font-size: 12px;
  color: var(--text-color);
  white-space: nowrap;
}

.wtab.on {
  background: var(--accent-color);
  color: #fff;
}

.day {
  background: var(--card-color);
  border-radius: 8px;
  padding: 8px 10px;
  margin-bottom: 8px;
}

.day-head {
  display: flex;
  align-items: baseline;
  gap: 8px;
  width: 100%;
  background: none;
  border: none;
  color: var(--text-color);
  text-align: left;
  padding: 2px 0;
}

.day-name {
  flex: none;
  font-size: 13px;
  font-weight: 600;
}

.day-sum {
  flex: 1;
  min-width: 0;
  font-size: 11px;
  color: var(--text-secondary);
  text-align: right;
}

.approx-note {
  white-space: nowrap;
}

.chev {
  flex: none;
  color: var(--text-secondary);
  font-size: 12px;
}

.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 4px;
}

.chip {
  font-size: 10px;
  background: var(--bg-secondary);
  border-radius: 6px;
  padding: 2px 6px;
  color: var(--text-secondary);
}

.day-body {
  border-top: 1px solid var(--bg-secondary);
  margin-top: 8px;
  padding-top: 6px;
}

.group-title {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 6px 0 2px;
}

.slot {
  display: block;
  width: 100%;
  text-align: left;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  padding: 7px 9px;
  margin-bottom: 5px;
  color: var(--text-color);
}

.slot-title {
  display: block;
  font-size: 13px;
  font-weight: 600;
  overflow-wrap: anywhere;
}

.slot-time {
  font-weight: 400;
  font-size: 11px;
  color: var(--text-secondary);
  margin-right: 4px;
}

.slot-part {
  font-weight: 400;
  font-size: 10px;
  color: var(--text-secondary);
  margin-left: 4px;
}

.slot-items {
  display: block;
  font-size: 11px;
  color: var(--text-color);
  margin-top: 2px;
  overflow-wrap: anywhere;
}

.slot-items .dim {
  color: var(--text-secondary);
}

.slot-note {
  display: block;
  font-size: 11px;
  color: var(--text-secondary);
  margin-top: 2px;
  white-space: pre-wrap;
}

.empty {
  font-size: 12px;
  color: var(--text-secondary);
  text-align: center;
  padding: 8px 0;
}

.day-actions {
  display: flex;
  gap: 6px;
  margin-top: 6px;
}

.act {
  flex: 1;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  padding: 7px 4px;
  font-size: 12px;
  color: var(--text-color);
}

.act.primary {
  background: var(--accent-color);
  color: #fff;
  flex: none;
  padding: 7px 12px;
}

.copy-box {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 6px;
  font-size: 12px;
  color: var(--text-secondary);
}

.copy-box select {
  flex: 1;
  min-width: 0;
  padding: 6px;
}

.copy-box .act {
  flex: none;
}

.hint {
  text-align: center;
  color: var(--text-secondary);
  padding: 20px 0;
}

.retry {
  background: none;
  border: none;
  color: var(--accent-color);
  text-decoration: underline;
}
</style>
