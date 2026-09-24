<script setup lang="ts">
import { computed, ref } from 'vue'
import { showToast } from '../../../shared/toast'
import * as foodApi from '../api'
import FoodItemsEditor from './FoodItemsEditor.vue'
import { assetUrl, r0, r1, type FoodItem, type FoodRecipe } from '../types'

const props = defineProps<{ recipe: FoodRecipe | null }>()
const emit = defineEmits<{ saved: []; close: [] }>()

const form = ref({
  name: props.recipe?.name ?? '',
  description: props.recipe?.description ?? '',
  steps: props.recipe?.steps ?? '',
  photo: props.recipe?.photo ?? '',
  final_weight: props.recipe?.final_weight ?? 0,
  portions: props.recipe?.portions ?? 0,
  archived: props.recipe?.archived ?? false,
})
const items = ref<FoodItem[]>(props.recipe ? props.recipe.items.map((i) => ({ ...i })) : [])
const saving = ref(false)
const uploading = ref(false)
const confirmDel = ref(false)

const totals = computed(() => {
  let c = 0,
    p = 0,
    f = 0,
    cb = 0
  for (const it of items.value) {
    c += it.calories
    p += it.protein
    f += it.fat
    cb += it.carbs
  }
  return { c, p, f, cb }
})

const per100 = computed(() => {
  const w = form.value.final_weight
  if (w <= 0) return null
  return {
    c: (totals.value.c / w) * 100,
    p: (totals.value.p / w) * 100,
    f: (totals.value.f / w) * 100,
    cb: (totals.value.cb / w) * 100,
  }
})

const perPortion = computed(() => {
  const n = form.value.portions
  if (n <= 0) return null
  return { c: totals.value.c / n, p: totals.value.p / n, f: totals.value.f / n, cb: totals.value.cb / n }
})

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
    showToast('Укажите название рецепта')
    return
  }
  saving.value = true
  try {
    const payload = { ...form.value, items: items.value }
    if (props.recipe) await foodApi.updateRecipe(props.recipe.id, payload)
    else await foodApi.createRecipe(payload)
    emit('saved')
    showToast('Рецепт сохранён ✅')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось сохранить')
  } finally {
    saving.value = false
  }
}

async function del() {
  if (!props.recipe) return
  if (!confirmDel.value) {
    confirmDel.value = true
    return
  }
  try {
    await foodApi.deleteRecipe(props.recipe.id)
    emit('saved')
    showToast('Рецепт удалён')
  } catch {
    showToast('Не удалось удалить')
  }
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content rec-form">
      <h3>{{ recipe ? '✏️ Рецепт' : '📖 Новый рецепт' }}</h3>
      <input v-model="form.name" placeholder="Название рецепта" maxlength="200" />
      <textarea v-model="form.description" placeholder="Описание…" maxlength="2000" rows="2"></textarea>

      <div class="photo-row">
        <img v-if="form.photo" :src="assetUrl(form.photo)" class="photo-preview" alt="" />
        <label class="photo-btn">
          {{ uploading ? '…' : form.photo ? '🔄 Заменить фото' : '📷 Фото' }}
          <input type="file" accept="image/*" hidden @change="onPhoto" />
        </label>
        <button v-if="form.photo" class="photo-del" @click="form.photo = ''">✖</button>
      </div>

      <h4>Ингредиенты</h4>
      <FoodItemsEditor v-model="items" />

      <div class="row2">
        <label><span>Итоговый вес блюда, г</span><input v-model.number="form.final_weight" type="number" min="0" step="1" /></label>
        <label><span>Порций</span><input v-model.number="form.portions" type="number" min="0" step="0.5" /></label>
      </div>

      <div v-if="items.length" class="calc">
        <div>Всего: <b>{{ r0(totals.c) }} ккал</b> · Б {{ r1(totals.p) }} · Ж {{ r1(totals.f) }} · У {{ r1(totals.cb) }}</div>
        <div v-if="per100">На 100 г: {{ r0(per100.c) }} ккал · Б {{ r1(per100.p) }} · Ж {{ r1(per100.f) }} · У {{ r1(per100.cb) }}</div>
        <div v-if="perPortion">На порцию: {{ r0(perPortion.c) }} ккал · Б {{ r1(perPortion.p) }} · Ж {{ r1(perPortion.f) }} · У {{ r1(perPortion.cb) }}</div>
      </div>

      <h4>Шаги приготовления (необязательно)</h4>
      <textarea v-model="form.steps" placeholder="1. …&#10;2. …" maxlength="10000" rows="4"></textarea>

      <label v-if="recipe" class="arch">
        <input v-model="form.archived" type="checkbox" />
        В архиве
      </label>

      <button class="btn primary" :disabled="saving" @click="save">
        {{ saving ? 'Сохраняем…' : '💾 Сохранить' }}
      </button>
      <button v-if="recipe" class="btn danger" @click="del">
        {{ confirmDel ? 'Точно удалить рецепт?' : '🗑 Удалить' }}
      </button>
      <button class="btn" @click="emit('close')">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.rec-form {
  text-align: left;
  max-height: 88vh;
  overflow-y: auto;
}

.rec-form h3 {
  text-align: center;
}

.rec-form h4 {
  margin: 12px 0 2px;
  font-size: 14px;
}

.rec-form input,
.rec-form textarea {
  width: 100%;
  margin-top: 6px;
  font: inherit;
  background: var(--bg-secondary);
  color: var(--text-color);
  border: 1px solid var(--hover-bg-color);
  border-radius: 6px;
  padding: 7px;
}

.rec-form textarea {
  resize: vertical;
}

.row2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  margin-top: 4px;
}

.row2 span {
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

.calc {
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 8px 10px;
  margin-top: 8px;
  font-size: 12px;
  line-height: 1.6;
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
