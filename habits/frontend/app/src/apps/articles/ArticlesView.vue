<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { showToast } from '../../shared/toast'
import * as artApi from './api'
import RecipientPicker from '../../components/RecipientPicker.vue'
import ArticleFolderNode from './components/ArticleFolderNode.vue'
import ArticleRow from './components/ArticleRow.vue'
import MarkdownView from './components/MarkdownView.vue'
import { diffLines, type DiffLine } from './diff'
import { fmtDateTime, fmtSize } from './types'
import type { Article, ArticleFolder, ArticleMeta, ArticleRevision, ContentHit, SharedTree } from './types'

const folders = ref<ArticleFolder[]>([])
const articles = ref<ArticleMeta[]>([])
const shared = ref<SharedTree[]>([])
const loading = ref(true)

// --- поиск: по заголовкам и категориям (мгновенно) или по контенту (сервер) ---
const searchQ = ref('')
const searchMode = ref<'meta' | 'content'>('meta')
const contentHits = ref<ContentHit[]>([])
const searching = ref(false)
let searchTimer: ReturnType<typeof setTimeout> | undefined

const metaResults = computed(() => {
  const q = searchQ.value.trim().toLowerCase()
  if (!q || searchMode.value !== 'meta') return null
  const allFolders = [...folders.value, ...shared.value.flatMap((t) => [t.root, ...t.folders])]
  const allArticles = [...articles.value, ...shared.value.flatMap((t) => t.articles)]
  return {
    folders: allFolders.filter((f) => f.name.toLowerCase().includes(q)),
    articles: allArticles.filter((a) => a.title.toLowerCase().includes(q)),
  }
})

function onSearchInput() {
  clearTimeout(searchTimer)
  if (searchMode.value !== 'content') return
  const q = searchQ.value.trim()
  if (q.length < 2) {
    contentHits.value = []
    return
  }
  searchTimer = setTimeout(async () => {
    searching.value = true
    try {
      contentHits.value = (await artApi.searchContent(q)).hits
    } catch {
      contentHits.value = []
    } finally {
      searching.value = false
    }
  }, 300)
}

function setSearchMode(m: 'meta' | 'content') {
  searchMode.value = m
  onSearchInput()
}

function openHit(id: number) {
  openArticleById(id)
}

// --- read-only статьи из доступных категорий ---
const sharedArticleIds = computed(() => new Set(shared.value.flatMap((t) => t.articles.map((a) => a.id))))

function sharedTreeFolders(t: SharedTree): ArticleFolder[] {
  return [t.root, ...t.folders]
}

const confirmLeaveId = ref<number | null>(null)

async function leaveSharedTree(t: SharedTree) {
  if (confirmLeaveId.value !== t.root.id) {
    confirmLeaveId.value = t.root.id
    showToast('Нажмите ✕ ещё раз, чтобы убрать категорию у себя')
    setTimeout(() => (confirmLeaveId.value = null), 3500)
    return
  }
  try {
    await artApi.leaveShared(t.root.id)
    shared.value = shared.value.filter((x) => x.root.id !== t.root.id)
    showToast('Категория убрана из доступных')
  } catch {
    showToast('Не удалось')
  }
}

// --- доступ к своей категории (в модалке папки) ---
interface ShareUser {
  id: number
  username: string
  first_name: string
}
const folderShares = ref<ShareUser[]>([])
const folderShareTo = ref('')
const folderShareBusy = ref(false)

async function loadFolderShares(folderId: number) {
  folderShares.value = []
  try {
    folderShares.value = (await artApi.fetchFolderShares(folderId)).users
  } catch {
    /* список просто пустой */
  }
}

async function grantFolderAccess() {
  const f = editingFolder.value
  const to = folderShareTo.value.trim()
  if (!f || !to) return
  folderShareBusy.value = true
  try {
    const { shared_to } = await artApi.shareFolderTo(f.id, to)
    if (!folderShares.value.some((u) => u.id === shared_to.id)) folderShares.value.push(shared_to)
    folderShareTo.value = ''
    showToast('Доступ открыт 📤')
  } catch {
    showToast('Пользователь не найден')
  } finally {
    folderShareBusy.value = false
  }
}

async function revokeFolderAccess(user: ShareUser) {
  const f = editingFolder.value
  if (!f) return
  try {
    await artApi.revokeFolderShare(f.id, user.id)
    folderShares.value = folderShares.value.filter((u) => u.id !== user.id)
  } catch {
    showToast('Не удалось отозвать')
  }
}

function shareUserLabel(u: ShareUser): string {
  return u.first_name || (u.username ? '@' + u.username : `#${u.id}`)
}

// подсказка по Markdown
const mdHelp = ref(false)

const OPEN_KEY = 'articles_open_folders'
const openSet = ref(new Set<number>(JSON.parse(localStorage.getItem(OPEN_KEY) || '[]')))

const rootFolders = computed(() => folders.value.filter((f) => f.parent_id === null))
const rootArticles = computed(() => articles.value.filter((a) => a.folder_id === null))

// --- читалка на весь экран ---
const reader = ref<Article | null>(null)
const readerLoading = ref(false)

// --- редактор ---
const editorOpen = ref(false)
const editing = ref<ArticleMeta | null>(null)
const form = ref({ title: '', content: '', folderId: null as number | null })
const confirmDelete = ref(false)

// --- меню ⋮ ---
const menuFor = ref<ArticleMeta | null>(null)
const menuPos = ref({ x: 0, y: 0 })

// --- модалка папки ---
const folderModal = ref(false)
const editingFolder = ref<ArticleFolder | null>(null)
const folderForm = ref({ name: '', parentId: null as number | null })
const confirmDeleteFolder = ref(false)

// --- шаринг ---
const shareFor = ref<ArticleMeta | null>(null)
const sendTo = ref('')
const inviteLink = ref('')
const downloadLink = ref('')
const readLink = ref('')
const sending = ref(false)

onMounted(load)

async function load() {
  try {
    const data = await artApi.fetchTree()
    folders.value = data.folders
    articles.value = data.articles
    shared.value = data.shared ?? []
  } catch {
    showToast('Не удалось загрузить статьи')
  } finally {
    loading.value = false
  }
}

function toggleFolder(id: number) {
  if (openSet.value.has(id)) openSet.value.delete(id)
  else openSet.value.add(id)
  localStorage.setItem(OPEN_KEY, JSON.stringify([...openSet.value]))
}

/** Плоский список папок с отступами для select. */
const folderOptions = computed(() => {
  const result: { id: number; label: string }[] = []
  const walk = (parentId: number | null, depth: number) => {
    for (const f of folders.value.filter((x) => x.parent_id === parentId)) {
      result.push({ id: f.id, label: `${'— '.repeat(depth)}📁 ${f.name}` })
      walk(f.id, depth + 1)
    }
  }
  walk(null, 0)
  return result
})

// --- читалка ---
const readerBody = ref<HTMLElement | null>(null)
let posTimer: ReturnType<typeof setTimeout> | undefined
let lastPos = 0

function openArticle(meta: ArticleMeta) {
  openArticleById(meta.id)
}

async function openArticleById(id: number) {
  readerLoading.value = true
  try {
    const { article, read_pos } = await artApi.fetchArticle(id)
    reader.value = article
    lastPos = 0
    // восстановление позиции — только после рендера контента
    readerLoading.value = false
    await nextTick()
    const el = readerBody.value
    if (el && read_pos > 0.01) {
      el.scrollTop = read_pos * (el.scrollHeight - el.clientHeight)
      lastPos = read_pos
    }
  } catch {
    showToast('Не удалось открыть статью')
    readerLoading.value = false
  }
}

function onReaderScroll() {
  const r = reader.value
  const el = readerBody.value
  if (!r || !el) return
  const max = el.scrollHeight - el.clientHeight
  lastPos = max > 0 ? Math.min(1, Math.max(0, el.scrollTop / max)) : 0
  clearTimeout(posTimer)
  const id = r.id
  const pos = lastPos
  posTimer = setTimeout(() => artApi.saveReadPos(id, pos).catch(() => {}), 800)
}

function closeReader() {
  const r = reader.value
  if (r) {
    clearTimeout(posTimer)
    artApi.saveReadPos(r.id, lastPos).catch(() => {})
  }
  reader.value = null
}

onUnmounted(() => clearTimeout(posTimer))

// двойной тап по месту в тексте → редактор, прокрученный к этому месту
function onReaderDblTap(ev: MouseEvent) {
  const r = reader.value
  if (!r || sharedArticleIds.value.has(r.id)) return
  const target = ev.target as HTMLElement
  const block = (target.closest('.md li') ?? target.closest('.md > *')) as HTMLElement | null
  const sample = (block?.textContent ?? '').trim()
  openEditAt(r, sample)
}

/** Открыть редактор и прокрутить к месту, где встречается sample. */
function openEditAt(article: Article, sample: string) {
  reader.value = null
  editing.value = article
  form.value = { title: article.title, content: article.content, folderId: article.folder_id }
  confirmDelete.value = false
  editorOpen.value = true
  nextTick(() => {
    const ta = document.querySelector<HTMLTextAreaElement>('.editor-textarea')
    if (!ta || !sample) return
    // отрендеренный текст ≠ сырой Markdown, поэтому ищем по началу блока,
    // постепенно укорачивая до первых слов
    const words = sample.split(/\s+/)
    let idx = -1
    for (let n = Math.min(words.length, 8); n >= 1 && idx < 0; n--) {
      idx = article.content.indexOf(words.slice(0, n).join(' '))
    }
    if (idx < 0) return
    ta.focus()
    ta.setSelectionRange(idx, idx)
    const before = article.content.slice(0, idx).split('\n').length
    const total = article.content.split('\n').length || 1
    ta.scrollTop = Math.max(0, (before / total) * ta.scrollHeight - ta.clientHeight / 3)
  })
}

// --- меню ⋮ ---
function openMenu(a: ArticleMeta, ev: MouseEvent) {
  menuFor.value = a
  const btn = (ev.currentTarget ?? ev.target) as HTMLElement
  const rect = btn.getBoundingClientRect()
  menuPos.value = { x: Math.max(8, rect.right - 180), y: rect.bottom + 4 }
}

// --- редактор ---
function openCreate(folderId: number | null = null) {
  editing.value = null
  form.value = { title: '', content: '', folderId }
  confirmDelete.value = false
  editorOpen.value = true
}

async function openEdit(meta: ArticleMeta) {
  menuFor.value = null
  try {
    const { article } = await artApi.fetchArticle(meta.id)
    editing.value = meta
    form.value = { title: article.title, content: article.content, folderId: article.folder_id }
    confirmDelete.value = false
    editorOpen.value = true
  } catch {
    showToast('Не удалось открыть статью')
  }
}

async function save() {
  const { title, content, folderId } = form.value
  if (!title.trim()) {
    showToast('Введите заголовок')
    return
  }
  try {
    if (editing.value) {
      const { article } = await artApi.updateArticle(editing.value.id, {
        title: title.trim(),
        content,
        folder_id: folderId,
        set_folder: true,
      })
      const i = articles.value.findIndex((x) => x.id === article.id)
      if (i >= 0) articles.value[i] = article
      if (reader.value?.id === article.id) reader.value = article
    } else {
      const { article } = await artApi.createArticle(title.trim(), content, folderId)
      articles.value.push(article)
    }
    editorOpen.value = false
  } catch {
    showToast('Не удалось сохранить')
  }
}

async function remove() {
  if (!editing.value) return
  if (!confirmDelete.value) {
    confirmDelete.value = true
    setTimeout(() => (confirmDelete.value = false), 3500)
    return
  }
  try {
    await artApi.deleteArticle(editing.value.id)
    articles.value = articles.value.filter((x) => x.id !== editing.value!.id)
    editorOpen.value = false
  } catch {
    showToast('Не удалось удалить')
  }
}

// --- копирование статьи в буфер ---
async function copyArticle(meta: ArticleMeta) {
  menuFor.value = null
  try {
    const { article } = await artApi.fetchArticle(meta.id)
    await navigator.clipboard.writeText(`# ${article.title}\n\n${article.content}`)
    showToast('Скопировано')
  } catch {
    showToast('Не удалось скопировать')
  }
}

// --- информация о статье ---
const infoFor = ref<ArticleMeta | null>(null)
const info = ref<{ size: number; chars: number } | null>(null)

async function openInfo(meta: ArticleMeta) {
  menuFor.value = null
  infoFor.value = meta
  info.value = null
  try {
    const { article } = await artApi.fetchArticle(meta.id)
    info.value = {
      size: new Blob([article.content]).size,
      chars: [...article.content].length,
    }
  } catch {
    /* покажем только даты */
  }
}

// --- история изменений (diff относительно текущей версии) ---
const historyFor = ref<ArticleMeta | null>(null)
const revisions = ref<ArticleRevision[]>([])
const historyLoading = ref(false)
const historyContent = ref('') // текущий content для сравнения
const diffRev = ref<ArticleRevision | null>(null)
const diff = ref<DiffLine[]>([])
const diffLoading = ref(false)

async function openHistory(meta: ArticleMeta) {
  menuFor.value = null
  historyFor.value = meta
  diffRev.value = null
  revisions.value = []
  historyLoading.value = true
  try {
    const [h, art] = await Promise.all([artApi.fetchHistory(meta.id), artApi.fetchArticle(meta.id)])
    revisions.value = h.revisions
    historyContent.value = art.article.content
  } catch {
    showToast('Не удалось загрузить историю')
    historyFor.value = null
  } finally {
    historyLoading.value = false
  }
}

async function openDiff(rev: ArticleRevision) {
  diffLoading.value = true
  try {
    const { content } = await artApi.fetchRevision(rev.id)
    diff.value = diffLines(content, historyContent.value)
    diffRev.value = rev
  } catch {
    showToast('Не удалось загрузить ревизию')
  } finally {
    diffLoading.value = false
  }
}

// --- вставка картинки в редакторе ---
const imgInput = ref<HTMLInputElement | null>(null)
const imgUploading = ref(false)

async function onImagePicked(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  imgUploading.value = true
  try {
    const { url } = await artApi.uploadArticleImage(file)
    insertAtCursor(`\n![картинка](${import.meta.env.BASE_URL}${url})\n`)
    showToast('Картинка добавлена в текст')
  } catch {
    showToast('Не удалось загрузить (jpeg/png/webp/gif, до 5 МБ)')
  } finally {
    imgUploading.value = false
  }
}

function insertAtCursor(text: string) {
  const ta = document.querySelector<HTMLTextAreaElement>('.editor-textarea')
  const pos = ta?.selectionStart ?? form.value.content.length
  form.value.content = form.value.content.slice(0, pos) + text + form.value.content.slice(pos)
  nextTick(() => {
    ta?.focus()
    ta?.setSelectionRange(pos + text.length, pos + text.length)
  })
}

// --- скачивание (локально, из меню) ---
async function download(meta: ArticleMeta) {
  menuFor.value = null
  try {
    const { article } = await artApi.fetchArticle(meta.id)
    const blob = new Blob([`# ${article.title}\n\n${article.content}`], {
      type: 'text/markdown;charset=utf-8',
    })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = article.title.replace(/[/\\:*?"<>|]/g, '_') + '.md'
    a.click()
    URL.revokeObjectURL(a.href)
  } catch {
    showToast('Не удалось скачать')
  }
}

// --- шаринг ---
async function openShare(a: ArticleMeta) {
  menuFor.value = null
  shareFor.value = a
  sendTo.value = ''
  inviteLink.value = ''
  downloadLink.value = ''
  readLink.value = ''
  try {
    const [share, dl, rd] = await Promise.all([
      artApi.shareToken(a.id),
      artApi.downloadToken(a.id),
      artApi.readToken(a.id),
    ])
    inviteLink.value = share.link || `art_${share.token}`
    downloadLink.value = location.origin + import.meta.env.BASE_URL + dl.path
    readLink.value = location.origin + import.meta.env.BASE_URL + rd.path
  } catch {
    showToast('Не удалось получить ссылки')
  }
}

async function copyText(text: string, what: string) {
  try {
    await navigator.clipboard.writeText(text)
    showToast(`${what} скопирована`)
  } catch {
    showToast('Не удалось скопировать')
  }
}

async function send() {
  const a = shareFor.value
  const to = sendTo.value.trim()
  if (!a || !to) return
  sending.value = true
  try {
    const { sent_to } = await artApi.sendArticle(a.id, to)
    showToast(`Отправлено ${sent_to.first_name || '@' + sent_to.username || '#' + sent_to.id} 📤`)
    shareFor.value = null
  } catch {
    showToast('Пользователь не найден или ошибка отправки')
  } finally {
    sending.value = false
  }
}

// --- папки ---
function openAddFolder() {
  editingFolder.value = null
  folderForm.value = { name: '', parentId: null }
  confirmDeleteFolder.value = false
  folderModal.value = true
}

function openEditFolder(f: ArticleFolder) {
  editingFolder.value = f
  folderForm.value = { name: f.name, parentId: f.parent_id }
  confirmDeleteFolder.value = false
  folderShareTo.value = ''
  folderModal.value = true
  loadFolderShares(f.id)
}

async function saveFolder() {
  const name = folderForm.value.name.trim()
  if (!name) return
  try {
    if (editingFolder.value) {
      const { folder } = await artApi.updateFolder(editingFolder.value.id, {
        name,
        parent_id: folderForm.value.parentId,
        set_parent: true,
      })
      const i = folders.value.findIndex((x) => x.id === folder.id)
      if (i >= 0) folders.value[i] = folder
    } else {
      const { folder } = await artApi.createFolder(name, folderForm.value.parentId)
      folders.value.push(folder)
      openSet.value.add(folder.id)
    }
    folderModal.value = false
  } catch {
    showToast('Не удалось сохранить папку')
  }
}

async function removeFolder() {
  const f = editingFolder.value
  if (!f) return
  if (!confirmDeleteFolder.value) {
    confirmDeleteFolder.value = true
    setTimeout(() => (confirmDeleteFolder.value = false), 3500)
    return
  }
  try {
    await artApi.deleteFolder(f.id)
    await load() // каскад мог удалить вложенное — перечитываем
    folderModal.value = false
  } catch {
    showToast('Не удалось удалить папку')
  }
}
</script>

<template>
  <div v-if="loading" class="hint">Загрузка…</div>

  <template v-else>
    <div class="toolbar">
      <button class="btn small" @click="openCreate(null)">＋📄 Статья</button>
      <button class="btn small" @click="openAddFolder">＋📁 Категория</button>
    </div>

    <input
      v-model="searchQ"
      type="search"
      class="search"
      placeholder="🔍 Поиск…"
      @input="onSearchInput"
    />
    <div v-if="searchQ.trim()" class="search-modes">
      <button :class="{ on: searchMode === 'meta' }" @click="setSearchMode('meta')">Названия и категории</button>
      <button :class="{ on: searchMode === 'content' }" @click="setSearchMode('content')">По содержимому</button>
    </div>

    <!-- поиск: названия и категории -->
    <template v-if="metaResults">
      <p v-if="metaResults.folders.length === 0 && metaResults.articles.length === 0" class="hint">
        Ничего не найдено
      </p>
      <div v-for="f in metaResults.folders" :key="'f' + f.id" class="hit-folder">📁 {{ f.name }}</div>
      <ArticleRow
        v-for="a in metaResults.articles"
        :key="a.id"
        :article="a"
        @open="openArticle(a)"
        @menu="openMenu(a, $event)"
      />
    </template>

    <!-- поиск: по содержимому -->
    <template v-else-if="searchQ.trim() && searchMode === 'content'">
      <p v-if="searching" class="hint">Ищем…</p>
      <p v-else-if="contentHits.length === 0" class="hint">Ничего не найдено</p>
      <button v-for="h in contentHits" :key="h.id" class="content-hit" @click="openHit(h.id)">
        <span class="hit-title">📄 {{ h.title }}</span>
        <span class="hit-snippet">…{{ h.snippet }}…</span>
      </button>
    </template>

    <!-- обычное дерево -->
    <template v-else>
      <p v-if="articles.length === 0 && folders.length === 0 && shared.length === 0" class="hint">
        Здесь живут заметки и статьи с разметкой Markdown — как README на GitHub 👇
      </p>

      <ArticleFolderNode
        v-for="f in rootFolders"
        :key="f.id"
        :folder="f"
        :folders="folders"
        :articles="articles"
        :open-set="openSet"
        :level="0"
        @toggle="toggleFolder"
        @edit-folder="openEditFolder"
        @add-to="openCreate"
        @open="openArticle"
        @menu="openMenu"
      />
      <ArticleRow
        v-for="a in rootArticles"
        :key="a.id"
        :article="a"
        @open="openArticle(a)"
        @menu="openMenu(a, $event)"
      />

      <!-- доступные мне чужие категории (живой доступ, read-only) -->
      <template v-if="shared.length > 0">
        <div class="shared-head">📥 Доступные мне</div>
        <div v-for="t in shared" :key="t.root.id" class="shared-tree">
          <div class="shared-owner">
            от {{ t.owner.first_name || '@' + t.owner.username || '#' + t.owner.id }}
            <button
              class="leave-btn"
              :title="confirmLeaveId === t.root.id ? 'Подтвердить' : 'Убрать у себя'"
              @click="leaveSharedTree(t)"
            >
              {{ confirmLeaveId === t.root.id ? 'точно?' : '✕' }}
            </button>
          </div>
          <ArticleFolderNode
            :folder="t.root"
            :folders="sharedTreeFolders(t)"
            :articles="t.articles"
            :open-set="openSet"
            :level="0"
            @toggle="toggleFolder"
            @edit-folder="showToast('Категория доступна только для чтения')"
            @add-to="showToast('Категория доступна только для чтения')"
            @open="openArticle"
            @menu="openMenu"
          />
        </div>
      </template>
    </template>
  </template>

  <!-- выпадающее меню ⋮ (для доступных read-only статей — только скачивание) -->
  <div v-if="menuFor" class="menu-overlay" @click="menuFor = null">
    <div class="dropdown" :style="{ left: menuPos.x + 'px', top: menuPos.y + 'px' }" @click.stop>
      <button v-if="!sharedArticleIds.has(menuFor.id)" @click="openEdit(menuFor!)">✏️ Изменить</button>
      <button @click="copyArticle(menuFor!)">📋 Копировать</button>
      <button @click="download(menuFor!)">⬇️ Скачать</button>
      <button v-if="!sharedArticleIds.has(menuFor.id)" @click="openShare(menuFor!)">📤 Поделиться</button>
      <button v-if="!sharedArticleIds.has(menuFor.id)" @click="openHistory(menuFor!)">🕓 История</button>
      <button @click="openInfo(menuFor!)">ℹ️ Информация</button>
    </div>
  </div>

  <!-- читалка на весь экран -->
  <div v-if="reader || readerLoading" class="reader">
    <div class="reader-head">
      <h2 class="reader-title">{{ reader?.title ?? '…' }}</h2>
      <button class="reader-close" title="Закрыть" @click="closeReader">✕</button>
    </div>
    <div ref="readerBody" class="reader-body" @scroll.passive="onReaderScroll" @dblclick="onReaderDblTap">
      <p v-if="readerLoading" class="hint">Загрузка…</p>
      <MarkdownView v-else-if="reader" :source="reader.content" />
    </div>
  </div>

  <!-- редактор на весь экран -->
  <div v-if="editorOpen" class="reader editor-screen">
    <div class="reader-head">
      <h2 class="reader-title">{{ editing ? '✏️ Редактирование' : '📄 Новая статья' }}</h2>
      <span class="head-btns">
        <button
          class="reader-close"
          :title="imgUploading ? 'Загрузка…' : 'Вставить картинку'"
          :disabled="imgUploading"
          @click="imgInput?.click()"
        >
          {{ imgUploading ? '⏳' : '🖼' }}
        </button>
        <button class="reader-close help-q" title="Правила разметки" @click="mdHelp = true">?</button>
        <button class="reader-close" title="Закрыть" @click="editorOpen = false">✕</button>
      </span>
      <input
        ref="imgInput"
        type="file"
        accept="image/jpeg,image/png,image/webp,image/gif"
        style="display: none"
        @change="onImagePicked"
      />
    </div>
    <div class="editor-body">
      <input v-model="form.title" placeholder="Заголовок" maxlength="300" />
      <select v-model="form.folderId">
        <option :value="null">🏠 Без категории</option>
        <option v-for="opt in folderOptions" :key="opt.id" :value="opt.id">{{ opt.label }}</option>
      </select>
      <textarea
        v-model="form.content"
        class="editor-textarea"
        placeholder="Текст в Markdown: # заголовки, **жирный**, списки, ```код```, таблицы… (правила — по кнопке ?)"
        spellcheck="false"
      ></textarea>
      <button class="btn primary" @click="save">💾 Сохранить</button>
      <button v-if="editing" class="btn danger" @click="remove">
        {{ confirmDelete ? 'Точно удалить статью?' : '🗑 Удалить' }}
      </button>
    </div>
  </div>

  <!-- правила разметки Markdown -->
  <div v-if="mdHelp" class="modal md-help-modal" @click.self="mdHelp = false">
    <div class="modal-content editor md-help">
      <h3>Разметка Markdown</h3>
      <pre class="md-cheats">
# Заголовок 1
## Заголовок 2

**жирный**  *курсив*  ~~зачёркнутый~~
`код в строке`

- список
- ещё пункт
1. нумерованный

- [ ] задача
- [x] сделана

[ссылка](https://example.com)
![картинка](https://…/img.png)

> цитата

```
блок кода
```

| Колонка | Колонка |
|---------|---------|
| ячейка  | ячейка  |

--- (разделитель)</pre>
      <button class="btn" @click="mdHelp = false">Понятно</button>
    </div>
  </div>

  <!-- модалка папки -->
  <div v-if="folderModal" class="modal" @click.self="folderModal = false">
    <div class="modal-content editor">
      <h3>{{ editingFolder ? 'Настройки категории' : 'Новая категория' }}</h3>
      <input v-model="folderForm.name" placeholder="Название" maxlength="200" />
      <select v-model="folderForm.parentId">
        <option :value="null">🏠 Корень</option>
        <option
          v-for="opt in folderOptions.filter((o) => o.id !== editingFolder?.id)"
          :key="opt.id"
          :value="opt.id"
        >
          {{ opt.label }}
        </option>
      </select>
      <template v-if="editingFolder">
        <label class="field-label">📤 Доступ пользователям (видят категорию вживую, только чтение)</label>
        <div v-if="folderShares.length" class="share-chips">
          <span v-for="u in folderShares" :key="u.id" class="chip">
            {{ shareUserLabel(u) }}
            <button class="chip-x" @click="revokeFolderAccess(u)">✕</button>
          </span>
        </div>
        <RecipientPicker v-model="folderShareTo" />
        <button
          class="btn grant-btn"
          :disabled="folderShareBusy || !folderShareTo.trim()"
          @click="grantFolderAccess"
        >
          {{ folderShareBusy ? 'Открываем…' : '📤 Открыть доступ' }}
        </button>
      </template>

      <button class="btn primary" @click="saveFolder">💾 Сохранить</button>
      <button v-if="editingFolder" class="btn danger" @click="removeFolder">
        {{ confirmDeleteFolder ? 'Удалить со всем содержимым?' : '🗑 Удалить' }}
      </button>
      <button class="btn" @click="folderModal = false">Отмена</button>
    </div>
  </div>

  <!-- шаринг -->
  <div v-if="shareFor" class="modal" @click.self="shareFor = null">
    <div class="modal-content editor">
      <h3>Поделиться «{{ shareFor.title }}»</h3>

      <label class="field-label">Пользователю приложения</label>
      <RecipientPicker v-model="sendTo" />
      <button class="btn primary" :disabled="sending || !sendTo.trim()" @click="send">
        {{ sending ? 'Отправка…' : '📤 Отправить' }}
      </button>

      <label class="field-label">Ссылка-приглашение (откроет статью в приложении)</label>
      <div class="link-box">{{ inviteLink || '…' }}</div>
      <button class="btn" :disabled="!inviteLink" @click="copyText(inviteLink, 'Ссылка')">🔗 Копировать</button>

      <label class="field-label">Ссылка на чтение (страница в браузере, без приложения)</label>
      <div class="link-box">{{ readLink || '…' }}</div>
      <button class="btn" :disabled="!readLink" @click="copyText(readLink, 'Ссылка')">📖 Копировать</button>

      <label class="field-label">Ссылка для скачивания .md (работает без приложения)</label>
      <div class="link-box">{{ downloadLink || '…' }}</div>
      <button class="btn" :disabled="!downloadLink" @click="copyText(downloadLink, 'Ссылка')">⬇️ Копировать</button>

      <button class="btn" @click="shareFor = null">Закрыть</button>
    </div>
  </div>

  <!-- информация о статье -->
  <div v-if="infoFor" class="modal" @click.self="infoFor = null">
    <div class="modal-content editor">
      <h3>ℹ️ «{{ infoFor.title }}»</h3>
      <div class="info-rows">
        <div class="info-row"><span>Размер</span><b>{{ info ? fmtSize(info.size) : '…' }}</b></div>
        <div class="info-row"><span>Символов</span><b>{{ info ? info.chars.toLocaleString('ru-RU') : '…' }}</b></div>
        <div class="info-row"><span>Создана</span><b>{{ fmtDateTime(infoFor.created_at) }}</b></div>
        <div class="info-row"><span>Изменена</span><b>{{ fmtDateTime(infoFor.updated_at) }}</b></div>
      </div>
      <button class="btn" @click="infoFor = null">Закрыть</button>
    </div>
  </div>

  <!-- история изменений: список ревизий и diff относительно текущей -->
  <div v-if="historyFor" class="reader history-screen">
    <div class="reader-head">
      <h2 class="reader-title">🕓 {{ diffRev ? fmtDateTime(diffRev.saved_at) : `История «${historyFor.title}»` }}</h2>
      <span class="head-btns">
        <button v-if="diffRev" class="reader-close" title="К списку" @click="diffRev = null">←</button>
        <button class="reader-close" title="Закрыть" @click="historyFor = null">✕</button>
      </span>
    </div>
    <div class="reader-body">
      <template v-if="!diffRev">
        <p v-if="historyLoading" class="hint">Загрузка…</p>
        <p v-else-if="revisions.length === 0" class="hint">
          История пуста — она наполняется при сохранении изменений текста
        </p>
        <button v-for="rev in revisions" :key="rev.id" class="rev-row" @click="openDiff(rev)">
          <span>{{ fmtDateTime(rev.saved_at) }}</span>
          <span class="rev-size">{{ fmtSize(rev.size) }}</span>
        </button>
        <p v-if="revisions.length" class="hint small-hint">
          Нажмите на версию — увидите, что изменилось с неё до текущей
        </p>
      </template>
      <template v-else>
        <p v-if="diffLoading" class="hint">Сравниваем…</p>
        <div v-else class="diff">
          <p v-if="diff.length === 0" class="hint">Текст не отличается от текущего</p>
          <div v-for="(l, i) in diff" :key="i" class="diff-line" :class="l.type">
            <span class="diff-sign">{{ l.type === 'add' ? '+' : l.type === 'del' ? '−' : ' ' }}</span>
            <span class="diff-text">{{ l.text || ' ' }}</span>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.hint {
  text-align: center;
  color: var(--text-secondary);
  padding: 24px 0;
}

.toolbar {
  display: flex;
  gap: 6px;
  margin-bottom: 12px;
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
  font-size: 13px;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn.danger {
  background: #b91c1c;
  color: #fff;
}

.search {
  width: 100%;
  margin-bottom: 8px;
}

.search-modes {
  display: flex;
  gap: 6px;
  margin-bottom: 10px;
}

.search-modes button {
  flex: 1;
  padding: 7px 4px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: 12px;
}

.search-modes button.on {
  background: var(--accent-color);
  color: #fff;
}

.hit-folder {
  padding: 8px 4px;
  font-weight: 600;
  font-size: 14px;
}

.content-hit {
  display: block;
  width: 100%;
  text-align: left;
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  padding: 9px 12px;
  margin-bottom: 6px;
  color: var(--text-color);
}

.hit-title {
  display: block;
  font-weight: 600;
  font-size: 14px;
}

.hit-snippet {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
  overflow-wrap: anywhere;
  margin-top: 2px;
}

.shared-head {
  margin: 18px 0 6px;
  font-weight: 700;
  font-size: 14px;
  color: var(--text-secondary);
}

.shared-tree {
  margin-bottom: 6px;
}

.shared-owner {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 11px;
  color: var(--text-secondary);
  padding: 0 2px;
}

.leave-btn {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 12px;
  padding: 2px 6px;
}

.head-btns {
  display: flex;
  gap: 8px;
  flex: none;
}

.help-q {
  font-weight: 700;
}

.editor-screen {
  z-index: 1250;
}

.editor-body {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 16px 20px;
  overflow-y: auto;
}

.editor-body input,
.editor-body select {
  width: 100%;
}

.editor-textarea {
  flex: 1;
  min-height: 40vh;
  width: 100%;
  resize: none;
  font-family: monospace;
  font-size: 13px;
}

.md-help-modal {
  z-index: 1300;
}

.md-cheats {
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 10px 12px;
  font-size: 12px;
  overflow-x: auto;
  max-height: 55vh;
  overflow-y: auto;
  margin: 8px 0 0;
}

.share-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
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

.grant-btn {
  font-size: 13px;
}

.menu-overlay {
  position: fixed;
  inset: 0;
  z-index: 1100;
}

.dropdown {
  position: fixed;
  width: 180px;
  background: var(--card-color);
  border-radius: 10px;
  box-shadow: 0 6px 24px rgba(0, 0, 0, 0.35);
  overflow: hidden;
  z-index: 1101;
}

.dropdown button {
  display: block;
  width: 100%;
  padding: 11px 14px;
  background: none;
  border: none;
  border-bottom: 1px solid var(--bg-secondary);
  color: var(--text-color);
  font-size: 14px;
  text-align: left;
}

.dropdown button:last-child {
  border-bottom: none;
}

.reader {
  position: fixed;
  inset: 0;
  z-index: 1200;
  background: var(--bg-color);
  display: flex;
  flex-direction: column;
}

.reader-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--hover-bg-color);
  background: var(--card-color);
}

.reader-title {
  margin: 0;
  font-size: 17px;
  min-width: 0;
  overflow-wrap: anywhere;
}

.reader-close {
  flex: none;
  background: var(--bg-secondary);
  border: none;
  border-radius: 50%;
  width: 32px;
  height: 32px;
  color: var(--text-color);
  font-size: 15px;
}

.reader-body {
  flex: 1;
  overflow-y: auto;
  padding: 14px 16px 40px;
}

.modal-content.editor {
  text-align: left;
  max-height: 88vh;
  overflow-y: auto;
}

.modal-content.editor h3 {
  text-align: center;
}

.modal-content.editor input,
.modal-content.editor select,
.modal-content.editor textarea {
  width: 100%;
  margin-top: 8px;
}

.modal-content.editor textarea {
  resize: vertical;
  font-family: monospace;
  font-size: 13px;
}

.modal-content.editor .btn {
  display: block;
  width: 100%;
  margin-top: 10px;
}

.field-label {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 12px;
}

.link-box {
  margin-top: 6px;
  padding: 8px 10px;
  background: var(--bg-secondary);
  border-radius: 8px;
  font-size: 12px;
  font-family: monospace;
  overflow-wrap: anywhere;
  color: var(--text-secondary);
}

.info-rows {
  margin-top: 10px;
}

.info-row {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  padding: 7px 0;
  border-bottom: 1px solid var(--bg-secondary);
  font-size: 14px;
}

.info-row span {
  color: var(--text-secondary);
}

.history-screen {
  z-index: 1260;
}

.rev-row {
  display: flex;
  width: 100%;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  padding: 11px 14px;
  margin-bottom: 6px;
  color: var(--text-color);
  font-size: 14px;
}

.rev-size {
  color: var(--text-secondary);
  font-size: 12px;
}

.small-hint {
  font-size: 12px;
  padding: 8px 0;
}

.diff {
  font-family: monospace;
  font-size: 12px;
  background: var(--card-color);
  border-radius: 8px;
  padding: 8px 0;
  overflow-x: auto;
}

.diff-line {
  display: flex;
  white-space: pre;
  padding: 1px 10px;
}

.diff-sign {
  flex: none;
  width: 14px;
  user-select: none;
}

.diff-line.add {
  background: rgba(34, 197, 94, 0.14);
  color: #22c55e;
}

.diff-line.del {
  background: rgba(239, 68, 68, 0.12);
  color: #ef4444;
}

.diff-line.skip {
  color: var(--text-secondary);
  padding: 4px 10px;
}

.diff-text {
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
</style>
