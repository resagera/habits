<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { showToast } from '../../../shared/toast'
import * as foodApi from '../api'
import { r0, type FoodProduct } from '../types'

// Поиск по каталогу продуктов (название/альт. название/производитель;
// недавние и частые — первыми) + создание нового продукта на месте.
const emit = defineEmits<{ pick: [product: FoodProduct]; close: [] }>()

const q = ref('')
const results = ref<FoodProduct[]>([])
const loading = ref(true)
const failed = ref(false)
let debounceTimer: ReturnType<typeof setTimeout> | null = null

async function search() {
  loading.value = true
  failed.value = false
  try {
    results.value = (await foodApi.fetchProducts(q.value.trim(), false, 50)).products
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
}

function onInput() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(search, 300)
}

onMounted(search)

// --- создание нового продукта ---
const creating = ref(false)
const saving = ref(false)
const form = ref({
  name: '',
  brand: '',
  base_type: 'g' as 'g' | 'ml',
  calories: 0,
  protein: 0,
  fat: 0,
  carbs: 0,
  piece_grams: 0,
  portion_grams: 0,
})

function openCreate() {
  form.value.name = q.value.trim()
  creating.value = true
}

async function saveNew() {
  if (!form.value.name.trim()) {
    showToast('Укажите название продукта')
    return
  }
  saving.value = true
  try {
    const { product } = await foodApi.createProduct(form.value)
    emit('pick', product)
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось создать продукт')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content picker">
      <template v-if="!creating">
        <h3>🔍 Продукт</h3>
        <input
          v-model="q"
          placeholder="Поиск: название, производитель…"
          @input="onInput"
        />
        <p v-if="loading" class="hint">Ищем…</p>
        <p v-else-if="failed" class="hint">
          Не удалось загрузить <button class="retry" @click="search">повторить</button>
        </p>
        <div v-else class="results">
          <p v-if="results.length === 0" class="hint">
            Ничего не найдено — создайте продукт 👇
          </p>
          <button v-for="p in results" :key="p.id" class="result" @click="emit('pick', p)">
            <span class="r-name">
              {{ p.name }}
              <span v-if="p.brand" class="r-brand">{{ p.brand }}</span>
              <span v-if="p.recent" class="r-recent" title="Недавно использовался">🕐</span>
            </span>
            <span class="r-kbju">
              {{ r0(p.calories) }} ккал · Б{{ r0(p.protein) }} Ж{{ r0(p.fat) }} У{{ r0(p.carbs) }}
              / 100 {{ p.base_type === 'ml' ? 'мл' : 'г' }}
            </span>
          </button>
        </div>
        <button class="btn primary" @click="openCreate">＋ Новый продукт</button>
        <button class="btn" @click="emit('close')">Отмена</button>
      </template>

      <template v-else>
        <h3>🥫 Новый продукт</h3>
        <input v-model="form.name" placeholder="Название" maxlength="200" />
        <input v-model="form.brand" placeholder="Производитель (необязательно)" maxlength="200" />
        <label class="row">
          <span>База расчёта</span>
          <select v-model="form.base_type">
            <option value="g">100 г</option>
            <option value="ml">100 мл</option>
          </select>
        </label>
        <div class="grid4">
          <label><span>Ккал</span><input v-model.number="form.calories" type="number" min="0" step="0.1" /></label>
          <label><span>Белки</span><input v-model.number="form.protein" type="number" min="0" step="0.1" /></label>
          <label><span>Жиры</span><input v-model.number="form.fat" type="number" min="0" step="0.1" /></label>
          <label><span>Углев.</span><input v-model.number="form.carbs" type="number" min="0" step="0.1" /></label>
        </div>
        <div class="grid4 two">
          <label><span>1 шт, г</span><input v-model.number="form.piece_grams" type="number" min="0" step="0.1" /></label>
          <label><span>1 порция, г</span><input v-model.number="form.portion_grams" type="number" min="0" step="1" /></label>
        </div>
        <button class="btn primary" :disabled="saving" @click="saveNew">
          {{ saving ? '…' : '💾 Создать и добавить' }}
        </button>
        <button class="btn" @click="creating = false">← Назад к поиску</button>
      </template>
    </div>
  </div>
</template>

<style scoped>
.picker {
  text-align: left;
  max-height: 85vh;
  overflow-y: auto;
}

.picker h3 {
  text-align: center;
}

.picker input,
.picker select {
  width: 100%;
  margin-top: 6px;
}

.results {
  max-height: 42vh;
  overflow-y: auto;
  margin-top: 8px;
}

.result {
  display: block;
  width: 100%;
  text-align: left;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  padding: 8px 10px;
  margin-bottom: 6px;
  color: var(--text-color);
}

.r-name {
  display: block;
  font-weight: 600;
  overflow-wrap: anywhere;
}

.r-brand {
  font-weight: 400;
  color: var(--text-secondary);
  font-size: 12px;
}

.r-recent {
  font-size: 11px;
}

.r-kbju {
  font-size: 11px;
  color: var(--text-secondary);
}

.row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
  font-size: 13px;
}

.row span {
  color: var(--text-secondary);
  flex: none;
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

.hint {
  color: var(--text-secondary);
  font-size: 13px;
  text-align: center;
  margin: 10px 0;
}

.retry {
  background: none;
  border: none;
  color: var(--accent-color);
  text-decoration: underline;
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
