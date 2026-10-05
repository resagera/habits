<script setup lang="ts">
// Индивидуальные настройки страницы «Дневник»: экспорт записей за период.
// Открывается из шестерёнки в шапке (см. PageSettingsModal).
import { ref } from 'vue'
import { showToast } from '../../shared/toast'
import { fetchEntries } from './api'
import type { DiaryEntry } from './types'

const exportFrom = ref('')
const exportTo = ref('')
const exporting = ref(false)

async function exportDiary(type: 'txt' | 'csv') {
  if (!exportFrom.value || !exportTo.value) {
    showToast('Выберите период 📅')
    return
  }
  exporting.value = true
  try {
    const { entries } = await fetchEntries({
      from: exportFrom.value,
      to: exportTo.value,
      limit: 500,
    })
    if (entries.length === 0) {
      showToast('За период записей нет')
      return
    }
    const content = type === 'txt' ? toTxt(entries) : toCsv(entries)
    const blob = new Blob(['﻿' + content], {
      type: type === 'txt' ? 'text/plain;charset=utf-8' : 'text/csv;charset=utf-8',
    })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `diary_${exportFrom.value}_${exportTo.value}.${type}`
    a.click()
    URL.revokeObjectURL(url)
    showToast('Файл выгружен ✅')
  } catch {
    showToast('Ошибка при экспорте')
  } finally {
    exporting.value = false
  }
}

function fmt(at: string): string {
  return new Date(at).toLocaleString('ru-RU')
}

function toTxt(entries: DiaryEntry[]): string {
  return entries
    .slice()
    .reverse()
    .map((e) => `${fmt(e.at)}\n${e.text}`)
    .join('\n\n---\n\n')
}

function toCsv(entries: DiaryEntry[]): string {
  const esc = (s: string) => `"${s.replaceAll('"', '""')}"`
  const rows = entries
    .slice()
    .reverse()
    .map((e) => `${esc(fmt(e.at))};${esc(e.text)}`)
  return 'date;text\n' + rows.join('\n')
}
</script>

<template>
  <section class="section">
    <h3>Экспорт записей</h3>
    <div class="date-range">
      <input v-model="exportFrom" type="date" />
      <input v-model="exportTo" type="date" />
    </div>
    <div class="row">
      <button class="btn" :disabled="exporting" @click="exportDiary('txt')">Экспорт .txt</button>
      <button class="btn" :disabled="exporting" @click="exportDiary('csv')">Экспорт .csv</button>
    </div>
  </section>
</template>

<style scoped>
.section {
  background: var(--card-color);
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 14px;
}

.section h3 {
  margin: 0 0 10px;
  font-size: 16px;
}

.date-range {
  display: flex;
  gap: 8px;
  margin-bottom: 8px;
}

.date-range input {
  flex: 1;
  min-width: 0;
}

.row {
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

.btn:disabled {
  opacity: 0.5;
}
</style>
