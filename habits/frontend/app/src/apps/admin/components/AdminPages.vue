<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../../../shared/api/client'
import { showToast } from '../../../shared/toast'

interface AccessUser {
  id: number
  username: string
  first_name: string
}

interface AdminFeature {
  code: string
  page: string
  title: string
  users: AccessUser[]
}

interface AdminPage {
  code: string
  title: string
  icon: string
  visibility: 'all' | 'personal'
  users: AccessUser[]
  features: AdminFeature[]
}

const pages = ref<AdminPage[]>([])
const loading = ref(true)
const expanded = ref(new Set<string>())

// единый поиск пользователя: ключ = 'page:<code>' | 'feature:<code>'
const pickerFor = ref('')
const pickerQ = ref('')
const pickerResults = ref<AccessUser[]>([])
let pickerTimer: ReturnType<typeof setTimeout> | undefined

onMounted(load)

async function load() {
  try {
    pages.value = (await api.get<{ pages: AdminPage[] }>('/admin/pages')).pages
  } catch {
    showToast('Не удалось загрузить страницы')
  } finally {
    loading.value = false
  }
}

function toggle(code: string) {
  if (expanded.value.has(code)) expanded.value.delete(code)
  else expanded.value.add(code)
}

async function setVisibility(page: AdminPage, visibility: 'all' | 'personal') {
  const prev = page.visibility
  page.visibility = visibility
  try {
    await api.put(`/admin/pages/${page.code}`, { visibility })
  } catch {
    page.visibility = prev
    showToast('Не удалось сохранить')
  }
}

function openPicker(key: string) {
  pickerFor.value = pickerFor.value === key ? '' : key
  pickerQ.value = ''
  pickerResults.value = []
}

function onPickerInput() {
  clearTimeout(pickerTimer)
  const q = pickerQ.value.trim()
  if (!q) {
    pickerResults.value = []
    return
  }
  pickerTimer = setTimeout(async () => {
    try {
      pickerResults.value = (
        await api.get<{ users: AccessUser[] }>(`/admin/users/search?q=${encodeURIComponent(q)}`)
      ).users
    } catch {
      pickerResults.value = []
    }
  }, 300)
}

function userLabel(u: AccessUser): string {
  const parts = [`#${u.id}`]
  if (u.username) parts.push('@' + u.username)
  if (u.first_name) parts.push(u.first_name)
  return parts.join(' ')
}

async function grant(user: AccessUser) {
  const [kind, code] = pickerFor.value.split(':', 2)
  const url =
    kind === 'page' ? `/admin/pages/${code}/access` : `/admin/features/${code}/access`
  try {
    await api.post(url, { user_id: user.id })
    const list = targetList(kind, code)
    if (list && !list.some((u) => u.id === user.id)) list.push(user)
    pickerQ.value = ''
    pickerResults.value = []
  } catch {
    showToast('Не удалось выдать доступ')
  }
}

async function revoke(kind: string, code: string, user: AccessUser) {
  const url =
    kind === 'page'
      ? `/admin/pages/${code}/access/${user.id}`
      : `/admin/features/${code}/access/${user.id}`
  try {
    await api.delete(url)
    const list = targetList(kind, code)
    if (list) {
      const i = list.findIndex((u) => u.id === user.id)
      if (i >= 0) list.splice(i, 1)
    }
  } catch {
    showToast('Не удалось отозвать доступ')
  }
}

function targetList(kind: string, code: string): AccessUser[] | null {
  if (kind === 'page') return pages.value.find((p) => p.code === code)?.users ?? null
  for (const p of pages.value) {
    const f = p.features.find((x) => x.code === code)
    if (f) return f.users
  }
  return null
}
</script>

<template>
  <section class="admin-pages">
    <h3>📄 Доступ к страницам</h3>
    <p class="hint-line">
      «Видна всем» — страница доступна каждому. «Персональный доступ» — только
      выбранным пользователям (админы видят всё всегда).
    </p>

    <p v-if="loading" class="hint-line">Загрузка…</p>

    <div v-for="p in pages" :key="p.code" class="page-row">
      <button class="page-head" @click="toggle(p.code)">
        <span class="chevron" :class="{ open: expanded.has(p.code) }">▸</span>
        <span class="page-name">{{ p.icon }} {{ p.title }}</span>
        <span class="page-vis" :class="p.visibility">
          {{ p.visibility === 'all' ? 'видна всем' : `персонально (${p.users.length})` }}
        </span>
      </button>

      <div v-if="expanded.has(p.code)" class="page-body">
        <div class="vis-switch">
          <button :class="{ on: p.visibility === 'all' }" @click="setVisibility(p, 'all')">Видна всем</button>
          <button :class="{ on: p.visibility === 'personal' }" @click="setVisibility(p, 'personal')">
            Персональный доступ
          </button>
        </div>

        <template v-if="p.visibility === 'personal'">
          <div class="grants">
            <span v-for="u in p.users" :key="u.id" class="chip">
              {{ userLabel(u) }}
              <button class="chip-x" @click="revoke('page', p.code, u)">✕</button>
            </span>
            <button class="chip add" @click="openPicker(`page:${p.code}`)">＋ пользователь</button>
          </div>
          <div v-if="pickerFor === `page:${p.code}`" class="picker">
            <input
              v-model="pickerQ"
              placeholder="id, @логин или имя…"
              @input="onPickerInput"
            />
            <button v-for="u in pickerResults" :key="u.id" class="picker-hit" @click="grant(u)">
              {{ userLabel(u) }}
            </button>
          </div>
        </template>

        <div v-for="f in p.features" :key="f.code" class="feature">
          <div class="feature-title">🔧 {{ f.title }} <span class="feature-note">— отдельный доступ</span></div>
          <div class="grants">
            <span v-for="u in f.users" :key="u.id" class="chip">
              {{ userLabel(u) }}
              <button class="chip-x" @click="revoke('feature', f.code, u)">✕</button>
            </span>
            <button class="chip add" @click="openPicker(`feature:${f.code}`)">＋ пользователь</button>
          </div>
          <div v-if="pickerFor === `feature:${f.code}`" class="picker">
            <input
              v-model="pickerQ"
              placeholder="id, @логин или имя…"
              @input="onPickerInput"
            />
            <button v-for="u in pickerResults" :key="u.id" class="picker-hit" @click="grant(u)">
              {{ userLabel(u) }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.admin-pages {
  background: var(--card-color);
  border-radius: 12px;
  padding: 14px;
  margin-top: 16px;
}

.admin-pages h3 {
  margin: 0 0 4px;
}

.hint-line {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 10px;
}

.page-row {
  border-top: 1px solid var(--bg-secondary);
}

.page-head {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 9px 0;
  background: none;
  border: none;
  color: var(--text-color);
  font-size: 14px;
  text-align: left;
}

.page-name {
  flex: 1;
  font-weight: 600;
}

.page-vis {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 10px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
}

.page-vis.personal {
  background: rgba(245, 158, 11, 0.18);
  color: #f59e0b;
}

.chevron {
  display: inline-block;
  transition: transform 0.15s;
  color: var(--text-secondary);
}

.chevron.open {
  transform: rotate(90deg);
}

.page-body {
  padding: 0 0 12px 20px;
}

.vis-switch {
  display: flex;
  gap: 6px;
  margin-bottom: 8px;
}

.vis-switch button {
  flex: 1;
  padding: 7px 4px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: 12px;
}

.vis-switch button.on {
  background: var(--accent-color);
  color: #fff;
}

.grants {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: var(--bg-secondary);
  border-radius: 12px;
  padding: 4px 10px;
  font-size: 12px;
}

.chip-x {
  background: none;
  border: none;
  color: var(--text-secondary);
  padding: 0 0 0 2px;
  font-size: 11px;
}

.chip.add {
  border: 1px dashed var(--text-secondary);
  background: none;
  color: var(--text-secondary);
}

.picker {
  margin-top: 8px;
}

.picker input {
  width: 100%;
}

.picker-hit {
  display: block;
  width: 100%;
  text-align: left;
  padding: 8px 10px;
  margin-top: 4px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
  font-size: 13px;
}

.feature {
  margin-top: 12px;
}

.feature-title {
  font-size: 13px;
  font-weight: 600;
  margin-bottom: 6px;
}

.feature-note {
  font-weight: 400;
  font-size: 11px;
  color: var(--text-secondary);
}
</style>
