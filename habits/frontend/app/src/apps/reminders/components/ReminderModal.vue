<script setup lang="ts">
import { computed, ref } from 'vue'
import { showToast } from '../../../shared/toast'
import type { Category } from '../../tracker/types'
import {
  INTERVAL_UNITS,
  KIND_LABELS,
  MONTHS,
  WEEKDAYS,
  currentTzOffset,
  splitInterval,
  type Reminder,
  type ReminderCategory,
  type ReminderDraft,
  type ReminderKind,
} from '../types'

const props = defineProps<{
  reminder: Reminder | null // null = создание
  categories: Category[] // категории Tracker для kind=tracker
  groups: ReminderCategory[] // свои категории напоминаний
  defaultGroupId?: number | null
}>()

const emit = defineEmits<{
  save: [draft: ReminderDraft]
  remove: []
  close: []
}>()

const kinds = Object.keys(KIND_LABELS) as ReminderKind[]

const title = ref(props.reminder?.title ?? '')
const note = ref(props.reminder?.note ?? '')
const kind = ref<ReminderKind>(props.reminder?.kind ?? 'daily')
const timeOfDay = ref(props.reminder?.time_of_day ?? '09:00')
const daysMask = ref(props.reminder?.days_mask ?? 127)
const dayOfMonth = ref(props.reminder?.day_of_month ?? 1)
const month = ref(props.reminder?.month ?? new Date().getMonth() + 1)
const initialInterval = splitInterval(props.reminder?.interval_minutes ?? 180)
const intervalValue = ref(initialInterval.value)
const intervalUnit = ref(initialInterval.unit)
const categoryId = ref<number | null>(props.reminder?.category_id ?? null)
const groupId = ref<number | null>(props.reminder?.group_id ?? props.defaultGroupId ?? null)
const confirmDelete = ref(false)

// datetime-local без секунд, по умолчанию — через час
function toLocalInput(d: Date): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}
const atLocal = ref(
  props.reminder?.at ? toLocalInput(new Date(props.reminder.at)) : toLocalInput(new Date(Date.now() + 3600_000)),
)

// daily-категории показываем первыми — они основные кандидаты
const sortedCategories = computed(() =>
  [...props.categories].sort((a, b) => Number(b.daily) - Number(a.daily) || a.position - b.position),
)

function toggleDay(i: number) {
  daysMask.value ^= 1 << i
}

function save() {
  if (!title.value.trim() && kind.value !== 'tracker') {
    showToast('Введите название')
    return
  }
  const draft: ReminderDraft = {
    title: title.value.trim() || 'Напоминание',
    note: note.value.trim(),
    kind: kind.value,
    days_mask: 127,
    group_id: groupId.value ?? undefined,
    tz_offset_minutes: currentTzOffset(),
    enabled: props.reminder?.enabled ?? true,
  }
  switch (kind.value) {
    case 'once': {
      const at = new Date(atLocal.value)
      if (isNaN(at.getTime())) {
        showToast('Укажите дату и время')
        return
      }
      if (at.getTime() <= Date.now()) {
        showToast('Время уже прошло')
        return
      }
      draft.at = at.toISOString()
      break
    }
    case 'daily':
      draft.time_of_day = timeOfDay.value
      break
    case 'weekly':
      if (daysMask.value === 0) {
        showToast('Выберите хотя бы один день недели')
        return
      }
      draft.time_of_day = timeOfDay.value
      draft.days_mask = daysMask.value
      break
    case 'monthly':
      draft.time_of_day = timeOfDay.value
      draft.day_of_month = Math.min(31, Math.max(1, Math.round(dayOfMonth.value) || 1))
      break
    case 'yearly':
      draft.time_of_day = timeOfDay.value
      draft.day_of_month = Math.min(31, Math.max(1, Math.round(dayOfMonth.value) || 1))
      draft.month = Math.min(12, Math.max(1, Math.round(month.value) || 1))
      break
    case 'interval': {
      const unit = INTERVAL_UNITS.find((u) => u.key === intervalUnit.value) ?? INTERVAL_UNITS[0]
      const total = (Math.round(intervalValue.value) || 0) * unit.minutes
      if (total < 5) {
        showToast('Интервал — минимум 5 минут')
        return
      }
      if (total > 525600) {
        showToast('Интервал — максимум год')
        return
      }
      draft.interval_minutes = total
      break
    }
    case 'tracker': {
      if (categoryId.value === null) {
        showToast('Выберите категорию Tracker')
        return
      }
      const cat = props.categories.find((c) => c.id === categoryId.value)
      draft.title = title.value.trim() || `Отметить «${cat?.name ?? '?'}»`
      draft.time_of_day = timeOfDay.value
      draft.days_mask = daysMask.value || 127
      draft.category_id = categoryId.value
      break
    }
  }
  emit('save', draft)
}

function onRemove() {
  if (!confirmDelete.value) {
    confirmDelete.value = true
    setTimeout(() => (confirmDelete.value = false), 3500)
    return
  }
  emit('remove')
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content rem-modal">
      <h3>{{ reminder ? 'Настройки напоминания' : 'Новое напоминание' }}</h3>

      <label class="field-label">Тип</label>
      <select v-model="kind">
        <option v-for="k in kinds" :key="k" :value="k">{{ KIND_LABELS[k] }}</option>
      </select>

      <template v-if="kind === 'tracker'">
        <label class="field-label">Категория Tracker</label>
        <select v-model="categoryId">
          <option :value="null" disabled>— выберите —</option>
          <option v-for="c in sortedCategories" :key="c.id" :value="c.id">
            {{ c.name }}{{ c.daily ? ' · daily' : '' }}
          </option>
        </select>
        <p class="hint-line">
          Сообщение придёт в указанное время, только если день в этой категории ещё не отмечен.
        </p>
      </template>

      <input v-model="title" :placeholder="kind === 'tracker' ? 'Название (необязательно)' : 'Название'" maxlength="200" />
      <input v-model="note" placeholder="Текст сообщения (необязательно)" maxlength="1000" />

      <template v-if="groups.length > 0">
        <label class="field-label">Категория</label>
        <select v-model="groupId">
          <option :value="null">📂 Без категории</option>
          <option v-for="g in groups" :key="g.id" :value="g.id">{{ g.name }}</option>
        </select>
      </template>

      <template v-if="kind === 'once'">
        <label class="field-label">Дата и время</label>
        <input v-model="atLocal" type="datetime-local" />
      </template>

      <template v-if="kind === 'daily' || kind === 'weekly' || kind === 'monthly' || kind === 'yearly' || kind === 'tracker'">
        <label class="field-label">Время</label>
        <input v-model="timeOfDay" type="time" />
      </template>

      <template v-if="kind === 'yearly'">
        <label class="field-label">Дата (каждый год) — для праздников и дней рождений</label>
        <div class="interval-row">
          <input v-model.number="dayOfMonth" type="number" min="1" max="31" />
          <select v-model.number="month" class="month-select">
            <option v-for="(m, i) in MONTHS" :key="m" :value="i + 1">{{ m }}</option>
          </select>
        </div>
      </template>

      <template v-if="kind === 'weekly' || kind === 'tracker'">
        <label class="field-label">Дни недели</label>
        <div class="days">
          <button
            v-for="(d, i) in WEEKDAYS"
            :key="d"
            type="button"
            class="day"
            :class="{ on: daysMask & (1 << i) }"
            @click="toggleDay(i)"
          >
            {{ d }}
          </button>
        </div>
      </template>

      <template v-if="kind === 'monthly'">
        <label class="field-label">Число месяца (если в месяце нет — последний день)</label>
        <input v-model.number="dayOfMonth" type="number" min="1" max="31" />
      </template>

      <template v-if="kind === 'interval'">
        <label class="field-label">Повторять каждые</label>
        <div class="interval-row">
          <input v-model.number="intervalValue" type="number" min="1" max="9999" />
          <select v-model="intervalUnit" class="month-select">
            <option v-for="u in INTERVAL_UNITS" :key="u.key" :value="u.key">{{ u.label }}</option>
          </select>
        </div>
      </template>

      <button class="btn primary" @click="save">💾 Сохранить</button>
      <button v-if="reminder" class="btn danger" @click="onRemove">
        {{ confirmDelete ? 'Точно удалить?' : '🗑 Удалить' }}
      </button>
      <button class="btn" @click="emit('close')">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.rem-modal {
  text-align: left;
  max-height: 85vh;
  overflow-y: auto;
}

.rem-modal h3 {
  text-align: center;
}

.rem-modal input,
.rem-modal select {
  width: 100%;
  margin-top: 8px;
}

.field-label {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 12px;
}

.hint-line {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 6px 0 0;
}

.days {
  display: flex;
  gap: 4px;
  margin-top: 8px;
}

.day {
  flex: 1;
  padding: 7px 0;
  border: none;
  border-radius: 6px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: 12px;
}

.day.on {
  background: var(--accent-color);
  color: #fff;
}

.interval-row {
  display: flex;
  align-items: center;
  gap: 6px;
}

.interval-row input {
  width: 72px !important;
  margin-top: 0;
}

.interval-row .month-select {
  flex: 1;
  width: auto;
  margin-top: 0;
}

.interval-row span {
  font-size: 13px;
  color: var(--text-secondary);
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
