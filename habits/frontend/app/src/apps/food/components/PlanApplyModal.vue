<script setup lang="ts">
import { ref } from 'vue'
import { showToast } from '../../../shared/toast'
import * as foodApi from '../api'
import { planDayDate, planDayLabel, todayStr, type FoodPlan } from '../types'

// Перенос дней плана в дневник. Записи создаются ТОЛЬКО в своём дневнике:
// участник открывает общий план у себя и применяет его сам.
const props = defineProps<{ plan: FoodPlan; dayIndex: number }>()
const emit = defineEmits<{ applied: []; close: [] }>()

const date = ref(planDayDate(props.plan, props.dayIndex) || todayStr())
const days = ref(1)
// если один из участников — это вы, по умолчанию переносим ваши порции
const participantId = ref(props.plan.participants.find((p) => p.is_me)?.id ?? 0)
const busy = ref(false)
// сколько записей из этого плана уже есть на выбранных датах (0 — ещё нет)
const existing = ref(0)

const maxDays = Math.min(props.plan.days - props.dayIndex, 14)

async function run(mode: '' | 'add' | 'replace') {
  busy.value = true
  try {
    const { result } = await foodApi.applyPlan(props.plan.id, {
      day_index: props.dayIndex,
      date: date.value,
      days: days.value,
      participant_id: participantId.value || null,
      mode,
    })
    if (!mode && result.existing > 0) {
      existing.value = result.existing
      return
    }
    const skipped = result.skipped
      ? `, примерных позиций не перенесено: ${result.skipped}`
      : ''
    showToast(`Добавлено записей: ${result.created}${skipped}`)
    emit('applied')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось применить')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content apply">
      <h3>📋 В дневник</h3>
      <p class="sub">{{ planDayLabel(plan, dayIndex) }} → ваш дневник</p>

      <label class="fld">
        <span>Начиная с даты</span>
        <input v-model="date" type="date" @change="existing = 0" />
      </label>

      <label class="fld">
        <span>Сколько дней плана перенести</span>
        <select v-model.number="days" @change="existing = 0">
          <option v-for="n in maxDays" :key="n" :value="n">
            {{ n }} {{ n === 1 ? 'день' : n < 5 ? 'дня' : 'дней' }}
          </option>
        </select>
      </label>

      <label v-if="plan.participants.length" class="fld">
        <span>Чьи порции переносить</span>
        <select v-model.number="participantId" @change="existing = 0">
          <option :value="0">Только общие приёмы</option>
          <option v-for="p in plan.participants" :key="p.id" :value="p.id">
            {{ p.emoji }} {{ p.name }}{{ p.is_me ? ' — это вы' : '' }} (×{{ p.portion_coef }})
          </option>
        </select>
      </label>

      <p class="hint">
        Примерные позиции КБЖУ не имеют — они попадут в описание записи, чтобы не потерялись.
      </p>

      <template v-if="existing > 0">
        <p class="warn">
          На эти даты план уже переносили — там {{ existing }}
          {{ existing === 1 ? 'запись' : 'записей' }} из этого плана.
        </p>
        <button class="btn primary" :disabled="busy" @click="run('replace')">
          🔁 Заменить прежние
        </button>
        <button class="btn" :disabled="busy" @click="run('add')">➕ Добавить поверх</button>
      </template>
      <button v-else class="btn primary" :disabled="busy" @click="run('')">
        {{ busy ? '…' : '📋 Перенести в дневник' }}
      </button>

      <button class="btn" @click="emit('close')">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.apply {
  text-align: left;
}

.apply h3 {
  text-align: center;
  margin-bottom: 2px;
}

.sub {
  text-align: center;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 10px;
}

.fld {
  display: block;
  margin-top: 8px;
}

.fld span {
  display: block;
  font-size: 11px;
  color: var(--text-secondary);
  margin-bottom: 2px;
}

.fld input,
.fld select {
  width: 100%;
}

.hint {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 8px 0 0;
}

.warn {
  font-size: 12px;
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 8px 10px;
  margin: 10px 0 0;
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

.btn:disabled {
  opacity: 0.5;
}
</style>
