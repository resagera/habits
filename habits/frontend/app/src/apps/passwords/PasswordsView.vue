<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '../../shared/api/client'
import { openExternalLink } from '../../shared/telegram'
import { showToast } from '../../shared/toast'
import {
  passwordStrength,
  openPackage,
  randomKey,
  sealPackage,
  type FolderPackage,
  type VaultEntry,
  type VaultFolder,
} from './crypto'
import {
  autoLockMinutes,
  createVault,
  entries,
  folders,
  initVault,
  lastSyncAt,
  lock,
  offlineMode,
  persistSession,
  setAutoLockMinutes,
  syncDirty,
  unlocked,
  unlockWithPassword,
  vaultVersion,
} from './session'
import { DEFAULT_OPTIONS, generatePassword } from './generator'
import { normalizeTotpSecret } from './totp'
import RecipientPicker from '../../components/RecipientPicker.vue'
import PassEntryRow from './components/PassEntryRow.vue'
import PassFolderNode from './components/PassFolderNode.vue'

// 'checking' — выясняем, есть ли хранилище (сервер/кэш)
const vaultState = ref<'checking' | 'exists' | 'none'>('checking')
const masterInput = ref('')
const masterConfirm = ref('')
const showMaster = ref(false)
const busy = ref(false)

const search = ref('')
const revealedId = ref<number | null>(null)
const collapsed = ref(new Set<number>())

// --- модалка записи ---
const entryModal = ref(false)
const editingEntry = ref<VaultEntry | null>(null)
const entryForm = ref({
  name: '',
  login: '',
  password: '',
  url: '',
  description: '',
  totp: '',
  folderId: null as number | null,
})
const confirmDeleteEntry = ref(false)
const showEntryPassword = ref(false)
const genOpen = ref(false)
const gen = ref({ ...DEFAULT_OPTIONS })

// --- модалка папки ---
const folderModal = ref(false)
const editingFolder = ref<VaultFolder | null>(null)
const folderForm = ref({ name: '', parentId: null as number | null, pinned: false })
const confirmDeleteFolder = ref(false)

// --- шаринг папки ---
const shareFolder = ref<VaultFolder | null>(null)
const shareTo = ref('')
const sharing = ref(false)

// --- входящие передачи ---
interface IncomingShare {
  id: number
  folder_name: string
  payload: string
  key: string
  from: { id: number; username: string; first_name: string }
}
const incoming = ref<IncomingShare[]>([])

const autoLock = ref(autoLockMinutes())

const mode = computed(() => {
  if (unlocked.value) return 'unlocked'
  if (vaultState.value === 'checking') return 'checking'
  return vaultState.value === 'exists' ? 'locked' : 'setup'
})

const strength = computed(() => passwordStrength(entryForm.value.password))
const setupStrength = computed(() => passwordStrength(masterInput.value))

const rootFolders = computed(() =>
  folders.value
    .filter((f) => f.parent_id === null)
    .sort((a, b) => Number(b.pinned ?? false) - Number(a.pinned ?? false) || a.id - b.id),
)
const rootEntries = computed(() => entries.value.filter((e) => e.folder_id === null))

/** Поиск по названию, логину, ссылке и описанию. */
const searchResults = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return null
  return entries.value.filter((e) =>
    [e.name, e.login, e.url ?? '', e.description ?? ''].some((v) => v.toLowerCase().includes(q)),
  )
})

const syncLine = computed(() => {
  if (syncDirty.value || offlineMode.value) return '⚠️ не синхронизировано — сервер недоступен'
  if (!lastSyncAt.value) return ''
  const d = new Date(lastSyncAt.value)
  const p = (n: number) => String(n).padStart(2, '0')
  return `v${vaultVersion.value} · дамп на сервер ${p(d.getDate())}.${p(d.getMonth() + 1)} ${p(d.getHours())}:${p(d.getMinutes())}`
})

onMounted(async () => {
  vaultState.value = (await initVault()) === 'exists' ? 'exists' : 'none'
  if (unlocked.value) loadIncoming()
})

async function loadIncoming() {
  try {
    incoming.value = (await api.get<{ shares: IncomingShare[] }>('/passwords/shares')).shares
  } catch {
    /* баннер просто не покажем */
  }
}

async function setup() {
  const pass = masterInput.value
  if (pass.length < 6) {
    showToast('Мастер-пароль — минимум 6 символов')
    return
  }
  if (pass !== masterConfirm.value) {
    showToast('Пароли не совпадают')
    return
  }
  busy.value = true
  try {
    await createVault(pass)
    vaultState.value = 'exists'
    masterInput.value = masterConfirm.value = ''
    showMaster.value = false
    loadIncoming()
  } finally {
    busy.value = false
  }
}

async function unlock() {
  if (!masterInput.value) return
  busy.value = true
  try {
    const res = await unlockWithPassword(masterInput.value)
    if (res === 'wrong') {
      showToast('Неверный мастер-пароль')
      return
    }
    if (res === 'empty') {
      vaultState.value = 'none'
      return
    }
    masterInput.value = ''
    showMaster.value = false
    loadIncoming()
  } finally {
    busy.value = false
  }
}

function nextId(list: { id: number }[]): number {
  return list.reduce((m, x) => Math.max(m, x.id), 0) + 1
}

async function persist(): Promise<'ok' | 'offline' | 'conflict'> {
  const res = await persistSession()
  if (res === 'offline') showToast('Сохранено локально — отправлю на сервер при связи ⚠️')
  return res
}

// --- записи ---
function openAddEntry(folderId: number | null = null) {
  editingEntry.value = null
  entryForm.value = { name: '', login: '', password: '', url: '', description: '', totp: '', folderId }
  confirmDeleteEntry.value = false
  showEntryPassword.value = false
  genOpen.value = false
  entryModal.value = true
}

function openEditEntry(entry: VaultEntry) {
  editingEntry.value = entry
  entryForm.value = {
    name: entry.name,
    login: entry.login,
    password: entry.password,
    url: entry.url ?? '',
    description: entry.description ?? '',
    totp: entry.totp ?? '',
    folderId: entry.folder_id,
  }
  confirmDeleteEntry.value = false
  showEntryPassword.value = false
  genOpen.value = false
  entryModal.value = true
}

function applyGenerated() {
  entryForm.value.password = generatePassword(gen.value)
  showEntryPassword.value = true
}

async function saveEntry() {
  const f = entryForm.value
  if (!f.name.trim()) {
    showToast('Название обязательно')
    return
  }
  let totp: string | undefined
  if (f.totp.trim()) {
    const normalized = normalizeTotpSecret(f.totp)
    if (!normalized) {
      showToast('TOTP-секрет не распознан (нужен base32 или otpauth://)')
      return
    }
    totp = normalized
  }
  const fields = {
    name: f.name.trim(),
    login: f.login.trim(),
    password: f.password,
    url: f.url.trim() || undefined,
    description: f.description.trim() || undefined,
    totp,
    folder_id: f.folderId,
  }
  try {
    if (editingEntry.value) {
      Object.assign(editingEntry.value, fields)
    } else {
      entries.value.push({ id: nextId(entries.value), ...fields })
    }
    const res = await persist()
    entryModal.value = false
    if (res !== 'conflict') showToast('Сохранено 🔐')
  } catch {
    showToast('Не удалось сохранить')
  }
}

async function removeEntry() {
  const entry = editingEntry.value
  if (!entry) return
  if (!confirmDeleteEntry.value) {
    confirmDeleteEntry.value = true
    setTimeout(() => (confirmDeleteEntry.value = false), 3000)
    return
  }
  entries.value = entries.value.filter((e) => e.id !== entry.id)
  await persist()
  entryModal.value = false
  showToast('Удалено 🗑')
}

/** Порядок: сдвиг записи среди соседей по папке. */
async function moveEntry(dir: -1 | 1) {
  const entry = editingEntry.value
  if (!entry) return
  const siblings = entries.value.filter((e) => e.folder_id === entry.folder_id)
  const pos = siblings.findIndex((e) => e.id === entry.id)
  const target = siblings[pos + dir]
  if (!target) return
  const i = entries.value.indexOf(entry)
  const j = entries.value.indexOf(target)
  ;[entries.value[i], entries.value[j]] = [entries.value[j], entries.value[i]]
  await persist()
}

async function copy(text: string, what: string) {
  try {
    await navigator.clipboard.writeText(text)
    showToast(`${what} скопирован`)
  } catch {
    showToast('Не удалось скопировать')
  }
}

function openEntryUrl(entry: VaultEntry) {
  if (!entry.url) return
  const url = /^https?:\/\//.test(entry.url) ? entry.url : 'https://' + entry.url
  openExternalLink(url)
}

// --- папки ---
function openAddFolder() {
  editingFolder.value = null
  folderForm.value = { name: '', parentId: null, pinned: false }
  confirmDeleteFolder.value = false
  folderModal.value = true
}

function openEditFolder(folder: VaultFolder) {
  editingFolder.value = folder
  folderForm.value = { name: folder.name, parentId: folder.parent_id, pinned: folder.pinned ?? false }
  confirmDeleteFolder.value = false
  folderModal.value = true
}

/** Папку нельзя перенести в саму себя или своего потомка. */
function folderDescendants(id: number): Set<number> {
  const ids = new Set<number>([id])
  let grew = true
  while (grew) {
    grew = false
    for (const f of folders.value) {
      if (f.parent_id !== null && ids.has(f.parent_id) && !ids.has(f.id)) {
        ids.add(f.id)
        grew = true
      }
    }
  }
  return ids
}

const folderOptions = computed(() => {
  const banned = editingFolder.value ? folderDescendants(editingFolder.value.id) : new Set<number>()
  const result: { id: number; label: string }[] = []
  const walk = (parentId: number | null, depth: number) => {
    for (const f of folders.value.filter((x) => x.parent_id === parentId)) {
      if (banned.has(f.id)) continue
      result.push({ id: f.id, label: `${'— '.repeat(depth)}📂 ${f.name}` })
      walk(f.id, depth + 1)
    }
  }
  walk(null, 0)
  return result
})

async function saveFolder() {
  const name = folderForm.value.name.trim()
  if (!name) {
    showToast('Введите название папки')
    return
  }
  if (editingFolder.value) {
    editingFolder.value.name = name
    editingFolder.value.parent_id = folderForm.value.parentId
    editingFolder.value.pinned = folderForm.value.pinned
  } else {
    folders.value.push({
      id: nextId(folders.value),
      name,
      parent_id: folderForm.value.parentId,
      pinned: folderForm.value.pinned,
    })
  }
  await persist()
  folderModal.value = false
}

async function removeFolder() {
  const folder = editingFolder.value
  if (!folder) return
  if (!confirmDeleteFolder.value) {
    confirmDeleteFolder.value = true
    setTimeout(() => (confirmDeleteFolder.value = false), 3000)
    return
  }
  // записи и подпапки не удаляем — переносим на уровень выше
  for (const e of entries.value) {
    if (e.folder_id === folder.id) e.folder_id = folder.parent_id
  }
  for (const f of folders.value) {
    if (f.parent_id === folder.id) f.parent_id = folder.parent_id
  }
  folders.value = folders.value.filter((f) => f.id !== folder.id)
  await persist()
  folderModal.value = false
  showToast('Папка удалена, содержимое перенесено выше')
}

function toggleFolder(id: number) {
  if (collapsed.value.has(id)) collapsed.value.delete(id)
  else collapsed.value.add(id)
  collapsed.value = new Set(collapsed.value)
}

// --- шаринг папки ---
function openShare(folder: VaultFolder) {
  folderModal.value = false
  shareFolder.value = folder
  shareTo.value = ''
}

async function sendFolder() {
  const folder = shareFolder.value
  const to = shareTo.value.trim()
  if (!folder || !to) return
  sharing.value = true
  try {
    // собираем поддерево папки с записями, шифруем случайным ключом
    const ids = folderDescendants(folder.id)
    const pkg: FolderPackage = {
      name: folder.name,
      folders: folders.value.filter((f) => ids.has(f.id)),
      entries: entries.value.filter((e) => e.folder_id !== null && ids.has(e.folder_id)),
    }
    const key = randomKey()
    const payload = await sealPackage(key, pkg)
    await api.post('/passwords/shares', { to, folder_name: folder.name, payload, key })
    showToast('Папка отправлена — получатель увидит её на вкладке Passwords 📤')
    shareFolder.value = null
  } catch {
    showToast('Пользователь не найден или ошибка отправки')
  } finally {
    sharing.value = false
  }
}

async function acceptShare(share: IncomingShare) {
  const pkg = await openPackage(share.key, share.payload)
  if (!pkg) {
    showToast('Не удалось расшифровать пакет')
    return
  }
  // переиндексация id пакета в свободные id хранилища
  const folderMap = new Map<number, number>()
  let fid = nextId(folders.value)
  for (const f of pkg.folders) folderMap.set(f.id, fid++)
  for (const f of pkg.folders) {
    folders.value.push({
      id: folderMap.get(f.id)!,
      name: f.name,
      parent_id: f.parent_id !== null && folderMap.has(f.parent_id) ? folderMap.get(f.parent_id)! : null,
      pinned: false,
    })
  }
  let eid = nextId(entries.value)
  for (const e of pkg.entries) {
    entries.value.push({
      ...e,
      id: eid++,
      folder_id: e.folder_id !== null && folderMap.has(e.folder_id) ? folderMap.get(e.folder_id)! : null,
    })
  }
  await persist()
  await declineShare(share, true)
  showToast(`Папка «${pkg.name}» добавлена ✅`)
}

async function declineShare(share: IncomingShare, silent = false) {
  try {
    await api.delete(`/passwords/shares/${share.id}`)
  } catch {
    /* удалим в следующий раз */
  }
  incoming.value = incoming.value.filter((s) => s.id !== share.id)
  if (!silent) showToast('Передача отклонена')
}

function onAutoLockChange() {
  setAutoLockMinutes(autoLock.value)
  showToast(autoLock.value > 0 ? `Автоблокировка: ${autoLock.value} мин` : 'Автоблокировка выключена')
}

function shareFromLabel(s: IncomingShare): string {
  return s.from.first_name || (s.from.username ? '@' + s.from.username : `#${s.from.id}`)
}
</script>

<template>
  <p class="notice">
    🔐 Пароли шифруются на устройстве (AES-256-GCM), мастер-пароль никуда не отправляется.
    На сервер синхронизируется только шифротекст — данные не теряются при смене устройства.
    Забудете мастер-пароль — расшифровать хранилище не сможет никто.
  </p>

  <div v-if="mode === 'checking'" class="hint">Проверяем хранилище…</div>

  <!-- Первичная настройка -->
  <form v-else-if="mode === 'setup'" class="stack" @submit.prevent="setup">
    <div class="pw-field">
      <input
        v-model="masterInput"
        :type="showMaster ? 'text' : 'password'"
        placeholder="Придумайте мастер-пароль"
        autocomplete="off"
      />
      <button type="button" class="pw-eye" @click="showMaster = !showMaster">
        {{ showMaster ? '🙈' : '👁' }}
      </button>
    </div>
    <div v-if="setupStrength" class="strength">
      <div class="strength-bar">
        <div
          class="strength-fill"
          :style="{ width: (setupStrength.score + 1) * 20 + '%', background: setupStrength.color }"
        />
      </div>
      <span class="strength-label" :style="{ color: setupStrength.color }">{{ setupStrength.label }}</span>
    </div>
    <input
      v-model="masterConfirm"
      :type="showMaster ? 'text' : 'password'"
      placeholder="Повторите мастер-пароль"
      autocomplete="off"
    />
    <button type="submit" class="btn primary" :disabled="busy">
      {{ busy ? 'Создание…' : 'Создать хранилище' }}
    </button>
  </form>

  <!-- Разблокировка -->
  <form v-else-if="mode === 'locked'" class="stack" @submit.prevent="unlock">
    <div class="pw-field">
      <input v-model="masterInput" :type="showMaster ? 'text' : 'password'" placeholder="Мастер-пароль" autocomplete="off" />
      <button type="button" class="pw-eye" @click="showMaster = !showMaster">
        {{ showMaster ? '🙈' : '👁' }}
      </button>
    </div>
    <button type="submit" class="btn primary" :disabled="busy">
      {{ busy ? 'Проверка…' : 'Разблокировать' }}
    </button>
    <p v-if="offlineMode" class="hint small">⚠️ Сервер недоступен — используется локальная копия</p>
  </form>

  <!-- Открытое хранилище -->
  <template v-else>
    <!-- входящие передачи папок -->
    <div v-for="share in incoming" :key="share.id" class="incoming">
      <div class="incoming-text">
        📥 Папка «{{ share.folder_name }}» от {{ shareFromLabel(share) }}
      </div>
      <div class="incoming-actions">
        <button class="btn small primary" @click="acceptShare(share)">Принять</button>
        <button class="btn small" @click="declineShare(share)">✕</button>
      </div>
    </div>

    <div class="toolbar">
      <button class="btn small" @click="openAddEntry(null)">＋🔑</button>
      <button class="btn small" @click="openAddFolder">＋📂</button>
      <button class="btn small" title="Заблокировать" @click="lock()">🔒</button>
    </div>

    <input v-model="search" type="search" class="search" placeholder="🔍 Название, логин, ссылка, описание…" />

    <p v-if="syncLine" class="sync-line" :class="{ warn: syncDirty || offlineMode }">{{ syncLine }}</p>

    <label class="autolock">
      <span>⏱ Автоблокировка при бездействии</span>
      <select v-model.number="autoLock" @change="onAutoLockChange">
        <option :value="0">выкл</option>
        <option :value="1">1 мин</option>
        <option :value="5">5 мин</option>
        <option :value="15">15 мин</option>
      </select>
    </label>

    <!-- результаты поиска -->
    <template v-if="searchResults !== null">
      <p v-if="searchResults.length === 0" class="hint">Ничего не найдено</p>
      <PassEntryRow
        v-for="entry in searchResults"
        :key="entry.id"
        :entry="entry"
        :revealed="revealedId === entry.id"
        @copy-login="copy(entry.login, 'Логин')"
        @copy-password="copy(entry.password, 'Пароль')"
        @toggle-reveal="revealedId = revealedId === entry.id ? null : entry.id"
        @edit="openEditEntry(entry)"
        @open-url="openEntryUrl(entry)"
      />
    </template>

    <!-- дерево -->
    <template v-else>
      <p v-if="entries.length === 0 && folders.length === 0" class="hint">Пока нет сохранённых паролей</p>

      <PassFolderNode
        v-for="folder in rootFolders"
        :key="folder.id"
        :folder="folder"
        :folders="folders"
        :entries="entries"
        :collapsed="collapsed"
        :revealed-id="revealedId"
        :level="0"
        @toggle="toggleFolder"
        @add-entry="openAddEntry"
        @edit-folder="openEditFolder"
        @edit-entry="openEditEntry"
        @copy-login="(e) => copy(e.login, 'Логин')"
        @copy-password="(e) => copy(e.password, 'Пароль')"
        @reveal="revealedId = revealedId === $event ? null : $event"
        @open-url="openEntryUrl"
      />

      <PassEntryRow
        v-for="entry in rootEntries"
        :key="entry.id"
        :entry="entry"
        :revealed="revealedId === entry.id"
        @copy-login="copy(entry.login, 'Логин')"
        @copy-password="copy(entry.password, 'Пароль')"
        @toggle-reveal="revealedId = revealedId === entry.id ? null : entry.id"
        @edit="openEditEntry(entry)"
        @open-url="openEntryUrl(entry)"
      />
    </template>
  </template>

  <!-- Модалка записи -->
  <div v-if="entryModal" class="modal" @click.self="entryModal = false">
    <div class="modal-content pass-modal">
      <h3>{{ editingEntry ? 'Редактирование' : 'Новый пароль' }}</h3>
      <input v-model="entryForm.name" placeholder="Название (обязательно)" />
      <input v-model="entryForm.login" placeholder="Логин / Email" autocomplete="off" />
      <div class="pw-row">
        <div class="pw-field">
          <input
            v-model="entryForm.password"
            :type="showEntryPassword ? 'text' : 'password'"
            placeholder="Пароль"
            autocomplete="off"
          />
          <button type="button" class="pw-eye" @click="showEntryPassword = !showEntryPassword">
            {{ showEntryPassword ? '🙈' : '👁' }}
          </button>
        </div>
        <button type="button" class="gen-btn" :class="{ active: genOpen }" title="Генератор" @click="genOpen = !genOpen">
          🎲
        </button>
      </div>

      <div v-if="strength" class="strength">
        <div class="strength-bar">
          <div class="strength-fill" :style="{ width: (strength.score + 1) * 20 + '%', background: strength.color }" />
        </div>
        <span class="strength-label" :style="{ color: strength.color }">{{ strength.label }}</span>
      </div>

      <div v-if="genOpen" class="gen-panel">
        <label class="gen-len">
          <span>Длина: {{ gen.length }}</span>
          <input v-model.number="gen.length" type="range" min="8" max="64" />
        </label>
        <div class="gen-checks">
          <label><input v-model="gen.lower" type="checkbox" /> abc</label>
          <label><input v-model="gen.upper" type="checkbox" /> ABC</label>
          <label><input v-model="gen.digits" type="checkbox" /> 123</label>
          <label><input v-model="gen.symbols" type="checkbox" /> #$%</label>
        </div>
        <button type="button" class="btn small" @click="applyGenerated">🎲 Сгенерировать</button>
      </div>

      <input v-model="entryForm.url" placeholder="Ссылка (https://…)" autocomplete="off" spellcheck="false" />
      <input v-model="entryForm.description" placeholder="Описание (необязательно)" />
      <input
        v-model="entryForm.totp"
        placeholder="TOTP-секрет для 2FA (base32 или otpauth://)"
        autocomplete="off"
        spellcheck="false"
      />
      <select v-model="entryForm.folderId">
        <option :value="null">🏠 Без папки</option>
        <option v-for="opt in folderOptions" :key="opt.id" :value="opt.id">{{ opt.label }}</option>
      </select>

      <div v-if="editingEntry" class="order-row">
        <span>Порядок:</span>
        <button class="btn slim" @click="moveEntry(-1)">⬆️ выше</button>
        <button class="btn slim" @click="moveEntry(1)">⬇️ ниже</button>
      </div>

      <button class="btn primary" @click="saveEntry">💾 Сохранить</button>
      <button v-if="editingEntry" class="btn danger" @click="removeEntry">
        {{ confirmDeleteEntry ? 'Точно удалить?' : '🗑 Удалить' }}
      </button>
      <button class="btn" @click="entryModal = false">Отмена</button>
    </div>
  </div>

  <!-- Модалка папки -->
  <div v-if="folderModal" class="modal" @click.self="folderModal = false">
    <div class="modal-content pass-modal">
      <h3>{{ editingFolder ? 'Настройки папки' : 'Новая папка' }}</h3>
      <input v-model="folderForm.name" placeholder="Название папки" maxlength="100" />
      <select v-model="folderForm.parentId">
        <option :value="null">🏠 Корень</option>
        <option v-for="opt in folderOptions" :key="opt.id" :value="opt.id">{{ opt.label }}</option>
      </select>
      <label class="check-line">
        <input v-model="folderForm.pinned" type="checkbox" />
        <span>📌 Закрепить (показывать первой)</span>
      </label>
      <button class="btn primary" @click="saveFolder">💾 Сохранить</button>
      <button v-if="editingFolder" class="btn" @click="openShare(editingFolder)">📤 Передать пользователю</button>
      <button v-if="editingFolder" class="btn danger" @click="removeFolder">
        {{ confirmDeleteFolder ? 'Удалить? Содержимое перенесётся выше' : '🗑 Удалить папку' }}
      </button>
      <button class="btn" @click="folderModal = false">Отмена</button>
    </div>
  </div>

  <!-- Шаринг папки -->
  <div v-if="shareFolder" class="modal" @click.self="shareFolder = null">
    <div class="modal-content pass-modal">
      <h3>Передать «{{ shareFolder.name }}»</h3>
      <p class="hint small left">
        Папка с вложенными подпапками и паролями будет зашифрована на этом
        устройстве и передана пользователю приложения. Он получит уведомление от бота.
      </p>
      <RecipientPicker v-model="shareTo" />
      <button class="btn primary" :disabled="sharing || !shareTo.trim()" @click="sendFolder">
        {{ sharing ? 'Отправка…' : '📤 Отправить' }}
      </button>
      <button class="btn" @click="shareFolder = null">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.notice {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 12px;
}

.stack {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.toolbar {
  display: flex;
  gap: 6px;
  margin-bottom: 10px;
}

.search {
  width: 100%;
  margin-bottom: 8px;
}

.sync-line {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 0 0 8px;
}

.sync-line.warn {
  color: #f59e0b;
}

.hint {
  text-align: center;
  color: var(--text-secondary);
  padding: 24px 0;
}

.hint.small {
  padding: 4px 0;
  font-size: 12px;
}

.hint.small.left {
  text-align: left;
}

.incoming {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  background: var(--card-color);
  border: 1px solid var(--accent-color);
  border-radius: 10px;
  padding: 10px 12px;
  margin-bottom: 10px;
}

.incoming-text {
  font-size: 13px;
  min-width: 0;
}

.incoming-actions {
  display: flex;
  gap: 6px;
  flex: none;
}

.btn {
  padding: 10px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
}

.btn.small {
  flex: 1;
  padding: 8px 6px;
  font-size: 14px;
}

.incoming .btn.small {
  flex: none;
  padding: 7px 12px;
  font-size: 13px;
}

.btn.slim {
  padding: 6px 10px;
  font-size: 12px;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn.danger {
  background: #b91c1c;
  color: #fff;
}

.autolock {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 12px;
}

.pass-modal {
  text-align: left;
  max-height: 88vh;
  overflow-y: auto;
}

.pass-modal h3 {
  text-align: center;
}

.pass-modal .btn {
  display: block;
  width: 100%;
  margin-top: 10px;
}

.pass-modal input,
.pass-modal select {
  width: 100%;
  margin-top: 8px;
}

.pw-row {
  display: flex;
  gap: 6px;
  align-items: center;
  margin-top: 8px;
}

.pw-row .pw-field {
  flex: 1;
  min-width: 0;
}

.pw-row .pw-field input {
  margin-top: 0;
}

.gen-btn {
  flex: none;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  padding: 8px 10px;
  font-size: 16px;
}

.gen-btn.active {
  background: var(--accent-color);
}

.gen-panel {
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 10px;
  margin-top: 8px;
}

.gen-len {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
}

.gen-panel input[type='range'] {
  width: 100%;
  margin-top: 4px;
}

.gen-checks {
  display: flex;
  gap: 12px;
  margin: 8px 0;
  font-size: 13px;
}

.gen-checks label {
  display: flex;
  align-items: center;
  gap: 4px;
}

.gen-checks input[type='checkbox'] {
  width: 16px !important;
  height: 16px;
  margin: 0;
  flex: none;
}

.gen-panel .btn.small {
  width: 100%;
}

/* индикатор надёжности: красный → оранжевый → жёлтый → зелёный */
.strength {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 6px;
}

.strength-bar {
  flex: 1;
  height: 5px;
  background: var(--bg-secondary);
  border-radius: 3px;
  overflow: hidden;
}

.strength-fill {
  height: 100%;
  border-radius: 3px;
  transition: width 0.2s, background 0.2s;
}

.strength-label {
  flex: none;
  font-size: 11px;
}

.order-row {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 10px;
  font-size: 12px;
  color: var(--text-secondary);
}

.order-row .btn {
  display: inline-block;
  width: auto;
  margin-top: 0;
}

.check-line {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 10px;
  cursor: pointer;
  font-size: 13px;
}

.check-line input[type='checkbox'] {
  width: 18px !important;
  height: 18px;
  flex: none;
  margin: 0;
}

.pw-field {
  position: relative;
}

.pw-field input {
  width: 100%;
  padding-right: 42px;
}

.pw-eye {
  position: absolute;
  top: 50%;
  right: 4px;
  transform: translateY(-50%);
  background: none;
  border: none;
  padding: 6px 8px;
  font-size: 16px;
  line-height: 1;
}
</style>
