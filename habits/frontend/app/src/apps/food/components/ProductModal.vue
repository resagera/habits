<script setup lang="ts">
import { ref } from 'vue'
import { showToast } from '../../../shared/toast'
import * as foodApi from '../api'
import { assetUrl, type FoodProduct } from '../types'

const props = defineProps<{ product: FoodProduct | null }>()
const emit = defineEmits<{ saved: []; close: [] }>()

const form = ref({
  name: props.product?.name ?? '',
  alt_name: props.product?.alt_name ?? '',
  brand: props.product?.brand ?? '',
  category: props.product?.category ?? '',
  photo: props.product?.photo ?? '',
  base_type: (props.product?.base_type ?? 'g') as 'g' | 'ml',
  calories: props.product?.calories ?? 0,
  protein: props.product?.protein ?? 0,
  fat: props.product?.fat ?? 0,
  carbs: props.product?.carbs ?? 0,
  piece_grams: props.product?.piece_grams ?? 0,
  portion_grams: props.product?.portion_grams ?? 0,
  archived: props.product?.archived ?? false,
})
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
    showToast('Укажите название')
    return
  }
  saving.value = true
  try {
    if (props.product) await foodApi.updateProduct(props.product.id, form.value)
    else await foodApi.createProduct(form.value)
    emit('saved')
    showToast('Продукт сохранён ✅')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось сохранить')
  } finally {
    saving.value = false
  }
}

async function del() {
  if (!props.product) return
  if (!confirmDel.value) {
    confirmDel.value = true
    return
  }
  try {
    const { archived } = await foodApi.deleteProduct(props.product.id)
    emit('saved')
    showToast(archived ? 'Продукт использовался в истории — перенесён в архив' : 'Продукт удалён')
  } catch {
    showToast('Не удалось удалить')
  }
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content prod-form">
      <h3>{{ product ? '✏️ Продукт' : '🥫 Новый продукт' }}</h3>
      <input v-model="form.name" placeholder="Название" maxlength="200" />
      <input v-model="form.alt_name" placeholder="Альтернативное название" maxlength="200" />
      <input v-model="form.brand" placeholder="Производитель" maxlength="200" />
      <input v-model="form.category" placeholder="Категория (например, молочные)" maxlength="100" />

      <div class="photo-row">
        <img v-if="form.photo" :src="assetUrl(form.photo)" class="photo-preview" alt="" />
        <label class="photo-btn">
          {{ uploading ? '…' : form.photo ? '🔄 Заменить фото' : '📷 Фото' }}
          <input type="file" accept="image/*" hidden @change="onPhoto" />
        </label>
        <button v-if="form.photo" class="photo-del" @click="form.photo = ''">✖</button>
      </div>

      <label class="fld">
        <span>База расчёта КБЖУ</span>
        <select v-model="form.base_type">
          <option value="g">На 100 г</option>
          <option value="ml">На 100 мл</option>
        </select>
      </label>
      <div class="grid4">
        <label><span>Ккал</span><input v-model.number="form.calories" type="number" min="0" step="0.1" /></label>
        <label><span>Белки</span><input v-model.number="form.protein" type="number" min="0" step="0.1" /></label>
        <label><span>Жиры</span><input v-model.number="form.fat" type="number" min="0" step="0.1" /></label>
        <label><span>Углев.</span><input v-model.number="form.carbs" type="number" min="0" step="0.1" /></label>
      </div>
      <div class="grid4 two">
        <label><span>Вес 1 шт, г</span><input v-model.number="form.piece_grams" type="number" min="0" step="0.1" /></label>
        <label><span>Вес 1 порции, г</span><input v-model.number="form.portion_grams" type="number" min="0" step="1" /></label>
      </div>

      <label v-if="product" class="arch">
        <input v-model="form.archived" type="checkbox" />
        В архиве
      </label>
      <p v-if="product" class="hint">
        Изменение продукта не меняет КБЖУ старых записей дневника — там хранятся снимки.
      </p>

      <button class="btn primary" :disabled="saving" @click="save">
        {{ saving ? 'Сохраняем…' : '💾 Сохранить' }}
      </button>
      <button v-if="product" class="btn danger" @click="del">
        {{ confirmDel ? 'Точно удалить?' : '🗑 Удалить' }}
      </button>
      <button class="btn" @click="emit('close')">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.prod-form {
  text-align: left;
  max-height: 88vh;
  overflow-y: auto;
}

.prod-form h3 {
  text-align: center;
}

.prod-form input,
.prod-form select {
  width: 100%;
  margin-top: 6px;
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

.grid4 {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 6px;
  margin-top: 6px;
}

.grid4.two {
  grid-template-columns: repeat(2, 1fr);
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

.hint {
  font-size: 11px;
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

.btn.danger {
  background: #b91c1c;
  color: #fff;
}

.btn:disabled {
  opacity: 0.5;
}
</style>
