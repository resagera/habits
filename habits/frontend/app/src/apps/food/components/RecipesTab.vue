<script setup lang="ts">
import { onMounted, ref } from 'vue'
import * as foodApi from '../api'
import ProductModal from './ProductModal.vue'
import RecipeModal from './RecipeModal.vue'
import TemplateModal from './TemplateModal.vue'
import { assetUrl, r0, type FoodProduct, type FoodRecipe, type FoodTemplate } from '../types'

// Вкладка «Рецепты»: рецепты + управление шаблонами и каталогом продуктов.
const recipes = ref<FoodRecipe[]>([])
const templates = ref<FoodTemplate[]>([])
const products = ref<FoodProduct[]>([])
const loading = ref(true)
const failed = ref(false)
const openSec = ref<Record<string, boolean>>({ recipes: true, templates: false, products: false })

const productQ = ref('')
const showArchived = ref(false)
let debounceTimer: ReturnType<typeof setTimeout> | null = null

async function load() {
  loading.value = true
  failed.value = false
  try {
    const [r, t] = await Promise.all([foodApi.fetchRecipes(), foodApi.fetchTemplates()])
    recipes.value = r.recipes
    templates.value = t.templates
    await loadProducts()
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
}

async function loadProducts() {
  try {
    products.value = (
      await foodApi.fetchProducts(productQ.value.trim(), showArchived.value, 100)
    ).products
  } catch {
    /* список просто не обновится */
  }
}

function onProductInput() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(loadProducts, 300)
}

onMounted(load)

const recipeModal = ref(false)
const editingRecipe = ref<FoodRecipe | null>(null)
const tplModal = ref(false)
const editingTpl = ref<FoodTemplate | null>(null)
const prodModal = ref(false)
const editingProd = ref<FoodProduct | null>(null)

function onSaved() {
  recipeModal.value = false
  tplModal.value = false
  prodModal.value = false
  load()
}
</script>

<template>
  <div v-if="loading" class="hint">Загрузка…</div>
  <p v-else-if="failed" class="hint">
    Не удалось загрузить <button class="retry" @click="load">повторить</button>
  </p>

  <template v-else>
    <!-- рецепты -->
    <section class="sec card-glass">
      <button class="sec-head" @click="openSec.recipes = !openSec.recipes">
        <h3>📖 Рецепты <span class="cnt">({{ recipes.length }})</span> {{ openSec.recipes ? '▴' : '▾' }}</h3>
      </button>
      <template v-if="openSec.recipes">
        <p v-if="recipes.length === 0" class="empty">
          У вас пока нет рецептов.<br />Создайте рецепт или сохраните блюдо из дневника (меню ⋮ записи).
        </p>
        <button
          v-for="rec in recipes"
          :key="rec.id"
          class="row"
          :class="{ archived: rec.archived }"
          @click="editingRecipe = rec; recipeModal = true"
        >
          <img v-if="rec.photo" :src="assetUrl(rec.photo)" class="row-photo" loading="lazy" alt="" />
          <span class="row-body">
            <span class="row-name">{{ rec.name }} <i v-if="rec.archived">(архив)</i></span>
            <span class="row-sub">
              {{ r0(rec.calories) }} ккал всего
              <template v-if="rec.final_weight > 0"> · {{ r0((rec.calories / rec.final_weight) * 100) }} ккал/100 г</template>
              <template v-if="rec.portions > 0"> · {{ r0(rec.calories / rec.portions) }} ккал/порция</template>
              · {{ rec.items.length }} ингр.
            </span>
          </span>
        </button>
        <button class="add" @click="editingRecipe = null; recipeModal = true">＋ Новый рецепт</button>
      </template>
    </section>

    <!-- шаблоны -->
    <section class="sec card-glass">
      <button class="sec-head" @click="openSec.templates = !openSec.templates">
        <h3>📋 Шаблоны <span class="cnt">({{ templates.length }})</span> {{ openSec.templates ? '▴' : '▾' }}</h3>
      </button>
      <template v-if="openSec.templates">
        <p v-if="templates.length === 0" class="empty">
          Шаблонов пока нет — сохраните приём пищи как шаблон или создайте вручную.
        </p>
        <button
          v-for="t in templates"
          :key="t.id"
          class="row"
          :class="{ archived: t.archived }"
          @click="editingTpl = t; tplModal = true"
        >
          <img v-if="t.photo" :src="assetUrl(t.photo)" class="row-photo" loading="lazy" alt="" />
          <span class="row-body">
            <span class="row-name">{{ t.name }} <i v-if="t.archived">(архив)</i></span>
            <span class="row-sub">{{ r0(t.calories) }} ккал · {{ t.items.length }} эл.</span>
          </span>
        </button>
        <button class="add" @click="editingTpl = null; tplModal = true">＋ Новый шаблон</button>
      </template>
    </section>

    <!-- продукты -->
    <section class="sec card-glass">
      <button class="sec-head" @click="openSec.products = !openSec.products">
        <h3>🥫 Продукты {{ openSec.products ? '▴' : '▾' }}</h3>
      </button>
      <template v-if="openSec.products">
        <input v-model="productQ" class="p-search" placeholder="Поиск продуктов…" @input="onProductInput" />
        <label class="arch-toggle">
          <input v-model="showArchived" type="checkbox" @change="loadProducts" />
          показать архив
        </label>
        <p v-if="products.length === 0" class="empty">Продуктов не найдено</p>
        <button v-for="p in products" :key="p.id" class="row" @click="editingProd = p; prodModal = true">
          <span class="row-body">
            <span class="row-name">{{ p.name }} <i v-if="p.brand">{{ p.brand }}</i></span>
            <span class="row-sub">
              {{ r0(p.calories) }} ккал · Б {{ r0(p.protein) }} Ж {{ r0(p.fat) }} У {{ r0(p.carbs) }} /
              100 {{ p.base_type === 'ml' ? 'мл' : 'г' }}
            </span>
          </span>
        </button>
        <button class="add" @click="editingProd = null; prodModal = true">＋ Новый продукт</button>
      </template>
    </section>
  </template>

  <RecipeModal v-if="recipeModal" :recipe="editingRecipe" @saved="onSaved" @close="recipeModal = false" />
  <TemplateModal v-if="tplModal" :template="editingTpl" @saved="onSaved" @close="tplModal = false" />
  <ProductModal v-if="prodModal" :product="editingProd" @saved="onSaved" @close="prodModal = false" />
</template>

<style scoped>
.sec {
  background: var(--card-color);
  border-radius: 8px;
  padding: 10px 12px;
  margin-bottom: 12px;
}

.sec-head {
  width: 100%;
  background: none;
  border: none;
  color: var(--text-color);
  text-align: left;
  padding: 0;
}

.sec-head h3 {
  margin: 0;
  font-size: 15px;
}

.cnt {
  color: var(--text-secondary);
  font-weight: 400;
  font-size: 13px;
}

.row {
  display: flex;
  gap: 10px;
  align-items: center;
  width: 100%;
  text-align: left;
  background: none;
  border: none;
  border-top: 1px solid var(--bg-secondary);
  color: var(--text-color);
  padding: 8px 0;
  margin-top: 4px;
}

.row.archived {
  opacity: 0.55;
}

.row-photo {
  width: 42px;
  height: 42px;
  object-fit: cover;
  border-radius: 8px;
  flex: none;
}

.row-body {
  min-width: 0;
}

.row-name {
  display: block;
  font-size: 14px;
  font-weight: 600;
  overflow-wrap: anywhere;
}

.row-name i {
  font-weight: 400;
  font-style: normal;
  color: var(--text-secondary);
  font-size: 12px;
}

.row-sub {
  font-size: 11px;
  color: var(--text-secondary);
}

.add {
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

.p-search {
  width: 100%;
  margin-top: 8px;
}

.arch-toggle {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 6px;
}

.arch-toggle input {
  width: auto;
}

.empty {
  font-size: 13px;
  color: var(--text-secondary);
  text-align: center;
  padding: 10px 0;
}

.hint {
  text-align: center;
  color: var(--text-secondary);
  padding: 20px 0;
}

.retry {
  background: none;
  border: none;
  color: var(--accent-color);
  text-decoration: underline;
}
</style>
