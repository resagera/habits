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
  type FoodTemplate,
  type MealType,
} from '../types'

const props = defineProps<{ template: FoodTemplate | null }>()
const emit = defineEmits<{ saved: []; close: [] }>()

const form = ref({
  name: props.template?.name ?? '',
  description: props.template?.description ?? '',
  photo: props.template?.photo ?? '',
  meal_type: (props.template?.meal_type ?? 'none') as MealType,
  archived: props.template?.archived ?? false,
})
const items = ref<FoodItem[]>(props.template ? props.template.items.map((i) => ({ ...i })) : [])
const saving = ref(false)
const uploading = ref(false)
const confirmDel = ref(false)

async function onPhoto(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  uploading.value = true
  try {
    form.value.photo = (await foodApi.uploadPhoto(file)).url
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось загрузить фото')
  } finally {
    uploading.value = false
  }
}

async function save() {
  if (!form.value.name.trim()) {
    showToast('Укажите название шаблона')
    return
  }
  saving.value = true
  try {
    const payload = { ...form.value, items: items.value }
    if (props.template) await foodApi.updateTemplate(props.template.id, payload)
    else await foodApi.createTemplate(payload)
    emit('saved')
    showToast('Шаблон сохранён ✅')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось сохранить')
  } finally {
    saving.value = false
  }
}

async function del() {
  if (!props.template) return
  if (!confirmDel.value) {
    confirmDel.value = true
    return
  }
  try {
    await foodApi.deleteTemplate(props.template.id)
    emit('saved')
    showToast('Шаблон удалён')
  } catch {
    showToast('Не удалось удалить')
  }
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content tpl-form">
      <h3>{{ template ? '✏️ Шаблон' : '📋 Новый шаблон' }}</h3>
      <input v-model="form.name" placeholder="Название" maxlength="200" />
      <textarea v-model="form.description" placeholder="Описание…" maxlength="2000" rows="2"></textarea>
      <label class="fld">
        <span>Категория по умолчанию</span>
        <select v-model="form.meal_type">
          <option v-for="t in MEAL_TYPES" :key="t" :value="t">{{ MEAL_TYPE_LABELS[t] }}</option>
        </select>
      </label>

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

      <label v-if="template" class="arch">
        <input v-model="form.archived" type="checkbox" />
        В архиве
      </label>

      <button class="btn primary" :disabled="saving" @click="save">
        {{ saving ? 'Сохраняем…' : '💾 Сохранить' }}
      </button>
      <button v-if="template" class="btn danger" @click="del">
        {{ confirmDel ? 'Точно удалить шаблон?' : '🗑 Удалить' }}
      </button>
      <button class="btn" @click="emit('close')">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.tpl-form {
  text-align: left;
  max-height: 88vh;
  overflow-y: auto;
}

.tpl-form h3 {
  text-align: center;
}

.tpl-form h4 {
  margin: 12px 0 2px;
  font-size: 14px;
}

.tpl-form input,
.tpl-form select,
.tpl-form textarea {
  width: 100%;
  margin-top: 6px;
  font: inherit;
  background: var(--bg-secondary);
  color: var(--text-color);
  border: 1px solid var(--hover-bg-color);
  border-radius: 6px;
  padding: 7px;
}

.tpl-form textarea {
  resize: vertical;
}

.fld span {
  display: block;
  font-size: 11px;
  color: var(--text-secondary);
  margin-top: 6px;
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

.arch {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  margin-top: 10px;
}

.arch input {
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
