<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../../shared/api/client'
import { loadCollapsed, saveCollapsed } from '../../shared/collapsed'
import type { Category, CategoryPatch } from './types'
import { useTracker } from './composables/useTracker'
import CategoryCard from './components/CategoryCard.vue'
import ColorPickerPopover from './components/ColorPickerPopover.vue'
import EmojiPickerPopover from './components/EmojiPickerPopover.vue'
import HistoryScreen from './components/HistoryScreen.vue'
import MonthCalendarModal from './components/MonthCalendarModal.vue'
import CategorySettingsModal from './components/CategorySettingsModal.vue'

const tracker = useTracker()
const newCategoryName = ref('')
const myId = ref(0)

// свёрнутые категории — состояние на сервере
const collapsed = ref(new Set<number>())

function toggleCollapse(id: number) {
  if (collapsed.value.has(id)) collapsed.value.delete(id)
  else collapsed.value.add(id)
  collapsed.value = new Set(collapsed.value)
  saveCollapsed('tracker', collapsed.value)
}

const calendarCategory = ref<Category | null>(null)
const settingsCategory = ref<Category | null>(null)
const colorPickerCategory = ref<Category | null>(null)
const emojiPickerCategory = ref<Category | null>(null)
const historyCategory = ref<Category | null>(null)

onMounted(async () => {
  loadCollapsed('tracker').then((s) => (collapsed.value = s))
  api.get<{ id: number }>('/me').then((me) => (myId.value = me.id)).catch(() => {})
  await tracker.load()
})

async function onAddCategory() {
  const name = newCategoryName.value.trim()
  if (!name) return
  if (await tracker.addCategory(name)) {
    newCategoryName.value = ''
  }
}

async function onSaveSettings(patch: CategoryPatch) {
  const category = settingsCategory.value
  if (!category) return
  if (await tracker.patchCategory(category.id, patch)) {
    settingsCategory.value = null
  }
}

async function onRemoveCategory() {
  const category = settingsCategory.value
  if (!category) return
  if (await tracker.removeCategory(category.id)) {
    settingsCategory.value = null
  }
}

async function onLeaveCategory() {
  const category = settingsCategory.value
  if (!category || !myId.value) return
  if (await tracker.leaveCategory(category, myId.value)) {
    settingsCategory.value = null
  }
}

function openHistoryFromSettings() {
  historyCategory.value = settingsCategory.value
  settingsCategory.value = null
}
</script>

<template>
  <div v-if="tracker.loading.value" class="loading">Загрузка…</div>

  <template v-else>
    <p v-if="tracker.categories.value.length === 0" class="empty">
      Пока нет ни одной категории — добавьте первую 👇
    </p>

    <CategoryCard
      v-for="category in tracker.categories.value"
      :key="category.id"
      :category="category"
      :mark-info="(day) => tracker.markInfo(category.id, day)"
      :collapsed="collapsed.has(category.id)"
      :active-color="tracker.activeColor(category)"
      :active-emoji="tracker.activeEmoji(category)"
      @toggle="(day) => tracker.toggle(category, day)"
      @increment="(day, delta) => tracker.increment(category, day, delta)"
      @toggle-collapse="toggleCollapse(category.id)"
      @open-calendar="calendarCategory = category"
      @open-settings="settingsCategory = category"
      @open-color-picker="colorPickerCategory = category"
      @open-emoji-picker="emojiPickerCategory = category"
    />

    <form class="add-category" @submit.prevent="onAddCategory">
      <input v-model="newCategoryName" placeholder="Новая категория…" maxlength="100" />
      <button type="submit">Добавить категорию</button>
    </form>
  </template>

  <MonthCalendarModal
    v-if="calendarCategory"
    :category="calendarCategory"
    :tracker="tracker"
    @close="calendarCategory = null"
  />

  <ColorPickerPopover
    v-if="colorPickerCategory"
    :current="tracker.activeColor(colorPickerCategory)"
    :recent="tracker.recentColors.value"
    @pick="(c) => tracker.setActiveColor(colorPickerCategory!, c)"
    @close="colorPickerCategory = null"
  />

  <EmojiPickerPopover
    v-if="emojiPickerCategory"
    :current="tracker.activeEmoji(emojiPickerCategory)"
    :recent="tracker.recentEmoji.value"
    @pick="(e) => tracker.setActiveEmoji(emojiPickerCategory!, e)"
    @close="emojiPickerCategory = null"
  />

  <HistoryScreen
    v-if="historyCategory"
    :category="historyCategory"
    :tracker="tracker"
    @close="historyCategory = null"
  />

  <CategorySettingsModal
    v-if="settingsCategory"
    :key="settingsCategory.id"
    :category="settingsCategory"
    @save="onSaveSettings"
    @remove="onRemoveCategory"
    @leave="onLeaveCategory"
    @open-history="openHistoryFromSettings"
    @close="settingsCategory = null"
  />
</template>

<style scoped>
.loading,
.empty {
  text-align: center;
  color: var(--text-secondary);
  padding: 32px 0;
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
</style>
