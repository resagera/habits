<script setup lang="ts">
import { inject } from 'vue'
import { linksHandlersKey } from '../keys'
import type { LinkItem } from '../types'

defineProps<{ link: LinkItem }>()
const h = inject(linksHandlersKey)!
</script>

<template>
  <div class="link-row">
    <button class="link-main" @click="h.openLink(link)">
      <span class="link-name">
        {{ link.pinned ? '⭐ ' : '' }}{{ link.name }}
        <span v-if="link.dead" class="dead-badge" title="Ссылка не отвечает (проверка битых ссылок)">💀</span>
      </span>
      <span v-if="link.tags.length" class="link-tags">
        <span v-for="tag in link.tags" :key="tag" class="tag">#{{ tag }}</span>
      </span>
    </button>
    <span class="link-actions">
      <button class="icon-btn" title="Копировать URL" @click="h.copyLink(link)">📋</button>
      <button class="icon-btn" title="Поделиться" @click="h.shareLink(link)">📤</button>
      <button class="icon-btn" title="Редактировать" @click="h.editLink(link)">✏️</button>
    </span>
  </div>
</template>

<style scoped>
.link-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  padding: 5px 0;
}

.link-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  background: none;
  border: none;
  color: var(--text-color);
  text-align: left;
  padding: 0;
}

.link-name {
  overflow-wrap: anywhere;
}

.dead-badge {
  font-size: 12px;
  opacity: 0.85;
}

.link-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.tag {
  font-size: 11px;
  color: var(--accent-color);
}

.link-actions {
  display: flex;
  flex: none;
}

.icon-btn {
  background: none;
  border: none;
  padding: 3px 5px;
}
</style>
