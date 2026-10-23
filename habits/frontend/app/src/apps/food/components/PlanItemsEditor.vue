<script setup lang="ts">
import { computed, ref } from 'vue'
import { showToast } from '../../../shared/toast'
import * as foodApi from '../api'
import ProductPicker from './ProductPicker.vue'
import {
  r0,
  r1,
  UNIT_LABELS,
  type FoodPlanItem,
  type FoodProduct,
  type FoodRecipe,
  type FoodUnit,
} from '../types'

// Редактор состава слота плана. Три уровня точности:
//   свободная позиция «≈ что-нибудь мясное» — без КБЖУ;
//   продукт/рецепт с отметкой ≈ — тоже без КБЖУ (известно только «что»);
//   точная позиция — количество задано, КБЖУ считается.
// Приблизительные позиции в сводку не входят и показываются отдельно —
// сумма плана никогда не выдаёт неполные данные за полные.
const items = defineModel<FoodPlanItem[]>({ required: true })

const pickerOpen = ref(false)
const recipesOpen = ref(false)
const expanded = ref<number | null>(null)

const recipes = ref<FoodRecipe[]>([])
const recipesLoading = ref(false)

function recalc(it: FoodPlanItem) {
  if (it.approx) {
    it.calories = it.protein = it.fat = it.carbs = 0
    return
  }
  const k = it.grams / 100
  it.calories = k * it.calories_per
  it.protein = k * it.protein_per
  it.fat = k * it.fat_per
  it.carbs = k * it.carbs_per
}

/** grams по количеству и единице; 0 — вес единицы неизвестен. */
function gramsFor(it: FoodPlanItem, p?: FoodProduct | null): number {
  switch (it.unit) {
    case 'g':
    case 'ml':
      return it.amount
    case 'piece':
      return it.amount * (p?.piece_grams ?? 0)
    case 'portion':
      return it.amount * (p?.portion_grams ?? 0)
  }
}

const pickedProducts = new Map<number, FoodProduct>()

function blank(): FoodPlanItem {
  return {
    kind: 'free',
    ref_id: null,
    name: '',
    approx: true,
    amount: 0,
    unit: 'g',
    grams: 0,
    base_type: 'g',
    calories_per: 0,
    protein_per: 0,
    fat_per: 0,
    carbs_per: 0,
    calories: 0,
    protein: 0,
    fat: 0,
    carbs: 0,
  }
}

function addFree() {
  items.value.push(blank())
}

function addFromProduct(p: FoodProduct) {
  pickedProducts.set(p.id, p)
  const it: FoodPlanItem = {
    ...blank(),
    kind: 'product',
    ref_id: p.id,
    name: p.name,
    approx: false,
    amount: 100,
    unit: p.base_type,
    grams: 100,
    base_type: p.base_type,
    calories_per: p.calories,
    protein_per: p.protein,
    fat_per: p.fat,
    carbs_per: p.carbs,
  }
  recalc(it)
  items.value.push(it)
  pickerOpen.value = false
}

async function openRecipes() {
  recipesOpen.value = true
  if (recipes.value.length) return
  recipesLoading.value = true
  try {
    recipes.value = (await foodApi.fetchRecipes()).recipes.filter((r) => !r.archived)
  } catch {
    showToast('Не удалось загрузить рецепты')
  } finally {
    recipesLoading.value = false
  }
}

/**
 * Рецепт в план: КБЖУ на 100 г берутся из итогов и веса блюда. Если итоговый
 * вес не задан, посчитать порцию не из чего — позиция становится примерной.
 */
function addFromRecipe(rec: FoodRecipe) {
  const it: FoodPlanItem = { ...blank(), kind: 'recipe', ref_id: rec.id, name: rec.name }
  if (rec.final_weight > 0) {
    const portion = rec.portions > 0 ? rec.final_weight / rec.portions : rec.final_weight
    it.approx = false
    it.amount = Math.round(portion)
    it.unit = 'g'
    it.grams = Math.round(portion)
    it.calories_per = (rec.calories / rec.final_weight) * 100
    it.protein_per = (rec.protein / rec.final_weight) * 100
    it.fat_per = (rec.fat / rec.final_weight) * 100
    it.carbs_per = (rec.carbs / rec.final_weight) * 100
    recalc(it)
  } else {
    showToast('У рецепта не задан итоговый вес — позиция добавлена как примерная')
  }
  items.value.push(it)
  recipesOpen.value = false
}

function onAmountChange(it: FoodPlanItem) {
  const p = it.ref_id ? pickedProducts.get(it.ref_id) : null
  const g = gramsFor(it, p)
  if (g > 0) it.grams = g
  else if (it.unit === 'piece' || it.unit === 'portion') {
    showToast('Вес одной единицы не задан — укажите вес вручную')
  }
  recalc(it)
}

/** Переключение «≈»: точная позиция получает вес по умолчанию, примерная — обнуляется. */
function toggleApprox(it: FoodPlanItem) {
  it.approx = !it.approx
  if (it.approx) {
    it.amount = 0
    it.grams = 0
  } else if (it.grams <= 0) {
    it.amount = 100
    it.unit = it.base_type
    it.grams = 100
  }
  recalc(it)
}

function remove(i: number) {
  items.value.splice(i, 1)
  if (expanded.value === i) expanded.value = null
}

const unitOptions: FoodUnit[] = ['g', 'ml', 'piece', 'portion']

const total = computed(() => {
  let c = 0,
    p = 0,
    f = 0,
    cb = 0,
    approx = 0
  for (const it of items.value) {
    if (it.approx) {
      approx++
      continue
    }
    c += it.calories
    p += it.protein
    f += it.fat
    cb += it.carbs
  }
  return { c, p, f, cb, approx, counted: items.value.length - approx }
})
</script>

<template>
  <div class="plan-items">
    <div v-for="(it, i) in items" :key="i" class="item" :class="{ approx: it.approx }">
      <div class="item-main">
        <input
          v-if="it.kind === 'free'"
          v-model="it.name"
          class="name-in"
          maxlength="200"
          placeholder="Например: что-нибудь мясное"
        />
        <button v-else class="item-name" @click="expanded = expanded === i ? null : i">
          {{ it.kind === 'recipe' ? '📖 ' : '' }}{{ it.name }}
          <span v-if="!it.approx" class="item-kbju">{{ r0(it.calories) }} ккал</span>
        </button>

        <template v-if="!it.approx">
          <input
            v-model.number="it.amount"
            type="number"
            min="0.1"
            step="any"
            class="num"
            @input="onAmountChange(it)"
          />
          <select v-model="it.unit" class="unit" @change="onAmountChange(it)">
            <option v-for="u in unitOptions" :key="u" :value="u">{{ UNIT_LABELS[u] }}</option>
          </select>
        </template>

        <button
          class="approx-btn"
          :class="{ on: it.approx }"
          :title="it.approx ? 'Примерно — КБЖУ не считается' : 'Точно — КБЖУ считается'"
          @click="toggleApprox(it)"
        >
          ≈
        </button>
        <button class="x" title="Убрать" @click="remove(i)">✖</button>
      </div>

      <div v-if="expanded === i && !it.approx" class="item-detail">
        <label v-if="it.unit === 'piece' || it.unit === 'portion'" class="d-row">
          <span>Вес, {{ it.base_type === 'ml' ? 'мл' : 'г' }}</span>
          <input
            v-model.number="it.grams"
            type="number"
            min="0.1"
            step="any"
            class="num"
            @input="recalc(it)"
          />
        </label>
        <p class="d-hint">На 100 {{ it.base_type === 'ml' ? 'мл' : 'г' }} (только для этого плана):</p>
        <div class="d-grid">
          <label><span>Ккал</span><input v-model.number="it.calories_per" type="number" min="0" step="0.1" @input="recalc(it)" /></label>
          <label><span>Белки</span><input v-model.number="it.protein_per" type="number" min="0" step="0.1" @input="recalc(it)" /></label>
          <label><span>Жиры</span><input v-model.number="it.fat_per" type="number" min="0" step="0.1" @input="recalc(it)" /></label>
          <label><span>Углев.</span><input v-model.number="it.carbs_per" type="number" min="0" step="0.1" @input="recalc(it)" /></label>
        </div>
        <p class="d-hint">
          Итог: {{ r1(it.calories) }} ккал · Б {{ r1(it.protein) }} · Ж {{ r1(it.fat) }} · У
          {{ r1(it.carbs) }}
        </p>
      </div>

      <p v-if="it.approx && it.kind !== 'free'" class="approx-hint">
        Отмечено «примерно» — в сводку по калориям не войдёт
      </p>
    </div>

    <div class="add-row">
      <button class="add-btn" @click="pickerOpen = true">＋ Продукт</button>
      <button class="add-btn" @click="openRecipes">＋ Рецепт</button>
      <button class="add-btn" @click="addFree">＋ Примерно</button>
    </div>

    <p v-if="items.length" class="total">
      Итого: <b>{{ r0(total.c) }} ккал</b> · Б {{ r1(total.p) }} · Ж {{ r1(total.f) }} · У
      {{ r1(total.cb) }}
      <span v-if="total.approx" class="total-approx">
        · посчитано {{ total.counted }} из {{ items.length }}
      </span>
    </p>

    <ProductPicker v-if="pickerOpen" @pick="addFromProduct" @close="pickerOpen = false" />

    <div v-if="recipesOpen" class="modal" @click.self="recipesOpen = false">
      <div class="modal-content picker">
        <h3>📖 Рецепт</h3>
        <p v-if="recipesLoading" class="hint">Загрузка…</p>
        <p v-else-if="recipes.length === 0" class="hint">
          Рецептов пока нет — создайте их на вкладке «Рецепты».
        </p>
        <div v-else class="results">
          <button v-for="rec in recipes" :key="rec.id" class="result" @click="addFromRecipe(rec)">
            <span class="r-name">{{ rec.name }}</span>
            <span class="r-kbju">
              {{ r0(rec.calories) }} ккал за блюдо
              <template v-if="rec.final_weight > 0"> · {{ r0(rec.final_weight) }} г</template>
              <template v-else> · вес не задан — будет «примерно»</template>
            </span>
          </button>
        </div>
        <button class="btn" @click="recipesOpen = false">Отмена</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.item {
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 6px 8px;
  margin-top: 6px;
}

.item.approx {
  border-left: 2px solid var(--text-secondary);
}

.item-main {
  display: flex;
  align-items: center;
  gap: 6px;
}

.item-name {
  flex: 1;
  min-width: 0;
  text-align: left;
  background: none;
  border: none;
  color: var(--text-color);
  font-size: 13px;
  overflow-wrap: anywhere;
  padding: 2px 0;
}

.name-in {
  flex: 1;
  min-width: 0;
  padding: 5px 6px;
  font-size: 13px;
}

.item-kbju {
  color: var(--text-secondary);
  font-size: 11px;
  margin-left: 4px;
}

.num {
  width: 62px;
  padding: 5px 6px;
  text-align: center;
  flex: none;
}

.unit {
  width: 70px;
  padding: 5px 4px;
  flex: none;
}

.approx-btn {
  flex: none;
  width: 26px;
  border: 1px solid var(--hover-bg-color);
  border-radius: 6px;
  background: none;
  color: var(--text-secondary);
  font-size: 14px;
  padding: 3px 0;
}

.approx-btn.on {
  background: var(--accent-color);
  border-color: var(--accent-color);
  color: #fff;
}

.x {
  background: none;
  border: none;
  color: var(--text-secondary);
  padding: 2px 4px;
  flex: none;
}

.item-detail {
  border-top: 1px solid var(--hover-bg-color);
  margin-top: 6px;
  padding-top: 6px;
}

.approx-hint {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 4px 0 0;
}

.d-row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--text-secondary);
  margin-bottom: 4px;
}

.d-hint {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 4px 0;
}

.d-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 6px;
}

.d-grid span {
  display: block;
  font-size: 10px;
  color: var(--text-secondary);
}

.d-grid input {
  width: 100%;
  margin-top: 2px;
  padding: 5px;
}

.add-row {
  display: flex;
  gap: 6px;
  margin-top: 8px;
}

.add-btn {
  flex: 1;
  padding: 8px 4px;
  border: 1px dashed var(--hover-bg-color);
  border-radius: 8px;
  background: none;
  color: var(--accent-color);
  font-size: 12px;
}

.total {
  font-size: 13px;
  margin: 8px 0 0;
}

.total-approx {
  color: var(--text-secondary);
  font-size: 12px;
}

.picker {
  text-align: left;
  max-height: 85vh;
  overflow-y: auto;
}

.picker h3 {
  text-align: center;
}

.results {
  max-height: 50vh;
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

.r-kbju {
  font-size: 11px;
  color: var(--text-secondary);
}

.hint {
  color: var(--text-secondary);
  font-size: 13px;
  text-align: center;
  margin: 10px 0;
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
</style>
