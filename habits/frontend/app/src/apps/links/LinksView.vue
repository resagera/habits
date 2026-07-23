<script setup lang="ts">
import { computed, onMounted, provide, ref } from 'vue'
import { openExternalLink } from '../../shared/telegram'
import { showToast } from '../../shared/toast'
import RecipientPicker from '../../components/RecipientPicker.vue'
import FolderNode from './components/FolderNode.vue'
import LinkRow from './components/LinkRow.vue'
import { linksHandlersKey } from './keys'
import {
  folderShareToken,
  getLinksMode,
  getLinksPrefs,
  linkShareToken,
  linksBackend,
  loadLinksMode,
  shareFolderToUser,
  shareLinkToUser,
} from './storage'
import type { LinkFolder, LinkItem } from './types'

const folders = ref<LinkFolder[]>([])
const links = ref<LinkItem[]>([])
const loading = ref(true)
const search = ref('')
const mode = ref(getLinksMode())
const prefs = getLinksPrefs()

const OPEN_KEY = 'links_open_folders'
const openFolders = ref(new Set<number>(JSON.parse(localStorage.getItem(OPEN_KEY) || '[]')))

// --- модалка ссылки (создание и редактирование) ---
const linkModal = ref(false)
const editingLink = ref<LinkItem | null>(null)
const linkForm = ref({ name: '', url: '', tags: '', pinned: false, folderId: null as number | null })
const confirmDeleteLink = ref(false)

// --- модалка папки ---
const folderModal = ref(false)
const editingFolder = ref<LinkFolder | null>(null)
const folderForm = ref({ name: '', parentId: null as number | null })
const confirmDeleteFolder = ref(false)

// --- попап «Поделиться» (папка или ссылка) ---
// Экран как в Checker: отправить пользователю по id/@логину, ссылка-приглашение
// (t.me/…?startapp=…) и копирование в буфер. Отправка по id и ссылка-приглашение
// работают только с серверным хранилищем — у объекта должен быть серверный id.
const shareFolderModal = ref<LinkFolder | null>(null)
const shareLinkModal = ref<LinkItem | null>(null)
const shareTo = ref('')
const shareBusy = ref(false)
const inviteLink = ref('')

onMounted(load)

async function load() {
  loading.value = true
  try {
    // выбор хранилища читаем с сервера: локальный кэш Telegram может очищать
    mode.value = await loadLinksMode()
    const data = await linksBackend(mode.value).loadTree()
    folders.value = data.folders
    links.value = data.links
  } catch {
    showToast('Не удалось загрузить ссылки')
  } finally {
    loading.value = false
  }
}

const rootFolders = computed(() => folders.value.filter((f) => f.parent_id === null))
const rootLinks = computed(() => links.value.filter((l) => l.folder_id === null))
const pinnedLinks = computed(() => links.value.filter((l) => l.pinned))

const topLinks = computed(() =>
  links.value
    .filter((l) => l.clicks > 0)
    .sort((a, b) => b.clicks - a.clicks)
    .slice(0, 10),
)

/** Все теги с частотой — для подсказок в модалке. */
const allTags = computed(() => {
  const counts = new Map<string, number>()
  for (const l of links.value) {
    for (const t of l.tags) counts.set(t, (counts.get(t) ?? 0) + 1)
  }
  return [...counts.entries()].sort((a, b) => b[1] - a[1]).map(([t]) => t)
})

/** Подсказки под полем тегов: по последнему вводимому сегменту. */
const tagSuggestions = computed(() => {
  const parts = linkForm.value.tags.split(',')
  const current = (parts[parts.length - 1] ?? '').trim().toLowerCase()
  const already = new Set(parts.slice(0, -1).map((t) => t.trim().toLowerCase()))
  return allTags.value
    .filter((t) => !already.has(t.toLowerCase()))
    .filter((t) => (current ? t.toLowerCase().startsWith(current) && t.toLowerCase() !== current : true))
    .slice(0, 8)
})

function appendTag(tag: string) {
  const parts = linkForm.value.tags.split(',').slice(0, -1)
  parts.push(' ' + tag)
  linkForm.value.tags = parts.join(',').replace(/^ /, '') + ', '
}

const pasteFallback = ref(false)

/** Чтение буфера через Telegram WebApp API (работает не во всех режимах). */
function tgReadClipboard(): Promise<string> {
  return new Promise((resolve) => {
    const tg = (window as { Telegram?: { WebApp?: { readTextFromClipboard?: (cb: (t: string | null) => void) => void } } })
      .Telegram?.WebApp
    if (!tg?.readTextFromClipboard) {
      resolve('')
      return
    }
    const timer = setTimeout(() => resolve(''), 1200)
    try {
      tg.readTextFromClipboard((t) => {
        clearTimeout(timer)
        resolve(t ?? '')
      })
    } catch {
      clearTimeout(timer)
      resolve('')
    }
  })
}

async function pasteUrl() {
  // 1) стандартный Clipboard API (браузер)
  try {
    const text = (await navigator.clipboard.readText()).trim()
    if (text) {
      linkForm.value.url = text
      return
    }
  } catch {
    // в Telegram-webview readText запрещён — идём дальше
  }
  // 2) Telegram WebApp API
  const tgText = (await tgReadClipboard()).trim()
  if (tgText) {
    linkForm.value.url = tgText
    return
  }
  // 3) поле ручной вставки: событие paste доступно без разрешений
  pasteFallback.value = true
}

function onFallbackPaste(e: ClipboardEvent) {
  const text = e.clipboardData?.getData('text')?.trim()
  if (text) {
    linkForm.value.url = text
    pasteFallback.value = false
  }
}

async function copyEditingUrl() {
  if (!editingLink.value) return
  try {
    await navigator.clipboard.writeText(editingLink.value.url)
    showToast('URL скопирован')
  } catch {
    showToast('Не удалось скопировать')
  }
}

const searchResults = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return null
  return links.value.filter(
    (l) =>
      l.name.toLowerCase().includes(q) ||
      l.url.toLowerCase().includes(q) ||
      l.tags.some((t) => t.toLowerCase().includes(q)),
  )
})

/** Папки-варианты для select: (id, имя с отступом), без ветки excludeId. */
function folderOptions(excludeId: number | null): { id: number; label: string }[] {
  const result: { id: number; label: string }[] = []
  const walk = (parentId: number | null, depth: number) => {
    for (const f of folders.value.filter((x) => x.parent_id === parentId)) {
      if (excludeId !== null && f.id === excludeId) continue // ветка исключается целиком
      result.push({ id: f.id, label: ' '.repeat(depth * 3) + '📂 ' + f.name })
      walk(f.id, depth + 1)
    }
  }
  walk(null, 0)
  return result
}

function saveOpen() {
  localStorage.setItem(OPEN_KEY, JSON.stringify([...openFolders.value]))
}

provide(linksHandlersKey, {
  toggleFolder(id) {
    if (openFolders.value.has(id)) openFolders.value.delete(id)
    else openFolders.value.add(id)
    saveOpen()
  },
  isOpen: (id) => openFolders.value.has(id),
  editFolder(folder) {
    editingFolder.value = folder
    folderForm.value = { name: folder.name, parentId: folder.parent_id }
    confirmDeleteFolder.value = false
    shareTo.value = ''
    folderModal.value = true
  },
  addLinkTo(folderId) {
    editingLink.value = null
    linkForm.value = { name: '', url: '', tags: '', pinned: false, folderId }
    pasteFallback.value = false
    linkModal.value = true
  },
  openLink(link) {
    openExternalLink(link.url)
    // счётчик переходов для топ-10 (не блокируем открытие)
    linksBackend()
      .click(link.id)
      .then((clicks) => (link.clicks = clicks))
      .catch(() => {})
  },
  async copyLink(link) {
    try {
      await navigator.clipboard.writeText(link.url)
      showToast('URL скопирован')
    } catch {
      showToast('Не удалось скопировать')
    }
  },
  shareLink(link) {
    openShareLink(link)
  },
  editLink(link) {
    editingLink.value = link
    linkForm.value = {
      name: link.name,
      url: link.url,
      tags: link.tags.join(', '),
      pinned: link.pinned,
      folderId: link.folder_id,
    }
    confirmDeleteLink.value = false
    pasteFallback.value = false
    linkModal.value = true
  },
})

function openAddLink() {
  editingLink.value = null
  linkForm.value = { name: '', url: '', tags: '', pinned: false, folderId: null }
  pasteFallback.value = false
  linkModal.value = true
}

function openAddFolder() {
  editingFolder.value = null
  folderForm.value = { name: '', parentId: null }
  confirmDeleteFolder.value = false
  folderModal.value = true
}

function parseTags(raw: string): string[] {
  return [...new Set(raw.split(',').map((t) => t.trim().replace(/^#/, '')).filter(Boolean))]
}

async function saveLink() {
  const f = linkForm.value
  const name = f.name.trim()
  let url = f.url.trim()
  if (!name || !url) {
    showToast('Название и URL обязательны')
    return
  }
  if (!/^[a-z][a-z0-9+.-]*:/i.test(url)) url = 'https://' + url
  try {
    if (editingLink.value) {
      const updated = await linksBackend().updateLink(editingLink.value.id, {
        name,
        url,
        tags: parseTags(f.tags),
        pinned: f.pinned,
        folder_id: f.folderId,
      })
      const i = links.value.findIndex((l) => l.id === updated.id)
      if (i >= 0) links.value[i] = updated
    } else {
      const created = await linksBackend().createLink({
        name,
        url,
        tags: parseTags(f.tags),
        pinned: f.pinned,
        folder_id: f.folderId,
      })
      links.value.push(created)
      if (f.folderId !== null) {
        openFolders.value.add(f.folderId)
        saveOpen()
      }
    }
    linkModal.value = false
  } catch {
    showToast('Не удалось сохранить ссылку')
  }
}

async function deleteLink() {
  const link = editingLink.value
  if (!link) return
  if (!confirmDeleteLink.value) {
    confirmDeleteLink.value = true
    setTimeout(() => (confirmDeleteLink.value = false), 3000)
    return
  }
  try {
    await linksBackend().deleteLink(link.id)
    links.value = links.value.filter((l) => l.id !== link.id)
    linkModal.value = false
  } catch {
    showToast('Не удалось удалить')
  }
}

async function saveFolder() {
  const name = folderForm.value.name.trim()
  if (!name) {
    showToast('Введите название папки')
    return
  }
  try {
    if (editingFolder.value) {
      const updated = await linksBackend().updateFolder(editingFolder.value.id, {
        name,
        parent_id: folderForm.value.parentId,
      })
      const i = folders.value.findIndex((f) => f.id === updated.id)
      if (i >= 0) folders.value[i] = updated
    } else {
      const created = await linksBackend().createFolder(name, folderForm.value.parentId)
      folders.value.push(created)
    }
    folderModal.value = false
  } catch {
    showToast('Не удалось сохранить папку')
  }
}

async function deleteFolder() {
  const folder = editingFolder.value
  if (!folder) return
  if (!confirmDeleteFolder.value) {
    confirmDeleteFolder.value = true
    setTimeout(() => (confirmDeleteFolder.value = false), 3000)
    return
  }
  try {
    await linksBackend().deleteFolder(folder.id)
    await load() // каскад мог удалить вложенное — проще перечитать
    folderModal.value = false
  } catch {
    showToast('Не удалось удалить папку')
  }
}

/** id папки и всех её потомков. */
function subtreeFolderIds(rootId: number): Set<number> {
  const ids = new Set<number>([rootId])
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

async function shareFolder() {
  const folder = shareFolderModal.value
  if (!folder) return
  const ids = subtreeFolderIds(folder.id)
  const list = links.value.filter((l) => l.folder_id !== null && ids.has(l.folder_id))
  if (list.length === 0) {
    showToast('Папка пуста')
    return
  }
  const text = `📂 ${folder.name}\n` + list.map((l) => `• ${l.name} — ${l.url}`).join('\n')
  try {
    await navigator.clipboard.writeText(text)
    showToast(`Скопировано ссылок: ${list.length} 📋`)
  } catch {
    showToast('Не удалось скопировать')
  }
}

/** Открыть попап «Поделиться» для папки; в серверном режиме запросить ссылку. */
function openShareFolder(folder: LinkFolder) {
  folderModal.value = false
  shareLinkModal.value = null
  shareFolderModal.value = folder
  shareTo.value = ''
  inviteLink.value = ''
  if (mode.value === 'server') {
    folderShareToken(folder.id)
      .then(({ link, token }) => (inviteLink.value = link || `lnf_${token}`))
      .catch(() => showToast('Не удалось получить ссылку'))
  }
}

/** Открыть попап «Поделиться» для одной ссылки. */
function openShareLink(link: LinkItem) {
  linkModal.value = false
  shareFolderModal.value = null
  shareLinkModal.value = link
  shareTo.value = ''
  inviteLink.value = ''
  if (mode.value === 'server') {
    linkShareToken(link.id)
      .then(({ link: url, token }) => (inviteLink.value = url || `lnk_${token}`))
      .catch(() => showToast('Не удалось получить ссылку'))
  }
}

function shareRecipientName(u: { id: number; username: string; first_name: string }): string {
  return u.first_name || (u.username ? '@' + u.username : '#' + u.id)
}

/** Отправить папку пользователю приложения (копия поддерева). */
async function sendFolderToUser() {
  const folder = shareFolderModal.value
  const to = shareTo.value.trim()
  if (!folder || !to || shareBusy.value) return
  shareBusy.value = true
  try {
    const { sent_to, queued } = await shareFolderToUser(folder.id, to)
    const who = shareRecipientName(sent_to)
    showToast(queued ? `Приглашение отправлено ${who} 📨` : `Отправлено ${who} 📤`)
    shareFolderModal.value = null
  } catch (e) {
    const msg = (e as { message?: string })?.message
    showToast(msg === 'user not found' ? 'Пользователь не найден' : 'Не удалось отправить')
  } finally {
    shareBusy.value = false
  }
}

/** Отправить одну ссылку пользователю приложения (копия). */
async function sendLinkToUser() {
  const link = shareLinkModal.value
  const to = shareTo.value.trim()
  if (!link || !to || shareBusy.value) return
  shareBusy.value = true
  try {
    const { sent_to, queued } = await shareLinkToUser(link.id, to)
    const who = shareRecipientName(sent_to)
    showToast(queued ? `Приглашение отправлено ${who} 📨` : `Отправлено ${who} 📤`)
    shareLinkModal.value = null
  } catch (e) {
    const msg = (e as { message?: string })?.message
    showToast(msg === 'user not found' ? 'Пользователь не найден' : 'Не удалось отправить')
  } finally {
    shareBusy.value = false
  }
}

/** Скопировать ссылку-приглашение в буфер. */
async function copyInvite() {
  if (!inviteLink.value) return
  try {
    await navigator.clipboard.writeText(inviteLink.value)
    showToast('Ссылка-приглашение скопирована 🔗')
  } catch {
    showToast('Не удалось скопировать')
  }
}

/** Скопировать URL ссылки из попапа шаринга. */
async function copyShareLinkUrl() {
  const link = shareLinkModal.value
  if (!link) return
  try {
    await navigator.clipboard.writeText(link.url)
    showToast('URL скопирован')
  } catch {
    showToast('Не удалось скопировать')
  }
}

function exportFolder() {
  const folder = editingFolder.value
  if (!folder) return
  const ids = subtreeFolderIds(folder.id)
  const data = {
    folders: folders.value.filter((f) => ids.has(f.id)),
    links: links.value.filter((l) => l.folder_id !== null && ids.has(l.folder_id)),
  }
  downloadJSON(data, `links_folder_${folder.name}.json`)
}

function downloadJSON(data: unknown, filename: string) {
  const url = URL.createObjectURL(
    new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }),
  )
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}
</script>

<template>
  <p class="mode-note">
    {{ mode === 'local' ? '📱 Хранится локально на устройстве' : '☁️ Хранится на сервере' }}
    <RouterLink to="/settings" class="mode-link">изменить</RouterLink>
  </p>

  <div class="controls">
    <button class="round-btn" title="Новая папка" @click="openAddFolder">+📂</button>
    <button class="round-btn" title="Новая ссылка" @click="openAddLink">+🔗</button>
    <input v-model="search" class="search" placeholder="Поиск по названию, URL, тегам…" />
  </div>

  <p v-if="!loading" class="stats">📂 {{ folders.length }} · 🔗 {{ links.length }}</p>

  <div v-if="loading" class="hint">Загрузка…</div>

  <!-- Режим поиска: плоский список -->
  <template v-else-if="searchResults">
    <p class="hint-small">Найдено: {{ searchResults.length }}</p>
    <LinkRow v-for="link in searchResults" :key="link.id" :link="link" />
  </template>

  <template v-else>
    <p v-if="folders.length === 0 && links.length === 0" class="hint">
      Пока нет ни папок, ни ссылок — добавьте 👆
    </p>

    <div v-if="prefs.showFavorites && pinnedLinks.length" class="pinned-block">
      <h4>⭐ Избранное</h4>
      <LinkRow v-for="link in pinnedLinks" :key="'p' + link.id" :link="link" />
    </div>

    <div v-if="prefs.showTop10 && topLinks.length" class="pinned-block">
      <h4>📈 Топ-10</h4>
      <LinkRow v-for="link in topLinks" :key="'t' + link.id" :link="link" />
    </div>

    <FolderNode
      v-for="folder in rootFolders"
      :key="folder.id"
      :folder="folder"
      :folders="folders"
      :links="links"
      root
    />
    <LinkRow v-for="link in rootLinks" :key="link.id" :link="link" />
  </template>

  <!-- Модалка ссылки -->
  <div v-if="linkModal" class="modal" @click.self="linkModal = false">
    <div class="modal-content">
      <h3>{{ editingLink ? 'Редактирование ссылки' : 'Новая ссылка' }}</h3>
      <input v-model="linkForm.name" placeholder="Название" maxlength="500" />
      <div class="url-row">
        <input v-model="linkForm.url" placeholder="URL" maxlength="2000" />
        <button class="paste-btn" title="Вставить из буфера" @click="pasteUrl">📥</button>
      </div>
      <input
        v-if="pasteFallback"
        class="paste-fallback"
        placeholder="Нажмите сюда и удерживайте → «Вставить»"
        readonly
        @paste.prevent="onFallbackPaste"
        @focus="($event.target as HTMLInputElement).removeAttribute('readonly')"
      />
      <input v-model="linkForm.tags" placeholder="Теги (через запятую)" />
      <div v-if="tagSuggestions.length" class="tag-suggest">
        <button v-for="tag in tagSuggestions" :key="tag" class="tag-chip" @click="appendTag(tag)">
          #{{ tag }}
        </button>
      </div>
      <select v-model="linkForm.folderId">
        <option :value="null">🏠 Корень</option>
        <option v-for="opt in folderOptions(null)" :key="opt.id" :value="opt.id">{{ opt.label }}</option>
      </select>
      <label class="check-line">
        <input v-model="linkForm.pinned" type="checkbox" />
        <span>⭐ В избранное</span>
      </label>
      <button class="btn primary" @click="saveLink">💾 Сохранить</button>
      <template v-if="editingLink">
        <div class="btn-row">
          <button class="btn" @click="openExternalLink(editingLink.url)">🌐 Перейти</button>
          <button class="btn" @click="copyEditingUrl">📋 Копировать</button>
          <button class="btn" @click="openShareLink(editingLink)">📤 Поделиться</button>
        </div>
        <button class="btn danger" @click="deleteLink">
          {{ confirmDeleteLink ? 'Точно удалить?' : '🗑 Удалить' }}
        </button>
      </template>
      <button class="btn" @click="linkModal = false">Отмена</button>
    </div>
  </div>

  <!-- Модалка папки -->
  <div v-if="folderModal" class="modal" @click.self="folderModal = false">
    <div class="modal-content">
      <h3>{{ editingFolder ? 'Настройки папки' : 'Новая папка' }}</h3>
      <input v-model="folderForm.name" placeholder="Название папки" maxlength="200" />
      <select v-model="folderForm.parentId">
        <option :value="null">🏠 Корень</option>
        <option
          v-for="opt in folderOptions(editingFolder?.id ?? null)"
          :key="opt.id"
          :value="opt.id"
        >
          {{ opt.label }}
        </option>
      </select>
      <button class="btn primary" @click="saveFolder">💾 Сохранить</button>
      <template v-if="editingFolder">
        <div class="btn-row">
          <button class="btn" @click="openShareFolder(editingFolder)">📤 Поделиться</button>
          <button class="btn" @click="exportFolder">🗂 Экспорт</button>
        </div>
        <button class="btn danger" @click="deleteFolder">
          {{ confirmDeleteFolder ? 'Точно удалить? Всё содержимое будет потеряно' : '🗑 Удалить папку' }}
        </button>
      </template>
      <button class="btn" @click="folderModal = false">Отмена</button>
    </div>
  </div>

  <!-- Попап «Поделиться папкой» -->
  <div v-if="shareFolderModal" class="modal" @click.self="shareFolderModal = null">
    <div class="modal-content">
      <h3>Поделиться «{{ shareFolderModal.name }}»</h3>

      <template v-if="mode === 'server'">
        <label class="field-label">Пользователю приложения</label>
        <RecipientPicker v-model="shareTo" />
        <button class="btn primary" :disabled="shareBusy || !shareTo.trim()" @click="sendFolderToUser">
          {{ shareBusy ? 'Отправка…' : '📤 Отправить' }}
        </button>
        <p class="field-hint">Получит копию папки со всеми вложенными папками и ссылками.</p>

        <label class="field-label">Ссылка-приглашение (для любого друга в Telegram)</label>
        <div class="invite-box">{{ inviteLink || 'получаем ссылку…' }}</div>
        <button class="btn" :disabled="!inviteLink" @click="copyInvite">🔗 Копировать ссылку</button>
      </template>
      <p v-else class="field-hint">
        Отправка пользователю и ссылка-приглашение доступны в серверном хранилище.
      </p>

      <label class="field-label">Скопировать текстом</label>
      <button class="btn" @click="shareFolder">📋 Копировать список ссылок</button>

      <button class="btn" @click="shareFolderModal = null">Закрыть</button>
    </div>
  </div>

  <!-- Попап «Поделиться ссылкой» -->
  <div v-if="shareLinkModal" class="modal" @click.self="shareLinkModal = null">
    <div class="modal-content">
      <h3>Поделиться «{{ shareLinkModal.name }}»</h3>

      <template v-if="mode === 'server'">
        <label class="field-label">Пользователю приложения</label>
        <RecipientPicker v-model="shareTo" />
        <button class="btn primary" :disabled="shareBusy || !shareTo.trim()" @click="sendLinkToUser">
          {{ shareBusy ? 'Отправка…' : '📤 Отправить' }}
        </button>
        <p class="field-hint">Получит копию ссылки в свои Links.</p>

        <label class="field-label">Ссылка-приглашение (для любого друга в Telegram)</label>
        <div class="invite-box">{{ inviteLink || 'получаем ссылку…' }}</div>
        <button class="btn" :disabled="!inviteLink" @click="copyInvite">🔗 Копировать ссылку</button>
      </template>
      <p v-else class="field-hint">
        Отправка пользователю и ссылка-приглашение доступны в серверном хранилище.
      </p>

      <label class="field-label">Скопировать URL</label>
      <button class="btn" @click="copyShareLinkUrl">📋 Копировать URL</button>

      <button class="btn" @click="shareLinkModal = null">Закрыть</button>
    </div>
  </div>
</template>

<style scoped>
.mode-note {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 10px;
}

.mode-link {
  color: var(--accent-color);
  margin-left: 6px;
}

.controls {
  display: flex;
  gap: 6px;
  margin-bottom: 12px;
}

.round-btn {
  flex: none;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  padding: 8px 10px;
  color: var(--text-color);
}

.search {
  flex: 1;
  min-width: 0;
}

.hint {
  text-align: center;
  color: var(--text-secondary);
  padding: 24px 0;
}

.hint-small {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 6px;
}

.pinned-block {
  background: var(--card-color);
  border-radius: 8px;
  padding: 8px 12px;
  margin-bottom: 12px;
}

.pinned-block h4 {
  margin: 0 0 4px;
  font-size: 14px;
}

.stats {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 8px;
}

.paste-fallback {
  border: 1px dashed var(--accent-color) !important;
}

.url-row {
  display: flex;
  gap: 6px;
  align-items: stretch;
}

.url-row input {
  flex: 1;
  min-width: 0;
}

.paste-btn {
  flex: none;
  margin-top: 8px;
  background: var(--bg-secondary);
  border: none;
  border-radius: 6px;
  padding: 0 10px;
}

.tag-suggest {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
}

.tag-chip {
  background: var(--bg-secondary);
  color: var(--accent-color);
  border: none;
  border-radius: 12px;
  padding: 3px 10px;
  font-size: 12px;
}

.field-label {
  display: block;
  font-size: 13px;
  color: var(--text-secondary);
  margin-top: 14px;
}

.field-hint {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 6px 0 0;
}

.invite-box {
  margin-top: 8px;
  padding: 8px 10px;
  border-radius: 8px;
  background: var(--bg-secondary);
  font-size: 12px;
  overflow-wrap: anywhere;
}

.btn:disabled {
  opacity: 0.5;
}

.btn-row {
  display: flex;
  gap: 8px;
}

.btn-row .btn {
  flex: 1;
}

.modal-content input,
.modal-content select {
  width: 100%;
  margin-top: 8px;
}

.check-line {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 10px;
  cursor: pointer;
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
</style>
