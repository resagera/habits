<script setup lang="ts">
import { computed, ref } from 'vue'
import { showToast } from '../../../shared/toast'
import ProductPicker from './ProductPicker.vue'
import { r0, r1, UNIT_LABELS, type FoodItem, type FoodProduct, type FoodUnit } from '../types'

// Редактор состава (приём пищи / шаблон / рецепт): добавление из каталога,
// количество + единица, авто-пересчёт КБЖУ. Данные — снимок: правка значений
// здесь не меняет глобальный продукт.
const items = defineModel<FoodItem[]>({ required: true })

const pickerOpen = ref(false)
const expanded = ref<number | null>(null) // индекс раскрытого элемента

function recalc(it: FoodItem) {
  const k = it.grams / 100
  it.calories = k * it.calories_per
  it.protein = k * it.protein_per
  it.fat = k * it.fat_per
  it.carbs = k * it.carbs_per
}

/** grams по количеству и единице; 0 — вес неизвестен (нужен ручной ввод). */
function gramsFor(it: FoodItem, p?: FoodProduct | null): number {
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

function addFromProduct(p: FoodProduct) {
  pickedProducts.set(p.id, p)
  const it: FoodItem = {
    product_id: p.id,
    name: p.name,
    amount: 100,
    unit: p.base_type,
    grams: 100,
    base_type: p.base_type,
    calories_per: p.calories,
    protein_per: p.protein,
    fat_per: p.fat,
    carbs_per: p.carbs,
    calories: 0,
    protein: 0,
    fat: 0,
    carbs: 0,
  }
  recalc(it)
  items.value.push(it)
  pickerOpen.value = false
}

function onAmountChange(it: FoodItem) {
  const p = it.product_id ? pickedProducts.get(it.product_id) : null
  const g = gramsFor(it, p)
  if (g > 0) it.grams = g
  else if (it.unit === 'piece' || it.unit === 'portion') {
    // вес единицы не задан у продукта — оставляем grams на ручной ввод
    showToast('Вес одной единицы не задан — укажите вес вручную')
  }
  recalc(it)
}

function onUnitChange(it: FoodItem) {
  onAmountChange(it)
}

function onGramsChange(it: FoodItem) {
  recalc(it)
}

function onPerChange(it: FoodItem) {
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
    cb = 0
  for (const it of items.value) {
    c += it.calories
    p += it.protein
    f += it.fat
    cb += it.carbs
  }
  return { c, p, f, cb }
})
</script>

<template>
  <div class="items-editor">
    <div v-for="(it, i) in items" :key="i" class="item">
      <div class="item-main">
        <button class="item-name" @click="expanded = expanded === i ? null : i">
          {{ it.name }}
          <span class="item-kbju">{{ r0(it.calories) }} ккал</span>
        </button>
        <input
          v-model.number="it.amount"
          type="number"
          min="0.1"
          step="any"
          class="num"
          @input="onAmountChange(it)"
        />
        <select v-model="it.unit" class="unit" @change="onUnitChange(it)">
          <option v-for="u in unitOptions" :key="u" :value="u">{{ UNIT_LABELS[u] }}</option>
        </select>
        <button class="x" title="Убрать" @click="remove(i)">✖</button>
      </div>

      <div v-if="expanded === i" class="item-detail">
        <label v-if="it.unit === 'piece' || it.unit === 'portion'" class="d-row">
          <span>Фактический вес, {{ it.base_type === 'ml' ? 'мл' : 'г' }}</span>
          <input v-model.number="it.grams" type="number" min="0.1" step="any" class="num" @input="onGramsChange(it)" />
        </label>
        <p class="d-hint">
          На 100 {{ it.base_type === 'ml' ? 'мл' : 'г' }} (правка — только для этой записи):
        </p>
        <div class="d-grid">
          <label><span>Ккал</span><input v-model.number="it.calories_per" type="number" min="0" step="0.1" @input="onPerChange(it)" /></label>
          <label><span>Белки</span><input v-model.number="it.protein_per" type="number" min="0" step="0.1" @input="onPerChange(it)" /></label>
          <label><span>Жиры</span><input v-model.number="it.fat_per" type="number" min="0" step="0.1" @input="onPerChange(it)" /></label>
          <label><span>Углев.</span><input v-model.number="it.carbs_per" type="number" min="0" step="0.1" @input="onPerChange(it)" /></label>
        </div>
        <p class="d-hint">
          Итог: {{ r1(it.calories) }} ккал · Б {{ r1(it.protein) }} · Ж {{ r1(it.fat) }} · У {{ r1(it.carbs) }}
          ({{ r1(it.grams) }} {{ it.base_type === 'ml' ? 'мл' : 'г' }})
        </p>
      </div>
    </div>

    <button class="add-btn" @click="pickerOpen = true">＋ Добавить продукт</button>

    <p v-if="items.length" class="total">
      Итого: <b>{{ r0(total.c) }} ккал</b> · Б {{ r1(total.p) }} · Ж {{ r1(total.f) }} · У {{ r1(total.cb) }}
    </p>

    <ProductPicker v-if="pickerOpen" @pick="addFromProduct" @close="pickerOpen = false" />
  </div>
</template>

<style scoped>
.item {
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 6px 8px;
  margin-top: 6px;
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

.item-kbju {
  color: var(--text-secondary);
  font-size: 11px;
  margin-left: 4px;
}

.num {
  width: 64px;
  padding: 5px 6px;
  text-align: center;
  flex: none;
}

.unit {
  width: 74px;
  padding: 5px 4px;
  flex: none;
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

.add-btn {
  display: block;
  width: 100%;
  margin-top: 8px;
  padding: 8px;
  border: 1px dashed var(--hover-bg-color);
  border-radius: 8px;
  background: none;
  color: var(--accent-color);
  font-size: 13px;
}

.total {
  font-size: 13px;
  margin: 8px 0 0;
}
</style>
