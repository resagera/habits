<script setup lang="ts">
import type { VaultEntry, VaultFolder } from '../crypto'
import PassEntryRow from './PassEntryRow.vue'

const props = defineProps<{
  folder: VaultFolder
  folders: VaultFolder[]
  entries: VaultEntry[]
  collapsed: Set<number>
  revealedId: number | null
  level: number
}>()

const emit = defineEmits<{
  toggle: [id: number]
  addEntry: [folderId: number]
  editFolder: [folder: VaultFolder]
  editEntry: [entry: VaultEntry]
  copyLogin: [entry: VaultEntry]
  copyPassword: [entry: VaultEntry]
  reveal: [id: number]
  openUrl: [entry: VaultEntry]
}>()

function sortFolders(list: VaultFolder[]): VaultFolder[] {
  return [...list].sort((a, b) => Number(b.pinned ?? false) - Number(a.pinned ?? false) || a.id - b.id)
}

function children(): VaultFolder[] {
  return sortFolders(props.folders.filter((f) => f.parent_id === props.folder.id))
}

function items(): VaultEntry[] {
  return props.entries.filter((e) => e.folder_id === props.folder.id)
}

function totalCount(): number {
  // записи в папке и всех вложенных
  const ids = new Set<number>([props.folder.id])
  let grew = true
  while (grew) {
    grew = false
    for (const f of props.folders) {
      if (f.parent_id !== null && ids.has(f.parent_id) && !ids.has(f.id)) {
        ids.add(f.id)
        grew = true
      }
    }
  }
  return props.entries.filter((e) => e.folder_id !== null && ids.has(e.folder_id)).length
}
</script>

<template>
  <div class="pass-folder" :class="{ nested: level > 0 }">
    <div class="folder-head">
      <button class="folder-toggle" @click="emit('toggle', folder.id)">
        <span class="chevron" :class="{ open: !collapsed.has(folder.id) }">▸</span>
        {{ folder.pinned ? '📌' : '📂' }} {{ folder.name }}
        <span class="count">{{ totalCount() }}</span>
      </button>
      <span>
        <button class="icon-btn" title="Добавить сюда" @click="emit('addEntry', folder.id)">＋</button>
        <button class="icon-btn" title="Настройки папки" @click="emit('editFolder', folder)">⚙️</button>
      </span>
    </div>

    <div v-if="!collapsed.has(folder.id)" class="folder-body">
      <PassFolderNode
        v-for="child in children()"
        :key="child.id"
        :folder="child"
        :folders="folders"
        :entries="entries"
        :collapsed="collapsed"
        :revealed-id="revealedId"
        :level="level + 1"
        @toggle="emit('toggle', $event)"
        @add-entry="emit('addEntry', $event)"
        @edit-folder="emit('editFolder', $event)"
        @edit-entry="emit('editEntry', $event)"
        @copy-login="emit('copyLogin', $event)"
        @copy-password="emit('copyPassword', $event)"
        @reveal="emit('reveal', $event)"
        @open-url="emit('openUrl', $event)"
      />
      <PassEntryRow
        v-for="entry in items()"
        :key="entry.id"
        :entry="entry"
        :revealed="revealedId === entry.id"
        @copy-login="emit('copyLogin', entry)"
        @copy-password="emit('copyPassword', entry)"
        @toggle-reveal="emit('reveal', entry.id)"
        @edit="emit('editEntry', entry)"
        @open-url="emit('openUrl', entry)"
      />
      <p v-if="items().length === 0 && children().length === 0" class="hint-empty">пусто</p>
    </div>
  </div>
</template>

<style scoped>
.pass-folder {
  background: var(--card-color);
  border-radius: 8px;
  padding: 6px 10px;
  margin-bottom: 8px;
}

.pass-folder.nested {
  background: none;
  padding: 0;
  margin-bottom: 4px;
}

.folder-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
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
  padding: 4px 0;
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
  margin-left: 8px;
  padding-left: 8px;
  border-left: 1px solid var(--hover-bg-color);
}

.folder-body :deep(.pass-item) {
  background: var(--bg-secondary);
  padding: 8px 10px;
}

.icon-btn {
  background: none;
  border: none;
  padding: 4px 6px;
}

.hint-empty {
  padding: 4px 0;
  font-size: 12px;
  color: var(--text-secondary);
}
</style>
