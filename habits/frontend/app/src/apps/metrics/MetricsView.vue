<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { showToast } from '../../shared/toast'
import * as metricsApi from './api'
import type { ChartType, ItemConfig, MetricCategory, MetricItem } from './types'
import ItemSettingsModal from './components/ItemSettingsModal.vue'
import MetricItemCard from './components/MetricItemCard.vue'

const categories = ref<MetricCategory[]>([])
const chartTypes = ref<ChartType[]>([])
const loading = ref(true)
const newCategoryName = ref('')

// сворачивание — локально
const COLLAPSE_KEY = 'metrics_collapsed_v1'
const collapsed = ref<{ cats: number[]; items: number[] }>(
  JSON.parse(localStorage.getItem(COLLAPSE_KEY) || '{"cats":[],"items":[]}'),
)

// модалка категории
const categoryModal = ref(false)
const editingCategory = ref<MetricCategory | null>(null)
const categoryName = ref('')
const confirmDeleteCategory = ref(false)

// модалка элемента (создание/настройки)
const itemModal = ref(false)
const itemModalCategory = ref<MetricCategory | null>(null)
const editingItem = ref<MetricItem | null>(null)
/** пересоздаёт MetricItemCard после изменения настроек */
const itemVersion = ref(0)

onMounted(async () => {
  try {
    const [tree, types] = await Promise.all([metricsApi.fetchTree(), metricsApi.fetchChartTypes()])
    categories.value = tree.categories
    chartTypes.value = types.chart_types
  } catch {
    showToast('Не удалось загрузить метрики')
  } finally {
    loading.value = false
  }
})

function saveCollapsed() {
  localStorage.setItem(COLLAPSE_KEY, JSON.stringify(collapsed.value))
}

function toggleCat(id: number) {
  const i = collapsed.value.cats.indexOf(id)
  if (i >= 0) collapsed.value.cats.splice(i, 1)
  else collapsed.value.cats.push(id)
  saveCollapsed()
}

function toggleItem(id: number) {
  const i = collapsed.value.items.indexOf(id)
  if (i >= 0) collapsed.value.items.splice(i, 1)
  else collapsed.value.items.push(id)
  saveCollapsed()
}

async function addCategory() {
  const name = newCategoryName.value.trim()
  if (!name) return
  try {
    const { category } = await metricsApi.createCategory(name)
    categories.value.push(category)
    newCategoryName.value = ''
  } catch {
    showToast('Не удалось создать категорию')
  }
}

function openCategorySettings(category: MetricCategory) {
  editingCategory.value = category
  categoryName.value = category.name
  confirmDeleteCategory.value = false
  categoryModal.value = true
}

async function saveCategory() {
  const category = editingCategory.value
  if (!category) return
  const name = categoryName.value.trim()
  if (!name) return
  try {
    const { category: updated } = await metricsApi.renameCategory(category.id, name)
    category.name = updated.name
    categoryModal.value = false
  } catch {
    showToast('Не удалось переименовать')
  }
}

async function removeCategory() {
  const category = editingCategory.value
  if (!category) return
  if (!confirmDeleteCategory.value) {
    confirmDeleteCategory.value = true
    setTimeout(() => (confirmDeleteCategory.value = false), 3500)
    return
  }
  try {
    await metricsApi.deleteCategory(category.id)
    categories.value = categories.value.filter((c) => c.id !== category.id)
    categoryModal.value = false
  } catch {
    showToast('Не удалось удалить категорию')
  }
}

function openCreateItem(category: MetricCategory) {
  itemModalCategory.value = category
  editingItem.value = null
  itemModal.value = true
}

function openItemSettings(category: MetricCategory, item: MetricItem) {
  itemModalCategory.value = category
  editingItem.value = item
  itemModal.value = true
}

async function saveItem(name: string, chartType: string, config: ItemConfig) {
  const category = itemModalCategory.value
  if (!category) return
  try {
    if (editingItem.value) {
      const { item } = await metricsApi.updateItem(editingItem.value.id, {
        name,
        chart_type: chartType,
        config,
      })
      const i = category.items.findIndex((x) => x.id === item.id)
      if (i >= 0) category.items[i] = item
      itemVersion.value++
    } else {
      const { item } = await metricsApi.createItem(category.id, name, chartType, config)
      category.items.push(item)
    }
    itemModal.value = false
  } catch {
    showToast('Не удалось сохранить элемент')
  }
}

async function removeItem() {
  const category = itemModalCategory.value
  const item = editingItem.value
  if (!category || !item) return
  try {
    await metricsApi.deleteItem(item.id)
    category.items = category.items.filter((x) => x.id !== item.id)
    itemModal.value = false
  } catch {
    showToast('Не удалось удалить элемент')
  }
}
</script>

<template>
  <div v-if="loading" class="hint">Загрузка…</div>

  <template v-else>
    <p v-if="categories.length === 0" class="hint">
      Создайте категорию (например, «Здоровье»), а в ней — элементы-графики («Вес», «Сон») 👇
    </p>

    <div v-for="category in categories" :key="category.id" class="category">
      <div class="cat-head">
        <button class="cat-toggle" @click="toggleCat(category.id)">
          <span class="chevron" :class="{ open: !collapsed.cats.includes(category.id) }">▸</span>
          {{ category.name }}
        </button>
        <span>
          <button class="icon-btn" title="Добавить элемент" @click="openCreateItem(category)">＋</button>
          <button class="icon-btn" title="Настройки категории" @click="openCategorySettings(category)">⚙️</button>
        </span>
      </div>

      <template v-if="!collapsed.cats.includes(category.id)">
        <p v-if="category.items.length === 0" class="hint small">нет элементов — добавьте ＋</p>
        <MetricItemCard
          v-for="item in category.items"
          :key="`${item.id}-${itemVersion}`"
          :item="item"
          :collapsed="collapsed.items.includes(item.id)"
          @toggle="toggleItem(item.id)"
          @open-settings="openItemSettings(category, item)"
        />
      </template>
    </div>

    <form class="add-category" @submit.prevent="addCategory">
      <input v-model="newCategoryName" placeholder="Новая категория…" maxlength="200" />
      <button type="submit">Добавить категорию</button>
    </form>
  </template>

  <!-- Модалка категории -->
  <div v-if="categoryModal" class="modal" @click.self="categoryModal = false">
    <div class="modal-content">
      <h3>Настройки категории</h3>
      <input v-model="categoryName" maxlength="200" />
      <button class="btn primary" @click="saveCategory">💾 Сохранить</button>
      <button class="btn danger" @click="removeCategory">
        {{ confirmDeleteCategory ? 'Точно удалить со всеми графиками?' : '🗑 Удалить категорию' }}
      </button>
      <button class="btn" @click="categoryModal = false">Отмена</button>
    </div>
  </div>

  <!-- Модалка элемента -->
  <ItemSettingsModal
    v-if="itemModal"
    :key="editingItem?.id ?? 'new'"
    :chart-types="chartTypes"
    :item="editingItem"
    @save="saveItem"
    @remove="removeItem"
    @close="itemModal = false"
  />
</template>

<style scoped>
.hint {
  text-align: center;
  color: var(--text-secondary);
  padding: 24px 0;
}

.hint.small {
  padding: 6px 0;
  font-size: 12px;
}

.category {
  background: var(--card-color);
  border-radius: 8px;
  padding: 8px 12px;
  margin-bottom: 14px;
}

.cat-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.cat-toggle {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 6px;
  background: none;
  border: none;
  color: var(--text-color);
  font-weight: 700;
  font-size: larger;
  text-align: left;
  padding: 4px 0;
}

.chevron {
  display: inline-block;
  transition: transform 0.15s;
  color: var(--text-secondary);
  font-size: 14px;
}

.chevron.open {
  transform: rotate(90deg);
}

.icon-btn {
  background: none;
  border: none;
  padding: 4px 6px;
}

.add-category {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 8px;
}

.add-category button {
  padding: 10px;
  background: var(--accent-color);
  border: none;
  border-radius: 8px;
  color: #fff;
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

.modal-content input {
  width: 100%;
  margin-top: 8px;
}

/* карточки-«стекло»: размытие фона под .category (класс неоднозначный —
   правило scoped, чтобы не задеть одноимённые не-карточки) */
:root[data-card-glass] .category {
  backdrop-filter: blur(var(--card-blur, 0px));
  -webkit-backdrop-filter: blur(var(--card-blur, 0px));
}
</style>
