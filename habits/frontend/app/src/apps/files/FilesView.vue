<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { showToast } from '../../shared/toast'
import * as filesApi from './api'
import {
  fileIcon,
  fileKind,
  fmtBytes,
  rootModeOf,
  type FileEntry,
  type FileKind,
  type FileMachine,
} from './types'

const machines = ref<FileMachine[]>([])
const loading = ref(true)

// свёрнутые машины (per-machine, в localStorage)
const COLLAPSE_KEY = 'files_collapsed'
const collapsed = ref(new Set<number>(JSON.parse(localStorage.getItem(COLLAPSE_KEY) || '[]')))
function toggleCollapse(id: number) {
  if (collapsed.value.has(id)) collapsed.value.delete(id)
  else collapsed.value.add(id)
  collapsed.value = new Set(collapsed.value)
  localStorage.setItem(COLLAPSE_KEY, JSON.stringify([...collapsed.value]))
}

interface Crumb {
  label: string
  path: string
}
interface Nav {
  stack: Crumb[]
  entries: FileEntry[]
  loading: boolean
  error: string
}
const nav = reactive<Record<number, Nav>>({})

function navFor(id: number): Nav {
  if (!nav[id]) nav[id] = { stack: [{ label: '🖥 Папки', path: '' }], entries: [], loading: false, error: '' }
  return nav[id]
}

async function loadDir(m: FileMachine) {
  const n = navFor(m.id)
  const path = n.stack[n.stack.length - 1].path
  n.loading = true
  n.error = ''
  try {
    const { entries } = await filesApi.listDir(m.id, path)
    n.entries = entries
  } catch (e) {
    n.error = e instanceof Error ? e.message : 'ошибка'
    n.entries = []
  } finally {
    n.loading = false
  }
}

function openMachine(m: FileMachine) {
  if (collapsed.value.has(m.id)) toggleCollapse(m.id)
  const n = navFor(m.id)
  if (n.entries.length === 0 && !n.error) loadDir(m)
}

function enterDir(m: FileMachine, e: FileEntry) {
  const n = navFor(m.id)
  n.stack.push({ label: e.name.split('/').pop() || e.name, path: e.path })
  loadDir(m)
}

function gotoCrumb(m: FileMachine, i: number) {
  const n = navFor(m.id)
  if (i === n.stack.length - 1) return
  n.stack = n.stack.slice(0, i + 1)
  loadDir(m)
}

async function loadAll() {
  try {
    machines.value = (await filesApi.fetchMachines()).machines
    // раскрытые машины сразу подгружают текущую папку
    for (const m of machines.value) {
      if (!collapsed.value.has(m.id) && m.online) loadDir(m)
    }
  } catch {
    showToast('Не удалось загрузить машины')
  } finally {
    loading.value = false
  }
}

onMounted(loadAll)

// --- добавление / настройки машины ---

const modal = ref(false)
const editing = ref<FileMachine | null>(null)
const form = ref({ name: '' })
const confirmDelete = ref(false)

const INSTALL_PREFIX =
  'curl -fsSL https://raw.githubusercontent.com/resagera/habits-agent/main/install-files.sh | sudo bash -s -- '

function installCmd(m: FileMachine): string {
  return `${INSTALL_PREFIX}${m.token} "/home/user/media:ro;/home/user/box:rw"`
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    showToast('Скопировано')
  } catch {
    showToast('Не удалось скопировать')
  }
}

function openCreate() {
  editing.value = null
  form.value = { name: '' }
  confirmDelete.value = false
  modal.value = true
}

function openSettings(m: FileMachine) {
  editing.value = m
  form.value = { name: m.name }
  confirmDelete.value = false
  modal.value = true
}

async function save() {
  const name = form.value.name.trim()
  if (!name) {
    showToast('Имя обязательно')
    return
  }
  try {
    if (editing.value) {
      const { machine } = await filesApi.renameMachine(editing.value.id, name)
      const i = machines.value.findIndex((x) => x.id === machine.id)
      if (i >= 0) machines.value[i] = machine
      modal.value = false
    } else {
      const { machine } = await filesApi.createMachine(name)
      machines.value.push(machine)
      editing.value = machine // показать токен и команду установки
    }
  } catch {
    showToast('Не удалось сохранить')
  }
}

async function removeMachine() {
  if (!editing.value) return
  if (!confirmDelete.value) {
    confirmDelete.value = true
    setTimeout(() => (confirmDelete.value = false), 3500)
    return
  }
  try {
    await filesApi.deleteMachine(editing.value.id)
    machines.value = machines.value.filter((x) => x.id !== editing.value!.id)
    modal.value = false
  } catch {
    showToast('Не удалось удалить')
  }
}

// --- операции записи ---

const inputModal = ref<{ open: boolean; title: string; value: string; onOk: (v: string) => void }>({
  open: false,
  title: '',
  value: '',
  onOk: () => {},
})
function askInput(title: string, value: string, onOk: (v: string) => void) {
  inputModal.value = { open: true, title, value, onOk }
}
function inputOk() {
  const v = inputModal.value.value.trim()
  inputModal.value.open = false
  if (v) inputModal.value.onOk(v)
}

function currentPath(m: FileMachine): string {
  const n = navFor(m.id)
  return n.stack[n.stack.length - 1].path
}
function currentMode(m: FileMachine): 'ro' | 'rw' | null {
  return rootModeOf(m, currentPath(m))
}

function newFolder(m: FileMachine) {
  askInput('Имя новой папки', '', async (name) => {
    try {
      await filesApi.mkdir(m.id, currentPath(m) + '/' + name)
      loadDir(m)
      showToast('Папка создана')
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Не удалось')
    }
  })
}

const uploadTarget = ref<FileMachine | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)
function pickUpload(m: FileMachine) {
  uploadTarget.value = m
  fileInput.value?.click()
}
async function onUpload(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  const m = uploadTarget.value
  input.value = ''
  if (!file || !m) return
  showToast('Загрузка…')
  try {
    await filesApi.uploadFile(m.id, currentPath(m), file)
    loadDir(m)
    showToast('Загружено')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось загрузить')
  }
}

// --- меню действий над записью ---

const entryMenu = ref<{ open: boolean; m: FileMachine | null; e: FileEntry | null; mode: 'ro' | 'rw' | null }>({
  open: false,
  m: null,
  e: null,
  mode: null,
})
function openEntryMenu(m: FileMachine, e: FileEntry) {
  entryMenu.value = { open: true, m, e, mode: rootModeOf(m, e.path) }
}

async function download(m: FileMachine, e: FileEntry) {
  entryMenu.value.open = false
  try {
    const url = await filesApi.streamUrl(m.id, e.path, true)
    const a = document.createElement('a')
    a.href = url
    a.download = e.name
    document.body.appendChild(a)
    a.click()
    a.remove()
  } catch {
    showToast('Не удалось скачать')
  }
}

function renameEntryPrompt(m: FileMachine, e: FileEntry) {
  entryMenu.value.open = false
  askInput('Новое имя', e.name, async (name) => {
    const dir = e.path.slice(0, e.path.length - e.name.length)
    try {
      await filesApi.renameEntry(m.id, e.path, dir + name)
      loadDir(m)
      showToast('Переименовано')
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Не удалось')
    }
  })
}

const confirmEntryDelete = ref(false)
async function deleteEntry(m: FileMachine, e: FileEntry) {
  if (!confirmEntryDelete.value) {
    confirmEntryDelete.value = true
    setTimeout(() => (confirmEntryDelete.value = false), 3500)
    return
  }
  confirmEntryDelete.value = false
  entryMenu.value.open = false
  try {
    await filesApi.removeEntry(m.id, e.path, e.is_dir)
    loadDir(m)
    showToast('Удалено')
  } catch (err) {
    showToast(err instanceof Error ? err.message : 'Не удалось')
  }
}

// --- просмотр файла ---

const viewer = ref<{
  open: boolean
  name: string
  kind: FileKind
  loading: boolean
  text: string
  url: string
  m: FileMachine | null
  e: FileEntry | null
}>({ open: false, name: '', kind: 'other', loading: false, text: '', url: '', m: null, e: null })

const TEXT_LIMIT = 2 * 1024 * 1024

async function openFile(m: FileMachine, e: FileEntry) {
  const kind = fileKind(e.name)
  viewer.value = { open: true, name: e.name, kind, loading: true, text: '', url: '', m, e }
  try {
    if (kind === 'text') {
      if (e.size > TEXT_LIMIT) {
        viewer.value.kind = 'other'
      } else {
        const url = await filesApi.streamUrl(m.id, e.path)
        viewer.value.text = await (await fetch(url)).text()
      }
    } else if (kind === 'image' || kind === 'audio' || kind === 'video' || kind === 'pdf') {
      viewer.value.url = await filesApi.streamUrl(m.id, e.path)
    }
  } catch {
    showToast('Не удалось открыть файл')
    viewer.value.open = false
  } finally {
    viewer.value.loading = false
  }
}

function rowClick(m: FileMachine, e: FileEntry) {
  if (e.is_dir) enterDir(m, e)
  else openFile(m, e)
}
</script>

<template>
  <div v-if="loading" class="hint">Загрузка…</div>

  <template v-else>
    <p v-if="machines.length === 0" class="hint">
      Добавьте домашнюю машину — установите на неё файловый агент, задайте
      папки и режим доступа (только чтение или чтение и запись), и здесь можно
      будет смотреть тексты, картинки, слушать музыку и запускать видео 👇
    </p>

    <div v-for="m in machines" :key="m.id" class="mc-card">
      <div class="mc-head">
        <button class="mc-title" @click="collapsed.has(m.id) ? openMachine(m) : toggleCollapse(m.id)">
          <span class="chevron" :class="{ open: !collapsed.has(m.id) }">▸</span>
          <span class="dot" :class="{ ok: m.online }" />
          🖥 {{ m.name }}
        </button>
        <span>
          <span v-if="!m.online" class="offline">офлайн</span>
          <button class="icon-btn" title="Настройки" @click="openSettings(m)">⚙️</button>
        </span>
      </div>

      <div v-if="!collapsed.has(m.id)" class="mc-body">
        <p v-if="!m.online" class="mc-note">
          Машина офлайн — агент не на связи. Запустите habits-files-agent на ней.
        </p>

        <template v-else>
          <!-- хлебные крошки -->
          <div class="crumbs">
            <template v-for="(c, i) in navFor(m.id).stack" :key="i">
              <button class="crumb" :class="{ last: i === navFor(m.id).stack.length - 1 }" @click="gotoCrumb(m, i)">
                {{ c.label }}
              </button>
              <span v-if="i < navFor(m.id).stack.length - 1" class="crumb-sep">/</span>
            </template>
          </div>

          <!-- панель операций записи -->
          <div v-if="currentMode(m) === 'rw'" class="rw-bar">
            <button class="rw-btn" @click="pickUpload(m)">⬆️ Загрузить</button>
            <button class="rw-btn" @click="newFolder(m)">📁➕ Папка</button>
          </div>

          <div v-if="navFor(m.id).loading" class="hint sm">Загрузка…</div>
          <p v-else-if="navFor(m.id).error" class="mc-err">⚠️ {{ navFor(m.id).error }}</p>
          <p v-else-if="navFor(m.id).entries.length === 0" class="hint sm">Пусто</p>

          <ul v-else class="entries">
            <li v-for="e in navFor(m.id).entries" :key="e.path" class="entry">
              <button class="entry-main" @click="rowClick(m, e)">
                <span class="entry-icon">{{ fileIcon(e) }}</span>
                <span class="entry-name">{{ e.name.split('/').pop() || e.name }}</span>
                <span v-if="!e.is_dir" class="entry-size">{{ fmtBytes(e.size) }}</span>
              </button>
              <button class="entry-menu" title="Действия" @click="openEntryMenu(m, e)">⋮</button>
            </li>
          </ul>
        </template>
      </div>
    </div>

    <button class="add-btn" @click="openCreate">＋ Добавить машину</button>
  </template>

  <input ref="fileInput" type="file" style="display: none" @change="onUpload" />

  <!-- Модалка машины -->
  <div v-if="modal" class="modal" @click.self="modal = false">
    <div class="modal-content mc-modal">
      <h3>{{ editing ? 'Машина' : 'Добавить машину' }}</h3>
      <input v-model="form.name" placeholder="Имя (например, домашний ПК)" maxlength="100" />

      <template v-if="editing && editing.token">
        <div class="push-block">
          <div class="push-label">Токен агента</div>
          <code class="push-code" @click="copyText(editing.token)">{{ editing.token }}</code>
          <div class="push-label">Установка на машине (Linux)</div>
          <code class="push-code" @click="copyText(installCmd(editing))">{{ installCmd(editing) }}</code>
          <p class="kind-hint">
            Нажмите, чтобы скопировать. В кавычках задайте свои папки и режим:
            <code>:ro</code> — только чтение, <code>:rw</code> — чтение и запись,
            через <code>;</code>.
          </p>
        </div>
      </template>

      <button class="btn primary" @click="save">💾 Сохранить</button>
      <button v-if="editing" class="btn danger" @click="removeMachine">
        {{ confirmDelete ? 'Точно удалить?' : '🗑 Удалить машину' }}
      </button>
      <button class="btn" @click="modal = false">{{ editing ? 'Закрыть' : 'Отмена' }}</button>
    </div>
  </div>

  <!-- Ввод текста (папка / переименование) -->
  <div v-if="inputModal.open" class="modal" @click.self="inputModal.open = false">
    <div class="modal-content">
      <h3>{{ inputModal.title }}</h3>
      <input v-model="inputModal.value" class="w100" @keyup.enter="inputOk" />
      <button class="btn primary" @click="inputOk">OK</button>
      <button class="btn" @click="inputModal.open = false">Отмена</button>
    </div>
  </div>

  <!-- Действия над записью -->
  <div v-if="entryMenu.open" class="modal" @click.self="entryMenu.open = false">
    <div class="modal-content">
      <h3 class="ellip">{{ entryMenu.e?.name.split('/').pop() }}</h3>
      <button v-if="entryMenu.e && !entryMenu.e.is_dir" class="btn" @click="download(entryMenu.m!, entryMenu.e!)">
        ⬇️ Скачать
      </button>
      <template v-if="entryMenu.mode === 'rw'">
        <button class="btn" @click="renameEntryPrompt(entryMenu.m!, entryMenu.e!)">✏️ Переименовать</button>
        <button class="btn danger" @click="deleteEntry(entryMenu.m!, entryMenu.e!)">
          {{ confirmEntryDelete ? 'Точно удалить?' : '🗑 Удалить' }}
        </button>
      </template>
      <button class="btn" @click="entryMenu.open = false">Отмена</button>
    </div>
  </div>

  <!-- Просмотр файла -->
  <div v-if="viewer.open" class="modal" @click.self="viewer.open = false">
    <div class="modal-content viewer">
      <div class="viewer-head">
        <span class="ellip">{{ viewer.name }}</span>
        <button class="icon-btn" @click="viewer.open = false">✕</button>
      </div>
      <div class="viewer-body">
        <div v-if="viewer.loading" class="hint sm">Загрузка…</div>
        <pre v-else-if="viewer.kind === 'text'" class="viewer-text">{{ viewer.text }}</pre>
        <img v-else-if="viewer.kind === 'image'" :src="viewer.url" class="viewer-img" />
        <audio v-else-if="viewer.kind === 'audio'" :src="viewer.url" controls autoplay class="viewer-audio" />
        <video v-else-if="viewer.kind === 'video'" :src="viewer.url" controls autoplay playsinline class="viewer-video" />
        <iframe v-else-if="viewer.kind === 'pdf'" :src="viewer.url" class="viewer-pdf" />
        <div v-else class="hint sm">
          Предпросмотр недоступен для этого типа файла.
        </div>
      </div>
      <button v-if="viewer.m && viewer.e" class="btn" @click="download(viewer.m, viewer.e)">⬇️ Скачать</button>
    </div>
  </div>
</template>

<style scoped>
.hint {
  text-align: center;
  color: var(--text-secondary);
  padding: 24px 0;
}
.hint.sm {
  padding: 10px 0;
  font-size: 13px;
}

.mc-card {
  background: var(--card-color);
  border-radius: 12px;
  padding: 6px 12px 10px;
  margin-bottom: 12px;
}

.mc-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.mc-title {
  display: flex;
  align-items: center;
  gap: 8px;
  background: none;
  border: none;
  color: var(--text-color);
  font-weight: 700;
  font-size: 15px;
  padding: 8px 0;
  flex: 1;
  text-align: left;
}

.chevron {
  display: inline-block;
  transition: transform 0.15s;
  color: var(--text-secondary);
}
.chevron.open {
  transform: rotate(90deg);
}

.dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: var(--text-secondary);
}
.dot.ok {
  background: #22c55e;
}

.offline {
  font-size: 11px;
  color: var(--text-secondary);
  margin-right: 6px;
}

.icon-btn {
  background: none;
  border: none;
  padding: 4px 6px;
}

.mc-note,
.mc-err {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 6px 0;
}
.mc-err {
  color: #ef4444;
  overflow-wrap: anywhere;
}

.crumbs {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 2px;
  margin: 4px 0 8px;
}
.crumb {
  background: none;
  border: none;
  color: var(--accent-color);
  font-size: 12px;
  padding: 2px 2px;
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.crumb.last {
  color: var(--text-color);
  font-weight: 600;
}
.crumb-sep {
  color: var(--text-secondary);
  font-size: 12px;
}

.rw-bar {
  display: flex;
  gap: 6px;
  margin-bottom: 8px;
}
.rw-btn {
  flex: 1;
  padding: 7px 6px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
  font-size: 12px;
}

.entries {
  list-style: none;
  margin: 0;
  padding: 0;
}
.entry {
  display: flex;
  align-items: center;
  border-top: 1px solid var(--bg-secondary);
}
.entry-main {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  min-width: 0;
  background: none;
  border: none;
  color: var(--text-color);
  padding: 9px 2px;
  text-align: left;
}
.entry-icon {
  flex-shrink: 0;
}
.entry-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 14px;
}
.entry-size {
  flex-shrink: 0;
  font-size: 11px;
  color: var(--text-secondary);
}
.entry-menu {
  background: none;
  border: none;
  color: var(--text-secondary);
  padding: 8px 6px;
  font-size: 16px;
}

.add-btn {
  display: block;
  width: 100%;
  margin-top: 4px;
  padding: 11px;
  border: none;
  border-radius: 8px;
  background: var(--accent-color);
  color: #fff;
}

.mc-modal {
  text-align: left;
}
.mc-modal h3 {
  text-align: center;
}
.mc-modal input,
.w100 {
  width: 100%;
  margin-top: 8px;
}

.push-block {
  margin-top: 10px;
}
.push-label {
  font-size: 12px;
  font-weight: 700;
  color: var(--text-secondary);
  margin: 8px 0 4px;
}
.push-code {
  display: block;
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 8px 10px;
  font-size: 11px;
  overflow-wrap: anywhere;
  cursor: pointer;
}
.kind-hint {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 8px 0 0;
}

.btn {
  display: block;
  width: 100%;
  margin-top: 10px;
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
.btn.danger {
  background: #b91c1c;
  color: #fff;
}

.ellip {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.viewer {
  max-width: 96vw;
  width: 640px;
}
.viewer-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  margin-bottom: 8px;
}
.viewer-body {
  max-height: 70vh;
  overflow: auto;
}
.viewer-text {
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 12px;
  text-align: left;
  margin: 0;
}
.viewer-img {
  max-width: 100%;
  border-radius: 8px;
}
.viewer-audio {
  width: 100%;
}
.viewer-video {
  width: 100%;
  max-height: 68vh;
  border-radius: 8px;
  background: #000;
}
.viewer-pdf {
  width: 100%;
  height: 68vh;
  border: none;
}
</style>
