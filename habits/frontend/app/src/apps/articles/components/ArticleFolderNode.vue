<script setup lang="ts">
import type { ArticleFolder, ArticleMeta } from '../types'
import ArticleRow from './ArticleRow.vue'

const props = defineProps<{
  folder: ArticleFolder
  folders: ArticleFolder[]
  articles: ArticleMeta[]
  openSet: Set<number>
  level: number
}>()

const emit = defineEmits<{
  toggle: [id: number]
  editFolder: [folder: ArticleFolder]
  addTo: [folderId: number]
  open: [article: ArticleMeta]
  menu: [article: ArticleMeta, ev: MouseEvent]
}>()

function children(): ArticleFolder[] {
  return props.folders.filter((f) => f.parent_id === props.folder.id)
}

function items(): ArticleMeta[] {
  return props.articles.filter((a) => a.folder_id === props.folder.id)
}
</script>

<template>
  <div
    class="afolder"
    :class="{ card: level === 0, 'card-glass': level === 0 }"
    :style="{ marginLeft: level > 0 ? '10px' : '0' }"
  >
    <div class="afolder-head">
      <button class="afolder-toggle" @click="emit('toggle', folder.id)">
        <span class="chevron" :class="{ open: openSet.has(folder.id) }">▸</span>
        📁 {{ folder.name }}
        <span class="count">{{ items().length }}</span>
      </button>
      <span>
        <button class="icon-btn" title="Добавить статью сюда" @click="emit('addTo', folder.id)">＋</button>
        <button class="icon-btn" title="Настройки папки" @click="emit('editFolder', folder)">⚙️</button>
      </span>
    </div>

    <div v-if="openSet.has(folder.id)" class="afolder-body">
      <ArticleFolderNode
        v-for="child in children()"
        :key="child.id"
        :folder="child"
        :folders="folders"
        :articles="articles"
        :open-set="openSet"
        :level="level + 1"
        @toggle="emit('toggle', $event)"
        @edit-folder="emit('editFolder', $event)"
        @add-to="emit('addTo', $event)"
        @open="emit('open', $event)"
        @menu="(a, e) => emit('menu', a, e)"
      />
      <ArticleRow
        v-for="a in items()"
        :key="a.id"
        :article="a"
        @open="emit('open', a)"
        @menu="(e) => emit('menu', a, e)"
      />
    </div>
  </div>
</template>

<style scoped>
/* категория верхнего уровня — карточка на подложке, как группы на остальных
   страницах; blur при полупрозрачности даёт глобальный класс .card-glass */
.afolder.card {
  background: var(--card-color);
  border-radius: 8px;
  padding: 4px 12px;
  margin-bottom: 10px;
}

.afolder-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.afolder-toggle {
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
  padding: 6px 0;
}

.count {
  font-size: 11px;
  color: var(--text-secondary);
}

.chevron {
  display: inline-block;
  transition: transform 0.15s;
  color: var(--text-secondary);
}

.chevron.open {
  transform: rotate(90deg);
}

.afolder-body {
  margin-left: 8px;
  padding-left: 8px;
  border-left: 1px solid var(--hover-bg-color);
}

.icon-btn {
  background: none;
  border: none;
  padding: 4px 6px;
}
</style>
