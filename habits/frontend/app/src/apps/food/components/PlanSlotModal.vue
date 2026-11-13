<script setup lang="ts">
import { ref } from 'vue'
import { showToast } from '../../../shared/toast'
import * as foodApi from '../api'
import PlanItemsEditor from './PlanItemsEditor.vue'
import {
  MEAL_TYPE_LABELS,
  MEAL_TYPES,
  planDayLabel,
  type FoodPlan,
  type FoodPlanItem,
  type FoodPlanSlot,
  type MealType,
} from '../types'

// Приём пищи в плане: тип, время, название, заметка, для кого и состав.
const props = defineProps<{
  plan: FoodPlan
  slot: FoodPlanSlot | null
  dayIndex: number
}>()
const emit = defineEmits<{ saved: []; close: [] }>()

const mealType = ref<MealType>(props.slot?.meal_type ?? 'breakfast')
const time = ref(props.slot?.time ?? '')
const title = ref(props.slot?.title ?? '')
const note = ref(props.slot?.note ?? '')
const participantId = ref<number>(props.slot?.participant_id ?? 0)
const items = ref<FoodPlanItem[]>(props.slot ? props.slot.items.map((i) => ({ ...i })) : [])
const saving = ref(false)
const confirmDelete = ref(false)

async function save() {
  if (items.value.some((i) => !i.name.trim())) {
    showToast('У каждой позиции должно быть название')
    return
  }
  saving.value = true
  const payload: foodApi.SlotPayload = {
    day_index: props.dayIndex,
    meal_type: mealType.value,
    time: time.value,
    title: title.value,
    note: note.value,
    participant_id: participantId.value || null,
    items: items.value,
  }
  try {
    if (props.slot) await foodApi.updateSlot(props.plan.id, props.slot.id, payload)
    else await foodApi.createSlot(props.plan.id, payload)
    emit('saved')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось сохранить')
  } finally {
    saving.value = false
  }
}

async function remove() {
  if (!props.slot) return
  if (!confirmDelete.value) {
    confirmDelete.value = true
    setTimeout(() => (confirmDelete.value = false), 3000)
    return
  }
  try {
    await foodApi.deleteSlot(props.plan.id, props.slot.id)
    emit('saved')
  } catch {
    showToast('Не удалось удалить')
  }
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content slot-modal">
      <h3>{{ slot ? '✏️ Приём пищи' : '＋ Приём пищи' }}</h3>
      <p class="day-lbl">{{ planDayLabel(plan, dayIndex) }}</p>

      <div class="row2">
        <label>
          <span>Приём</span>
          <select v-model="mealType">
            <option v-for="t in MEAL_TYPES" :key="t" :value="t">{{ MEAL_TYPE_LABELS[t] }}</option>
          </select>
        </label>
        <label>
          <span>Время (необязательно)</span>
          <input v-model="time" placeholder="ЧЧ:ММ" maxlength="5" />
        </label>
      </div>

      <label class="fld">
        <span>Название</span>
        <input v-model="title" maxlength="200" placeholder="Например: каша с ягодами" />
      </label>

      <label v-if="plan.participants.length" class="fld">
        <span>Для кого</span>
        <select v-model.number="participantId">
          <option :value="0">👥 Общий — на всех</option>
          <option v-for="p in plan.participants" :key="p.id" :value="p.id">
            {{ p.emoji }} {{ p.name }}
          </option>
        </select>
      </label>
      <p v-if="plan.participants.length && !participantId" class="hint">
        Общий приём войдёт в план каждого участника с его коэффициентом порции.
      </p>

      <label class="fld">
        <span>Заметка</span>
        <textarea v-model="note" rows="2" maxlength="2000" placeholder="Необязательно" />
      </label>

      <p class="sec-title">Состав</p>
      <p class="hint">
        Кнопка «≈» делает позицию примерной: понятно, что будет, но КБЖУ не считается.
      </p>
      <PlanItemsEditor v-model="items" />

      <button class="btn primary" :disabled="saving" @click="save">
        {{ saving ? '…' : '💾 Сохранить' }}
      </button>
      <button v-if="slot" class="btn danger" @click="remove">
        {{ confirmDelete ? 'Точно удалить?' : '🗑 Удалить приём' }}
      </button>
      <button class="btn" @click="emit('close')">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.slot-modal {
  text-align: left;
  max-height: 88vh;
  overflow-y: auto;
}

.slot-modal h3 {
  text-align: center;
  margin-bottom: 2px;
}

.day-lbl {
  text-align: center;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 10px;
}

.row2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

.fld,
.row2 label {
  display: block;
  margin-top: 8px;
}

.fld span,
.row2 span {
  display: block;
  font-size: 11px;
  color: var(--text-secondary);
  margin-bottom: 2px;
}

.fld input,
.fld select,
.fld textarea,
.row2 input,
.row2 select {
  width: 100%;
}

.sec-title {
  font-size: 14px;
  font-weight: 600;
  margin: 14px 0 2px;
}

.hint {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 2px 0 6px;
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

.btn:disabled {
  opacity: 0.5;
}
</style>
