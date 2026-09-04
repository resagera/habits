<script setup lang="ts">
// Журнал релизов. Пока страница доступна только админам (роут meta.admin), но
// вёрстка и API уже разделяют публичное (видно всем) и техническое (только
// админ) — чтобы позже открыть страницу пользователям без изменений бэкенда.
import { onMounted, ref } from 'vue'
import { isAdmin } from '../../shared/me'
import { showToast } from '../../shared/toast'
import { fetchReleases, updateRelease } from './api'
import { RELEASE_STATUSES, STATUS_LABELS, type Release } from './types'

const releases = ref<Release[]>([])
const loading = ref(true)
const expanded = ref<Set<number>>(new Set())
const savingId = ref<number | null>(null)
// локальные правки комментария до сохранения
const commentDraft = ref<Record<number, string>>({})

onMounted(async () => {
  try {
    releases.value = (await fetchReleases()).releases
    for (const r of releases.value) commentDraft.value[r.id] = r.comment ?? ''
  } catch {
    showToast('Не удалось загрузить релизы')
  } finally {
    loading.value = false
  }
})

function toggle(id: number) {
  const s = new Set(expanded.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  expanded.value = s
}

function fmtDate(iso: string): string {
  try {
    return new Date(iso).toLocaleDateString('ru-RU')
  } catch {
    return iso.slice(0, 10)
  }
}

async function saveComment(r: Release) {
  savingId.value = r.id
  try {
    const { release } = await updateRelease(r.id, { comment: commentDraft.value[r.id] ?? '' })
    r.comment = release.comment
    showToast('Комментарий сохранён ✅')
  } catch {
    showToast('Не удалось сохранить комментарий')
  } finally {
    savingId.value = null
  }
}

async function changeStatus(r: Release, status: string) {
  savingId.value = r.id
  try {
    const { release } = await updateRelease(r.id, { status })
    r.status = release.status
    showToast('Статус обновлён ✅')
  } catch {
    showToast('Не удалось обновить статус')
  } finally {
    savingId.value = null
  }
}
</script>

<template>
  <p v-if="loading" class="hint">Загрузка…</p>
  <p v-else-if="!releases.length" class="hint">Релизов пока нет.</p>

  <div v-for="r in releases" v-else :key="r.id" class="rel">
    <button class="rel-head" @click="toggle(r.id)">
      <span class="chev">{{ expanded.has(r.id) ? '▾' : '▸' }}</span>
      <span class="ver">v{{ r.version }}</span>
      <span class="rel-title">{{ r.title }}</span>
      <span class="status" :class="'st-' + r.status">{{ STATUS_LABELS[r.status] ?? r.status }}</span>
    </button>

    <div v-if="expanded.has(r.id)" class="rel-body">
      <div class="date">📅 {{ fmtDate(r.released_on) }}</div>

      <div class="block">
        <h4>Что нового</h4>
        <p class="public">{{ r.public_notes || '—' }}</p>
      </div>

      <!-- технические подробности — только админ (API их обычным не отдаёт) -->
      <div v-if="isAdmin && r.tech_notes" class="block tech">
        <h4>🛠 Технические подробности</h4>
        <pre class="tech-text">{{ r.tech_notes }}</pre>
      </div>

      <!-- статус и комментарий админа -->
      <div v-if="isAdmin" class="block admin">
        <label class="row">
          <span>Статус:</span>
          <select :value="r.status" :disabled="savingId === r.id" @change="changeStatus(r, ($event.target as HTMLSelectElement).value)">
            <option v-for="s in RELEASE_STATUSES" :key="s" :value="s">{{ STATUS_LABELS[s] }}</option>
          </select>
        </label>
        <h4>Комментарий</h4>
        <textarea
          v-model="commentDraft[r.id]"
          rows="2"
          class="comment"
          placeholder="Заметка администратора…"
        ></textarea>
        <button class="btn" :disabled="savingId === r.id" @click="saveComment(r)">
          {{ savingId === r.id ? 'Сохранение…' : 'Сохранить комментарий' }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.hint {
  font-size: 14px;
  color: var(--text-secondary);
  padding: 8px 0;
}

.rel {
  background: var(--card-color);
  border-radius: 8px;
  margin-bottom: 10px;
  overflow: hidden;
}

.rel-head {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 12px 14px;
  background: none;
  border: none;
  color: var(--text-color);
  text-align: left;
}

.chev {
  color: var(--text-secondary);
  flex: none;
}

.ver {
  font-weight: 600;
  color: var(--accent-color);
  flex: none;
}

.rel-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 14px;
}

.status {
  flex: none;
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
}

.st-released {
  background: #16653420;
  color: #22c55e;
}

.st-rolled_back {
  background: #7f1d1d20;
  color: #ef4444;
}

.st-deprecated {
  background: #78350f20;
  color: #f59e0b;
}

.rel-body {
  padding: 4px 14px 14px;
}

.date {
  font-size: 12px;
  color: var(--text-secondary);
  margin-bottom: 8px;
}

.block {
  margin-top: 10px;
}

.block h4 {
  margin: 0 0 6px;
  font-size: 13px;
  color: var(--text-secondary);
}

.public {
  margin: 0;
  font-size: 14px;
  line-height: 1.45;
}

.tech-text {
  margin: 0;
  font-size: 12px;
  line-height: 1.4;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: ui-monospace, monospace;
  background: var(--bg-secondary);
  padding: 8px 10px;
  border-radius: 6px;
}

.admin {
  border-top: 1px solid var(--hover-bg-color);
  padding-top: 10px;
}

.row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  font-size: 13px;
}

.comment {
  width: 100%;
  font: inherit;
  background: var(--bg-secondary);
  color: var(--text-color);
  border: 1px solid var(--hover-bg-color);
  border-radius: 6px;
  padding: 8px;
  margin-bottom: 8px;
  resize: vertical;
}

.btn {
  padding: 8px 12px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
}

.btn:disabled {
  opacity: 0.5;
}
</style>
