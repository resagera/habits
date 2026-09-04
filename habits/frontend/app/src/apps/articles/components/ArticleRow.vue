<script setup lang="ts">
import { fmtDate, type ArticleMeta } from '../types'

defineProps<{ article: ArticleMeta }>()

const emit = defineEmits<{
  open: []
  menu: [ev: MouseEvent]
}>()
</script>

<template>
  <div class="article-row">
    <button class="article-main" @click="emit('open')">
      <span class="article-title">📄 {{ article.title }}</span>
      <span class="article-date">{{ fmtDate(article.updated_at) }}</span>
    </button>
    <button class="menu-btn" title="Меню" @click.stop="emit('menu', $event)">⋮</button>
  </div>
</template>

<style scoped>
.article-row {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 3px 0;
}

.article-main {
  flex: 1;
  min-width: 0;
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 8px;
  background: none;
  border: none;
  color: var(--text-color);
  text-align: left;
  padding: 5px 0;
}

.article-title {
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: 14px;
}

.article-date {
  flex: none;
  font-size: 11px;
  color: var(--text-secondary);
}

.menu-btn {
  flex: none;
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 18px;
  font-weight: 700;
  padding: 2px 8px;
}
</style>
