<script setup lang="ts">
import { computed, inject } from 'vue'
import { linksHandlersKey } from '../keys'
import type { LinkFolder, LinkItem } from '../types'
import LinkRow from './LinkRow.vue'

const props = defineProps<{
  folder: LinkFolder
  folders: LinkFolder[]
  links: LinkItem[]
  root?: boolean
}>()

const h = inject(linksHandlersKey)!

const children = computed(() => props.folders.filter((f) => f.parent_id === props.folder.id))
const folderLinks = computed(() => props.links.filter((l) => l.folder_id === props.folder.id))
const open = computed(() => h.isOpen(props.folder.id))
const count = computed(() => folderLinks.value.length)
</script>

<template>
  <div class="folder" :class="{ card: root, 'card-glass': root }">
    <div class="folder-head">
      <button class="folder-toggle" @click="h.toggleFolder(folder.id)">
        <span class="chevron" :class="{ open }">▸</span>
        <span>📂 {{ folder.name }}</span>
        <span class="count">{{ count }}</span>
      </button>
      <span>
        <button class="icon-btn" title="Добавить ссылку сюда" @click="h.addLinkTo(folder.id)">＋</button>
        <button class="icon-btn" title="Настройки папки" @click="h.editFolder(folder)">⚙️</button>
      </span>
    </div>

    <div v-if="open" class="folder-body">
      <FolderNode
        v-for="child in children"
        :key="child.id"
        :folder="child"
        :folders="folders"
        :links="links"
      />
      <LinkRow v-for="link in folderLinks" :key="link.id" :link="link" />
      <p v-if="children.length === 0 && folderLinks.length === 0" class="empty">пусто</p>
    </div>
  </div>
</template>

<style scoped>
/* корневая папка — карточка на подложке, как группы на остальных страницах;
   blur при полупрозрачности даёт глобальный класс .card-glass (см. theme.css) */
.folder.card {
  background: var(--card-color);
  border-radius: 8px;
  padding: 6px 12px;
  margin-bottom: 10px;
}

.folder-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 0;
}

.folder-toggle {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 6px;
  background: none;
  border: none;
  color: var(--text-color);
  font-weight: 600;
  text-align: left;
  padding: 2px 0;
}

.chevron {
  display: inline-block;
  transition: transform 0.15s;
  color: var(--text-secondary);
}

.chevron.open {
  transform: rotate(90deg);
}

.count {
  font-size: 11px;
  color: var(--text-secondary);
}

.folder-body {
  margin-left: 14px;
  padding-left: 8px;
  border-left: 1px solid var(--hover-bg-color);
}

.icon-btn {
  background: none;
  border: none;
  padding: 3px 5px;
}

.empty {
  margin: 2px 0 6px;
  font-size: 12px;
  color: var(--text-secondary);
}
</style>
