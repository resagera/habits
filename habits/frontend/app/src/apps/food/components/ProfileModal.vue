<script setup lang="ts">
import { ref } from 'vue'
import { showToast } from '../../../shared/toast'
import * as foodApi from '../api'
import {
  ACTIVITY_LABELS,
  GOAL_LABELS,
  PROTEIN_BASE_LABELS,
  PROTEIN_COEF_PRESETS,
  r0,
  todayStr,
  type FoodProfile,
  type FoodTargets,
} from '../types'

const props = defineProps<{
  profile: FoodProfile | null // null — первичная настройка
}>()
const emit = defineEmits<{ saved: []; close: [] }>()

const form = ref<FoodProfile>(
  props.profile
    ? { ...props.profile }
    : {
        sex: '',
        birth_date: '',
        height_cm: 0,
        weight_kg: 0,
        target_weight_kg: 0,
        body_fat_percent: 0,
        activity_level: 'medium',
        goal_type: 'maintain',
        rate_kcal: 400,
        protein_base: 'current',
        protein_base_kg: 0,
        protein_coef: 1.2,
      },
)

const targets = ref<FoodTargets | null>(null)
const manual = ref({ calories: 0, protein: 0, fat: 0, carbs: 0 })
const useManual = ref(false)
const calcing = ref(false)
const saving = ref(false)

async function calc() {
  calcing.value = true
  try {
    targets.value = (await foodApi.calculateTargets(form.value)).targets
    manual.value = {
      calories: targets.value.calories,
      protein: targets.value.protein,
      fat: targets.value.fat,
      carbs: targets.value.carbs,
    }
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Заполните пол, дату рождения, рост и вес')
  } finally {
    calcing.value = false
  }
}

async function save() {
  saving.value = true
  try {
    await foodApi.saveProfile(form.value)
    // цель: авто-расчёт или ручные значения
    const t = targets.value
    const src = useManual.value || !t ? 'manual' : 'auto'
    const vals = useManual.value || !t ? manual.value : t
    if (vals.calories > 0 || vals.protein > 0) {
      await foodApi.createGoal({
        date_from: todayStr(),
        goal_type: form.value.goal_type,
        calories: vals.calories,
        protein: vals.protein,
        fat: vals.fat,
        carbs: vals.carbs,
        source: src,
        details: src === 'auto' && t ? t.details : 'Задано вручную',
      })
    }
    emit('saved')
    showToast('Профиль сохранён ✅')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось сохранить')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content prof">
      <h3>{{ profile ? '⚙️ Профиль питания' : '👋 Настройка питания' }}</h3>
      <p v-if="!profile" class="intro">
        Пара вопросов — и посчитаем дневные цели по калориям и БЖУ.
      </p>

      <label class="fld">
        <span>Пол (для расчёта)</span>
        <select v-model="form.sex">
          <option value="">— не указан —</option>
          <option value="male">Мужской</option>
          <option value="female">Женский</option>
        </select>
      </label>
      <div class="row2">
        <label><span>Дата рождения</span><input v-model="form.birth_date" type="date" /></label>
        <label><span>Рост, см</span><input v-model.number="form.height_cm" type="number" min="0" max="300" /></label>
      </div>
      <div class="row2">
        <label><span>Текущий вес, кг</span><input v-model.number="form.weight_kg" type="number" min="0" max="500" step="0.1" /></label>
        <label><span>Целевой вес, кг</span><input v-model.number="form.target_weight_kg" type="number" min="0" max="500" step="0.1" /></label>
      </div>
      <label class="fld">
        <span>Процент жира, % (необязательно)</span>
        <input v-model.number="form.body_fat_percent" type="number" min="0" max="70" step="0.1" />
      </label>
      <label class="fld">
        <span>Уровень активности</span>
        <select v-model="form.activity_level">
          <option v-for="(label, k) in ACTIVITY_LABELS" :key="k" :value="k">{{ label }}</option>
        </select>
      </label>
      <label class="fld">
        <span>Цель</span>
        <select v-model="form.goal_type">
          <option v-for="(label, k) in GOAL_LABELS" :key="k" :value="k">{{ label }}</option>
        </select>
      </label>
      <label v-if="form.goal_type !== 'maintain'" class="fld">
        <span>{{ form.goal_type === 'lose' ? 'Дефицит' : 'Профицит' }}, ккал/день</span>
        <input v-model.number="form.rate_kcal" type="number" min="0" max="1500" step="50" />
      </label>

      <h4>Белок</h4>
      <label class="fld">
        <span>База расчёта</span>
        <select v-model="form.protein_base">
          <option v-for="(label, k) in PROTEIN_BASE_LABELS" :key="k" :value="k">{{ label }}</option>
        </select>
      </label>
      <label v-if="form.protein_base === 'manual'" class="fld">
        <span>Вес для расчёта, кг</span>
        <input v-model.number="form.protein_base_kg" type="number" min="0" max="500" step="0.1" />
      </label>
      <label class="fld">
        <span>Коэффициент, г/кг</span>
        <input v-model.number="form.protein_coef" type="number" min="0.5" max="4" step="0.1" list="protein-coefs" />
        <datalist id="protein-coefs">
          <option v-for="p in PROTEIN_COEF_PRESETS" :key="p.v" :value="p.v">{{ p.label }}</option>
        </datalist>
      </label>
      <p class="coefs-hint">
        <span v-for="p in PROTEIN_COEF_PRESETS" :key="p.v" class="coef-chip" @click="form.protein_coef = p.v">
          {{ p.label }}
        </span>
      </p>

      <button class="btn" :disabled="calcing" @click="calc">
        {{ calcing ? '…' : '🧮 Рассчитать цели' }}
      </button>

      <template v-if="targets">
        <div class="targets">
          <b>{{ r0(targets.calories) }} ккал</b> · Б {{ r0(targets.protein) }} · Ж {{ r0(targets.fat) }} · У {{ r0(targets.carbs) }}
          <p class="details">{{ targets.details }}</p>
        </div>
        <label class="manual-toggle">
          <input v-model="useManual" type="checkbox" />
          Задать цели вручную
        </label>
      </template>

      <div v-if="useManual || !targets" class="grid4">
        <label><span>Ккал</span><input v-model.number="manual.calories" type="number" min="0" /></label>
        <label><span>Белки</span><input v-model.number="manual.protein" type="number" min="0" /></label>
        <label><span>Жиры</span><input v-model.number="manual.fat" type="number" min="0" /></label>
        <label><span>Углев.</span><input v-model.number="manual.carbs" type="number" min="0" /></label>
      </div>

      <p class="disclaimer">
        Расчёты ориентировочные и не заменяют медицинские рекомендации.
      </p>

      <button class="btn primary" :disabled="saving" @click="save">
        {{ saving ? 'Сохраняем…' : '💾 Сохранить профиль и цели' }}
      </button>
      <button class="btn" @click="emit('close')">{{ profile ? 'Отмена' : 'Позже' }}</button>
    </div>
  </div>
</template>

<style scoped>
.prof {
  text-align: left;
  max-height: 88vh;
  overflow-y: auto;
}

.prof h3 {
  text-align: center;
}

.prof h4 {
  margin: 12px 0 2px;
  font-size: 14px;
}

.intro {
  font-size: 13px;
  color: var(--text-secondary);
  text-align: center;
  margin: 4px 0 8px;
}

.fld {
  display: block;
}

.fld span,
.row2 span {
  display: block;
  font-size: 11px;
  color: var(--text-secondary);
  margin-top: 8px;
}

.prof input,
.prof select {
  width: 100%;
  margin-top: 2px;
}

.row2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

.coefs-hint {
  margin: 6px 0 0;
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.coef-chip {
  font-size: 10px;
  color: var(--text-secondary);
  background: var(--bg-secondary);
  border-radius: 10px;
  padding: 2px 8px;
  cursor: pointer;
}

.targets {
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 10px 12px;
  margin-top: 10px;
  font-size: 15px;
}

.details {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 6px 0 0;
}

.manual-toggle {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  margin-top: 8px;
}

.manual-toggle input {
  width: auto;
}

.grid4 {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 6px;
  margin-top: 6px;
}

.grid4 span {
  display: block;
  font-size: 10px;
  color: var(--text-secondary);
}

.grid4 input {
  margin-top: 2px;
  padding: 6px;
}

.disclaimer {
  font-size: 11px;
  color: var(--text-secondary);
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
