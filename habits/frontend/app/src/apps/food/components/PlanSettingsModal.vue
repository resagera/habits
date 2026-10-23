<script setup lang="ts">
import { ref } from 'vue'
import { showToast } from '../../../shared/toast'
import * as foodApi from '../api'
import type { FoodPlan } from '../types'

// Настройки плана: только владелец. Укорочение плана не удаляет приёмы за
// новой границей — вернув длину, вы получите их обратно.
const props = defineProps<{ plan: FoodPlan }>()
const emit = defineEmits<{ saved: []; deleted: []; close: [] }>()

const name = ref(props.plan.name)
const description = ref(props.plan.description)
const days = ref(props.plan.days)
const startDate = ref(props.plan.start_date)
const archived = ref(props.plan.archived)
const busy = ref(false)
const confirmDel = ref(false)

async function save() {
  if (!name.value.trim()) {
    showToast('Укажите название плана')
    return
  }
  busy.value = true
  try {
    await foodApi.updatePlan(props.plan.id, {
      name: name.value,
      description: description.value,
      days: days.value,
      start_date: startDate.value,
      archived: archived.value,
    })
    emit('saved')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось сохранить')
  } finally {
    busy.value = false
  }
}

async function duplicate() {
  busy.value = true
  try {
    await foodApi.duplicatePlan(props.plan.id, `${props.plan.name} (копия)`)
    showToast('Копия создана')
    emit('saved')
  } catch {
    showToast('Не удалось скопировать')
  } finally {
    busy.value = false
  }
}

async function remove() {
  if (!confirmDel.value) {
    confirmDel.value = true
    setTimeout(() => (confirmDel.value = false), 3000)
    return
  }
  try {
    await foodApi.deletePlan(props.plan.id)
    showToast('План удалён')
    emit('deleted')
  } catch {
    showToast('Не удалось удалить')
  }
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content settings">
      <h3>⚙️ Настройки плана</h3>

      <label class="fld">
        <span>Название</span>
        <input v-model="name" maxlength="200" />
      </label>

      <label class="fld">
        <span>Описание</span>
        <textarea v-model="description" rows="2" maxlength="2000" />
      </label>

      <div class="row2">
        <label>
          <span>Дней в плане</span>
          <input v-model.number="days" type="number" min="1" max="90" />
        </label>
        <label>
          <span>Дата начала</span>
          <input v-model="startDate" type="date" />
        </label>
      </div>
      <p class="hint">
        Без даты начала план «шаблонный» — его можно применить к любой дате. Уменьшение
        длительности приёмы не удаляет: вернёте дни — вернутся и они.
      </p>

      <label class="check">
        <input v-model="archived" type="checkbox" />
        В архиве
      </label>

      <button class="btn primary" :disabled="busy" @click="save">
        {{ busy ? '…' : '💾 Сохранить' }}
      </button>
      <button class="btn" :disabled="busy" @click="duplicate">⧉ Дублировать план</button>
      <button class="btn danger" @click="remove">
        {{ confirmDel ? 'Точно удалить план?' : '🗑 Удалить план' }}
      </button>
      <button class="btn" @click="emit('close')">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.settings {
  text-align: left;
  max-height: 88vh;
  overflow-y: auto;
}

.settings h3 {
  text-align: center;
}

.fld {
  display: block;
  margin-top: 8px;
}

.row2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
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
.fld textarea,
.row2 input {
  width: 100%;
}

.hint {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 6px 0 0;
}

.check {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  margin-top: 10px;
}

.check input {
  width: auto;
  margin: 0;
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
