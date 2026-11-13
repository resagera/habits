<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { showToast } from '../../shared/toast'
import * as diaryApi from './api'
import type { DiaryEntry } from './types'

const entries = ref<DiaryEntry[]>([])
const loading = ref(true)

const entryAt = ref(toLocalInput(new Date()))
const entryText = ref('')
const editingId = ref<number | null>(null)

const search = ref('')
const filterFrom = ref('')
const filterTo = ref('')
const confirmDeleteId = ref<number | null>(null)

// длинные записи в списке укорачиваются; разворачиваются кнопкой
const expanded = ref(new Set<number>())

function isLong(entry: DiaryEntry): boolean {
  return entry.text.length > 400 || entry.text.split('\n').length > 6
}

function toggleExpand(id: number) {
  if (expanded.value.has(id)) expanded.value.delete(id)
  else expanded.value.add(id)
  expanded.value = new Set(expanded.value)
}

let searchTimer: ReturnType<typeof setTimeout> | undefined

onMounted(load)

/** Date -> значение для input[type=datetime-local] (локальное время). */
function toLocalInput(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function formatAt(at: string): string {
  return new Date(at).toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

async function load() {
  try {
    const { entries: list } = await diaryApi.fetchEntries({
      q: search.value.trim() || undefined,
      from: filterFrom.value || undefined,
      to: filterTo.value || undefined,
    })
    entries.value = list
  } catch {
    showToast('Не удалось загрузить записи')
  } finally {
    loading.value = false
  }
}

function onSearchInput() {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(load, 300)
}

async function save() {
  const text = entryText.value.trim()
  if (!text) {
    showToast('Введите текст записи')
    return
  }
  const at = new Date(entryAt.value).toISOString()
  try {
    if (editingId.value) {
      await diaryApi.updateEntry(editingId.value, { at, text })
      showToast('Запись обновлена ✅')
    } else {
      await diaryApi.createEntry(at, text)
      showToast('Запись добавлена ✅')
    }
    cancelEdit()
    await load()
  } catch {
    showToast('Не удалось сохранить')
  }
}

function startEdit(entry: DiaryEntry) {
  editingId.value = entry.id
  entryAt.value = toLocalInput(new Date(entry.at))
  entryText.value = entry.text
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

function cancelEdit() {
  editingId.value = null
  entryText.value = ''
  entryAt.value = toLocalInput(new Date())
}

async function remove(id: number) {
  if (confirmDeleteId.value !== id) {
    confirmDeleteId.value = id
    setTimeout(() => {
      if (confirmDeleteId.value === id) confirmDeleteId.value = null
    }, 3000)
    return
  }
  confirmDeleteId.value = null
  try {
    await diaryApi.deleteEntry(id)
    entries.value = entries.value.filter((e) => e.id !== id)
    if (editingId.value === id) cancelEdit()
    showToast('Запись удалена 🗑')
  } catch {
    showToast('Не удалось удалить')
  }
}
</script>

<template>
  <form class="entry-form" @submit.prevent="save">
    <input v-model="entryAt" type="datetime-local" />
    <textarea
      v-model="entryText"
      rows="4"
      placeholder="Напиши, что произошло сегодня…"
    ></textarea>
    <div class="form-actions">
      <button type="submit" class="btn primary">
        {{ editingId ? 'Обновить' : 'Сохранить' }}
      </button>
      <button v-if="editingId" type="button" class="btn" @click="cancelEdit">Отмена</button>
    </div>
  </form>

  <div class="filters">
    <input
      v-model="search"
      type="text"
      placeholder="Поиск по записям…"
      @input="onSearchInput"
    />
    <div class="date-range">
      <input v-model="filterFrom" type="date" @change="load" />
      <input v-model="filterTo" type="date" @change="load" />
    </div>
  </div>

  <div v-if="loading" class="hint">Загрузка…</div>
  <div v-else-if="entries.length === 0" class="hint">Записей нет</div>

  <div v-for="entry in entries" :key="entry.id" class="entry">
    <div class="entry-head">
      <span class="entry-date">{{ formatAt(entry.at) }}</span>
      <span class="entry-actions">
        <button class="icon-btn" title="Редактировать" @click="startEdit(entry)">✏️</button>
        <button
          class="icon-btn"
          :class="{ confirming: confirmDeleteId === entry.id }"
          @click="remove(entry.id)"
        >
          {{ confirmDeleteId === entry.id ? 'точно?' : '🗑' }}
        </button>
      </span>
    </div>
    <div class="entry-text" :class="{ clamped: isLong(entry) && !expanded.has(entry.id) }">{{ entry.text }}</div>
    <button v-if="isLong(entry)" class="expand-btn" @click="toggleExpand(entry.id)">
      {{ expanded.has(entry.id) ? '▴ Свернуть' : '▾ Развернуть' }}
    </button>
  </div>
</template>

<style scoped>
.entry-form {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 16px;
}

textarea {
  font: inherit;
  background: var(--bg-secondary);
  color: var(--text-color);
  border: 1px solid var(--hover-bg-color);
  border-radius: 6px;
  padding: 8px;
  resize: vertical;
}

.form-actions {
  display: flex;
  gap: 8px;
}

.btn {
  flex: 1;
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

.filters {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 16px;
}

.date-range {
  display: flex;
  gap: 8px;
}

.date-range input {
  flex: 1;
  min-width: 0;
}

.hint {
  text-align: center;
  color: var(--text-secondary);
  padding: 24px 0;
}

.entry {
  background: var(--card-color);
  border-radius: 8px;
  padding: 10px 12px;
  margin-bottom: 10px;
}

.entry-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 4px;
}

.entry-date {
  font-size: 13px;
  color: var(--text-secondary);
}

.icon-btn {
  background: none;
  border: none;
  padding: 2px 6px;
}

.icon-btn.confirming {
  color: #ef4444;
  font-weight: 600;
}

.entry-text {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.entry-text.clamped {
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 6;
  line-clamp: 6;
  overflow: hidden;
}

.expand-btn {
  display: block;
  margin-top: 4px;
  padding: 4px 0;
  background: none;
  border: none;
  color: var(--accent-color);
  font-size: 12px;
}

/* карточки-«стекло»: размытие фона под .entry (класс неоднозначный —
   правило scoped, чтобы не задеть одноимённые не-карточки) */
:root[data-card-glass] .entry {
  backdrop-filter: blur(var(--card-blur, 0px));
  -webkit-backdrop-filter: blur(var(--card-blur, 0px));
}
</style>
