<script setup lang="ts">
import type { Category, MarkInfo } from '../types'
import WeekGrid from './WeekGrid.vue'

defineProps<{
  category: Category
  markInfo: (day: string) => MarkInfo | undefined
  collapsed: boolean
  activeColor: string
  activeEmoji: string
}>()

const emit = defineEmits<{
  toggle: [day: string]
  increment: [day: string, delta: 1 | -1]
  openCalendar: []
  openSettings: []
  openColorPicker: []
  openEmojiPicker: []
  toggleCollapse: []
}>()
</script>

<template>
  <div class="category">
    <div class="category-name">
      <button class="collapse-btn" @click="emit('toggleCollapse')">
        <span class="chevron" :class="{ open: !collapsed }">▸</span>
        {{ category.name }}
        <span v-if="category.shared" class="shared-badge" title="Совместный трекер">👥</span>
      </button>
      <div class="category-buttons">
        <button
          v-if="category.multi && category.style !== 'emoji' && category.kind !== 'counter'"
          class="active-swatch"
          title="Активный цвет"
          @click="emit('openColorPicker')"
        >
          <span class="swatch-dot" :style="{ backgroundColor: activeColor }"></span>
        </button>
        <button
          v-if="category.multi && category.style === 'emoji' && category.kind !== 'counter'"
          title="Активный эмодзи"
          @click="emit('openEmojiPicker')"
        >
          {{ activeEmoji }}
        </button>
        <button title="Календарь" @click="emit('openCalendar')">📅</button>
        <button title="Настройки" @click="emit('openSettings')">⚙️</button>
      </div>
    </div>
    <WeekGrid
      v-if="!collapsed"
      :category="category"
      :mark-info="markInfo"
      @toggle="(day) => emit('toggle', day)"
      @increment="(day, delta) => emit('increment', day, delta)"
    />
  </div>
</template>

<style scoped>
.category {
  margin-bottom: 24px;
  background: var(--card-color);
  padding: 10px;
  border-radius: 8px;
}

.category-name {
  font-weight: 700;
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin: 0 0 8px;
  font-size: larger;
}

.category-buttons {
  display: flex;
  align-items: center;
}

.category-buttons button {
  background: none;
  border: none;
  padding: 4px 8px;
  font-size: 16px;
}

.active-swatch {
  display: flex;
  align-items: center;
}

.swatch-dot {
  display: inline-block;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  border: 2px solid var(--text-secondary);
}

.shared-badge {
  font-size: 14px;
  font-weight: 400;
}

.collapse-btn {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 6px;
  background: none;
  border: none;
  color: var(--text-color);
  font-weight: 700;
  font-size: inherit;
  text-align: left;
  padding: 0;
}

.chevron {
  display: inline-block;
  transition: transform 0.15s;
  color: var(--text-secondary);
  font-size: 13px;
}

.chevron.open {
  transform: rotate(90deg);
}

/* карточки-«стекло»: размытие фона под .category (класс неоднозначный —
   правило scoped, чтобы не задеть одноимённые не-карточки) */
:root[data-card-glass] .category {
  backdrop-filter: blur(var(--card-blur, 0px));
  -webkit-backdrop-filter: blur(var(--card-blur, 0px));
}
</style>
