<script setup lang="ts">
import { ref } from 'vue'
import { showToast } from '../../../shared/toast'
import * as foodApi from '../api'
import FoodItemsEditor from './FoodItemsEditor.vue'
import {
  assetUrl,
  MEAL_TYPE_LABELS,
  MEAL_TYPES,
  type FoodItem,
  type FoodMeal,
  type MealType,
} from '../types'

const props = defineProps<{
  meal: FoodMeal | null // null — создание
  day: string
}>()
const emit = defineEmits<{ saved: [meal: FoodMeal]; close: [] }>()

const form = ref({
  day: props.meal?.day ?? props.day,
  time: props.meal?.time ?? '',
  meal_type: (props.meal?.meal_type ?? 'none') as MealType,
  name: props.meal?.name ?? '',
  description: props.meal?.description ?? '',
  photo: props.meal?.photo ?? '',
  calories: props.meal?.calories ?? 0,
  protein: props.meal?.protein ?? 0,
  fat: props.meal?.fat ?? 0,
  carbs: props.meal?.carbs ?? 0,
})
const items = ref<FoodItem[]>(props.meal ? props.meal.items.map((i) => ({ ...i })) : [])

const saving = ref(false)
const uploading = ref(false)

async function onPhoto(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  uploading.value = true
  try {
    const { url } = await foodApi.uploadPhoto(file)
    form.value.photo = url
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось загрузить фото')
  } finally {
    uploading.value = false
  }
}

async function save() {
  if (items.value.length === 0 && !form.value.name.trim()) {
    showToast('Добавьте продукты или укажите название и КБЖУ вручную')
    return
  }
  saving.value = true
  try {
    const payload = { ...form.value, items: items.value }
    const { meal } = props.meal
      ? await foodApi.updateMeal(props.meal.id, payload)
      : await foodApi.createMeal(payload)
    emit('saved', meal)
    showToast('Сохранено ✅')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось сохранить')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content meal-form">
      <h3>{{ meal ? '✏️ Приём пищи' : '🍽 Новый приём пищи' }}</h3>

      <div class="row2">
        <label><span>Дата</span><input v-model="form.day" type="date" /></label>
        <label><span>Время</span><input v-model="form.time" type="time" /></label>
      </div>
      <label class="fld">
        <span>Категория</span>
        <select v-model="form.meal_type">
          <option v-for="t in MEAL_TYPES" :key="t" :value="t">{{ MEAL_TYPE_LABELS[t] }}</option>
        </select>
      </label>
      <input v-model="form.name" placeholder="Название (например, Пельмени с майонезом)" maxlength="200" />
      <textarea v-model="form.description" placeholder="Краткое описание…" maxlength="2000" rows="2"></textarea>

      <div class="photo-row">
        <img v-if="form.photo" :src="assetUrl(form.photo)" class="photo-preview" alt="" />
        <label class="photo-btn">
          {{ uploading ? '…' : form.photo ? '🔄 Заменить фото' : '📷 Фото' }}
          <input type="file" accept="image/*" hidden @change="onPhoto" />
        </label>
        <button v-if="form.photo" class="photo-del" @click="form.photo = ''">✖</button>
      </div>

      <h4>Состав</h4>
      <FoodItemsEditor v-model="items" />

      <template v-if="items.length === 0">
        <p class="hint">Или укажите КБЖУ вручную:</p>
        <div class="grid4">
          <label><span>Ккал</span><input v-model.number="form.calories" type="number" min="0" step="1" /></label>
          <label><span>Белки</span><input v-model.number="form.protein" type="number" min="0" step="0.1" /></label>
          <label><span>Жиры</span><input v-model.number="form.fat" type="number" min="0" step="0.1" /></label>
          <label><span>Углев.</span><input v-model.number="form.carbs" type="number" min="0" step="0.1" /></label>
        </div>
      </template>

      <button class="btn primary" :disabled="saving" @click="save">
        {{ saving ? 'Сохраняем…' : '💾 Сохранить' }}
      </button>
      <button class="btn" @click="emit('close')">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.meal-form {
  text-align: left;
  max-height: 88vh;
  overflow-y: auto;
}

.meal-form h3 {
  text-align: center;
}

.meal-form h4 {
  margin: 12px 0 2px;
  font-size: 14px;
}

.meal-form input,
.meal-form select,
.meal-form textarea {
  width: 100%;
  margin-top: 6px;
  font: inherit;
  background: var(--bg-secondary);
  color: var(--text-color);
  border: 1px solid var(--hover-bg-color);
  border-radius: 6px;
  padding: 7px;
}

.meal-form textarea {
  resize: vertical;
}

.row2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

.row2 span,
.fld span {
  display: block;
  font-size: 11px;
  color: var(--text-secondary);
  margin-top: 6px;
}

.fld {
  display: block;
}

.photo-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
}

.photo-preview {
  width: 56px;
  height: 56px;
  object-fit: cover;
  border-radius: 8px;
}

.photo-btn {
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 8px 12px;
  font-size: 13px;
  cursor: pointer;
}

.photo-del {
  background: none;
  border: none;
  color: var(--text-secondary);
}

.grid4 {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 6px;
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

.hint {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 8px 0 0;
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
