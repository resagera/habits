<script setup lang="ts">
/**
 * Сейф: файлы шифруются в браузере паролем папки, сервер хранит только
 * шифробайты (habits/PLAN-vault.md). Ключи живут в памяти страницы минуту,
 * поэтому почти любое действие начинается с проверки «папка открыта?».
 */
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { showToast } from '../../shared/toast'
import * as vaultApi from './api'
import {
  CHUNK_SIZE,
  decryptFile,
  decryptThumb,
  encryptFile,
  encryptThumb,
  openMeta,
  rewrapContentKey,
  sealMeta,
  toB64,
} from './crypto'
import {
  installAutoLock,
  isUnlocked,
  keyFor,
  lockAll,
  lockFolder,
  lockVersion,
  tryKnownPasswords,
  unlock,
} from './session'
import type { FileMeta, VaultFile, VaultFolder, VaultQuota } from './types'
import { zipFiles } from './office'
import FileActions from './components/FileActions.vue'
import FileViewer from './components/FileViewer.vue'
import FolderModal from './components/FolderModal.vue'
import QuotaBar from './components/QuotaBar.vue'

const route = useRoute()

const folders = ref<VaultFolder[]>([])
const files = ref<VaultFile[]>([])
const quota = ref<VaultQuota>({ used: 0, total_limit: 0, file_limit: 0 })
const loading = ref(true)

const currentId = ref<number | null>(null)
const password = ref('')
const unlocking = ref(false)

// расшифрованные имена и превью — только в памяти, гибнут вместе с ключом
const metas = ref(new Map<number, FileMeta>())
const thumbs = ref(new Map<number, string>())
const selected = ref(new Set<number>())
const selectMode = ref(false)
const query = ref('')
const uploading = ref<{ name: string; pct: number } | null>(null)
const exporting = ref(false)
const confirmDelete = ref(false)

const folderModal = ref<{ folder: VaultFolder | null } | null>(null)
const viewerIndex = ref<number | null>(null)
const actionsFile = ref<VaultFile | null>(null)

const current = computed(() => folders.value.find((f) => f.id === currentId.value) ?? null)
const children = computed(() => {
  // косметика: пока папка заперта, вложенные не показываем (сервер их всё
  // равно знает — об этом прямо сказано у галочки в настройках папки)
  if (current.value?.hide_children && !unlocked.value) return []
  return folders.value.filter((f) => f.parent_id === (currentId.value ?? null))
})
const currentFiles = computed(() =>
  currentId.value ? files.value.filter((f) => f.folder_id === currentId.value) : [],
)
// поиск идёт по расшифрованным именам и заметкам — они есть только в памяти
const visibleFiles = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return currentFiles.value
  return currentFiles.value.filter((f) => {
    const m = metas.value.get(f.id)
    return `${m?.name ?? ''} ${m?.note ?? ''}`.toLowerCase().includes(q)
  })
})
const viewerFile = computed(() =>
  viewerIndex.value === null ? null : (visibleFiles.value[viewerIndex.value] ?? null),
)
// lockVersion в зависимостях специально: Map ключей не реактивна
const unlocked = computed(() => (lockVersion.value, currentId.value ? isUnlocked(currentId.value) : false))
const path = computed(() => {
  const out: VaultFolder[] = []
  let f = current.value
  while (f) {
    out.unshift(f)
    f = folders.value.find((p) => p.id === f!.parent_id) ?? null
  }
  return out
})

let stopAutoLock: (() => void) | undefined

onMounted(async () => {
  stopAutoLock = installAutoLock()
  await load()
  // ссылка вида /vault?file=123 — из «Открыть в браузере» для PDF
  const id = Number(route.query.file)
  if (id) {
    const file = files.value.find((f) => f.id === id)
    if (file) currentId.value = file.folder_id
  }
})

onUnmounted(() => {
  stopAutoLock?.()
  for (const url of thumbs.value.values()) URL.revokeObjectURL(url)
  lockAll() // ушли со страницы — ключи не остаются висеть
})

async function load() {
  try {
    const data = await vaultApi.fetchVault()
    folders.value = data.folders
    files.value = data.files
    quota.value = data.quota
  } catch {
    showToast('Не удалось загрузить сейф')
  } finally {
    loading.value = false
  }
}

async function openFolder(f: VaultFolder) {
  currentId.value = f.id
  selected.value = new Set()
  password.value = ''
  // тот же пароль, что у уже открытой папки, подходит часто — пробуем молча
  if (!isUnlocked(f.id)) await tryKnownPasswords(f)
  if (isUnlocked(f.id)) await decryptFolder(f)
}

async function submitPassword() {
  const f = current.value
  if (!f || unlocking.value) return
  unlocking.value = true
  try {
    if (!(await unlock(f, password.value))) {
      showToast('Пароль не подходит')
      return
    }
    password.value = ''
    await decryptFolder(f)
  } finally {
    unlocking.value = false
  }
}

/** Расшифровать имена файлов и подтянуть превью. */
async function decryptFolder(f: VaultFolder) {
  const key = keyFor(f.id)
  if (!key) return
  for (const file of files.value.filter((x) => x.folder_id === f.id)) {
    const meta = await openMeta(key, file.meta_env)
    if (meta) metas.value.set(file.id, meta)
    if (file.has_thumb && !thumbs.value.has(file.id)) void loadThumb(file, key)
  }
  metas.value = new Map(metas.value)
}

async function loadThumb(file: VaultFile, key: CryptoKey) {
  try {
    const raw = await vaultApi.fetchBlob(file.id, true)
    const blob = await decryptThumb(key, file.key_env, raw)
    if (blob) {
      thumbs.value.set(file.id, URL.createObjectURL(blob))
      thumbs.value = new Map(thumbs.value)
    }
  } catch {
    /* превью необязательно */
  }
}

function lockCurrent() {
  if (!currentId.value) return
  lockFolder(currentId.value)
  forgetDecrypted()
}

function lockEverything() {
  lockAll()
  forgetDecrypted()
}

/** Забыть всё расшифрованное: имена, превью, открытый просмотрщик. */
function forgetDecrypted() {
  for (const url of thumbs.value.values()) URL.revokeObjectURL(url)
  thumbs.value = new Map()
  metas.value = new Map()
  viewerIndex.value = null
  actionsFile.value = null
  selected.value = new Set()
  selectMode.value = false
}

function metaOf(file: VaultFile): FileMeta {
  return metas.value.get(file.id) ?? { name: '…', type: '', size: file.plain_size }
}

function fmtSize(bytes: number): string {
  if (bytes >= 1 << 20) return (bytes / (1 << 20)).toFixed(1) + ' МБ'
  if (bytes >= 1 << 10) return Math.round(bytes / (1 << 10)) + ' КБ'
  return bytes + ' Б'
}

function icon(file: VaultFile): string {
  const t = metaOf(file).type
  if (t.startsWith('image/')) return '🖼'
  if (t.startsWith('audio/')) return '🎵'
  if (t.startsWith('video/')) return '🎬'
  if (t === 'application/pdf') return '📕'
  if (t.startsWith('text/')) return '📝'
  return '📎'
}

// --- загрузка файлов ---

async function makeThumb(file: File): Promise<Blob | null> {
  if (!file.type.startsWith('image/')) return null
  try {
    const bitmap = await createImageBitmap(file)
    const k = Math.min(1, 320 / Math.max(bitmap.width, bitmap.height))
    const canvas = document.createElement('canvas')
    canvas.width = Math.round(bitmap.width * k)
    canvas.height = Math.round(bitmap.height * k)
    canvas.getContext('2d')?.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
    return await new Promise((res) => canvas.toBlob((b) => res(b), 'image/webp', 0.8))
  } catch {
    return null // формат, который браузер не декодирует — переживём без превью
  }
}

async function uploadFiles(list: FileList | File[]) {
  const folder = current.value
  const key = folder ? keyFor(folder.id) : null
  if (!folder || !key) return showToast('Сначала откройте папку паролем')
  for (const file of Array.from(list)) {
    if (file.size > quota.value.file_limit) {
      showToast(`«${file.name}» больше лимита на один файл`)
      continue
    }
    uploading.value = { name: file.name, pct: 0 }
    try {
      const enc = await encryptFile(file, key, (done, total) => {
        uploading.value = { name: file.name, pct: Math.round((done / total) * 50) }
      })
      const { upload_id } = await vaultApi.initUpload({
        folder_id: folder.id,
        plain_size: enc.plainSize,
        chunk_size: CHUNK_SIZE,
      })
      for (let i = 0, retries = 0; i < enc.chunks.length; ) {
        try {
          await vaultApi.uploadChunk(upload_id, enc.chunks[i])
          i++
          retries = 0
        } catch (err) {
          // связь оборвалась: спрашиваем сервер, сколько байт дошло, и
          // продолжаем с этого места, а не начинаем файл заново
          const from = ++retries <= 4 ? await resumePoint(upload_id, enc.chunks) : null
          if (from === null) throw err
          i = from
          uploading.value = { name: file.name, pct: 50 + Math.round((i / enc.chunks.length) * 50) }
          await new Promise((res) => setTimeout(res, 500 * retries))
          continue
        }
        uploading.value = { name: file.name, pct: 50 + Math.round((i / enc.chunks.length) * 50) }
      }
      let thumb: string | undefined
      if (folder.thumbs) {
        const blob = await makeThumb(file)
        if (blob) {
          const sealed = await encryptThumb(key, enc.keyEnv, blob)
          if (sealed) thumb = toB64(sealed)
        }
      }
      const { file: created } = await vaultApi.finishUpload(upload_id, {
        key_env: enc.keyEnv,
        meta_env: enc.metaEnv,
        thumb,
      })
      files.value = [created, ...files.value]
      metas.value.set(created.id, { name: file.name, type: file.type, size: file.size })
      metas.value = new Map(metas.value)
      if (thumb) void loadThumb(created, key)
      quota.value = { ...quota.value, used: quota.value.used + file.size }
    } catch (e) {
      showToast(e instanceof Error && e.message ? e.message : `Не удалось загрузить «${file.name}»`)
    } finally {
      uploading.value = null
    }
  }
}

/**
 * С какого чанка продолжать. Сервер пишет чанк целиком или никак (тело
 * читается в память до записи), поэтому счётчик всегда стоит на границе;
 * если вдруг нет — возобновлять нельзя, честно падаем.
 */
async function resumePoint(uploadId: string, chunks: Uint8Array[]): Promise<number | null> {
  try {
    const { written } = await vaultApi.uploadStatus(uploadId)
    let acc = 0
    let i = 0
    while (i < chunks.length && acc + chunks[i].length <= written) {
      acc += chunks[i].length
      i++
    }
    return acc === written ? i : null
  } catch {
    return null
  }
}

function onPick(e: Event) {
  const input = e.target as HTMLInputElement
  if (input.files?.length) void uploadFiles(input.files)
  input.value = ''
}

function onDrop(e: DragEvent) {
  if (e.dataTransfer?.files.length) void uploadFiles(e.dataTransfer.files)
}

function onPaste(e: ClipboardEvent) {
  const items = e.clipboardData?.files
  if (items?.length && currentId.value) void uploadFiles(items)
}

// --- выделение и массовые действия ---

function toggleSelect(id: number) {
  const next = new Set(selected.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selected.value = next
  confirmDelete.value = false
}

/** Клик по карточке: в режиме выбора — отметить, иначе открыть просмотр. */
function onCardClick(file: VaultFile, index: number) {
  if (selectMode.value) toggleSelect(file.id)
  else viewerIndex.value = index
}

// долгое нажатие включает режим выбора: на телефоне это привычнее галочек
let pressTimer: ReturnType<typeof setTimeout> | undefined

function pressStart(file: VaultFile) {
  pressTimer = setTimeout(() => {
    selectMode.value = true
    if (!selected.value.has(file.id)) toggleSelect(file.id)
  }, 500)
}

function pressEnd() {
  clearTimeout(pressTimer)
}

function toggleSelectMode() {
  selectMode.value = !selectMode.value
  if (!selectMode.value) selected.value = new Set()
}

function selectAll() {
  selected.value = new Set(visibleFiles.value.map((f) => f.id))
  selectMode.value = true
}

async function deleteSelected() {
  const ids = [...selected.value]
  if (!ids.length) return
  try {
    await vaultApi.deleteFiles(ids)
    const freed = files.value
      .filter((f) => ids.includes(f.id))
      .reduce((sum, f) => sum + f.plain_size, 0)
    files.value = files.value.filter((f) => !ids.includes(f.id))
    quota.value = { ...quota.value, used: Math.max(0, quota.value.used - freed) }
    selected.value = new Set()
    confirmDelete.value = false
  } catch {
    showToast('Не удалось удалить')
  }
}

/** Перенос: ключ содержимого переворачивается под ключ целевой папки. */
async function moveSelected(targetId: number) {
  const from = currentId.value ? keyFor(currentId.value) : null
  const to = keyFor(targetId)
  if (!from || !to) return showToast('Откройте обе папки паролем')
  for (const id of selected.value) {
    const file = files.value.find((f) => f.id === id)
    if (!file) continue
    const keyEnv = await rewrapContentKey(from, to, file.key_env)
    const meta = metas.value.get(id)
    if (!keyEnv || !meta) continue
    try {
      const { file: updated } = await vaultApi.updateFile(id, {
        key_env: keyEnv,
        folder_id: targetId,
        meta_env: await sealMeta(to, meta),
      })
      const i = files.value.findIndex((f) => f.id === id)
      if (i >= 0) files.value[i] = updated
    } catch {
      showToast('Не удалось перенести файл')
    }
  }
  selected.value = new Set()
  files.value = [...files.value]
}

function onFileSaved(file: VaultFile, meta: FileMeta) {
  const i = files.value.findIndex((f) => f.id === file.id)
  if (i >= 0) files.value[i] = file
  metas.value.set(file.id, meta)
  metas.value = new Map(metas.value)
  files.value = [...files.value]
}

function onFileCopied(file: VaultFile, meta: FileMeta) {
  files.value = [file, ...files.value]
  metas.value.set(file.id, meta)
  metas.value = new Map(metas.value)
  quota.value = { ...quota.value, used: quota.value.used + file.plain_size }
}

function onFileDeleted(id: number) {
  const gone = files.value.find((f) => f.id === id)
  files.value = files.value.filter((f) => f.id !== id)
  if (gone) quota.value = { ...quota.value, used: Math.max(0, quota.value.used - gone.plain_size) }
  actionsFile.value = null
}

/**
 * Выгрузка папки одним архивом: файлы расшифровываются в браузере и
 * складываются в zip. Внутри Telegram скачивание ненадёжно — предупреждаем.
 */
async function exportFolder() {
  const key = currentId.value ? keyFor(currentId.value) : null
  if (!key || exporting.value) return
  const list = selected.value.size
    ? visibleFiles.value.filter((f) => selected.value.has(f.id))
    : visibleFiles.value
  if (!list.length) return
  exporting.value = true
  try {
    const items: { name: string; blob: Blob }[] = []
    for (const file of list) {
      const meta = metas.value.get(file.id)
      if (!meta) continue
      const data = await vaultApi.fetchBlob(file.id)
      const blob = await decryptFile(data, key, file.key_env, meta)
      if (blob) items.push({ name: meta.name, blob })
    }
    if (!items.length) return showToast('Нечего выгружать')
    const zip = await zipFiles(items)
    const url = URL.createObjectURL(zip)
    const a = document.createElement('a')
    a.href = url
    a.download = `${current.value?.name ?? 'vault'}.zip`
    a.click()
    setTimeout(() => URL.revokeObjectURL(url), 60_000)
    showToast('Архив собран — внутри Telegram он может не сохраниться')
  } catch {
    showToast('Не удалось собрать архив')
  } finally {
    exporting.value = false
  }
}

const moveTargets = computed(() =>
  (lockVersion.value, folders.value.filter((f) => f.id !== currentId.value && f.mine && isUnlocked(f.id))),
)

function onFolderSaved(f: VaultFolder) {
  const i = folders.value.findIndex((x) => x.id === f.id)
  if (i >= 0) folders.value[i] = f
  folderModal.value = null
}

function onFolderCreated(f: VaultFolder) {
  folders.value.push(f)
  folderModal.value = null
  void openFolder(f)
}

function onFolderDeleted(id: number) {
  const gone = files.value.filter((f) => f.folder_id === id)
  const freed = gone.reduce((sum, f) => sum + f.plain_size, 0)
  folders.value = folders.value.filter((f) => f.id !== id)
  files.value = files.value.filter((f) => f.folder_id !== id)
  quota.value = { ...quota.value, used: Math.max(0, quota.value.used - freed) }
  folderModal.value = null
  currentId.value = null
}
</script>

<template>
  <div class="vault" @paste="onPaste" @dragover.prevent @drop.prevent="onDrop">
    <div v-if="loading" class="state">Загрузка…</div>

    <template v-else>
      <QuotaBar :quota="quota" />

      <div class="bar">
        <div class="crumbs">
          <button class="crumb" @click="currentId = null">🔐 Сейф</button>
          <template v-for="f in path" :key="f.id">
            <span class="sep">›</span>
            <button class="crumb" @click="currentId = f.id">{{ f.name }}</button>
          </template>
        </div>
        <button class="icon-btn" title="Запереть всё" @click="lockEverything">🔒</button>
      </div>

      <!-- список папок текущего уровня -->
      <div v-if="children.length" class="folders">
        <button v-for="f in children" :key="f.id" class="folder" @click="openFolder(f)">
          <span class="f-icon">{{ isUnlocked(f.id) ? '📂' : '🔒' }}</span>
          <span class="f-name">{{ f.name }}</span>
          <span v-if="f.shared" class="f-badge" title="Общая папка">👥</span>
        </button>
      </div>

      <button class="btn ghost" @click="folderModal = { folder: null }">
        ➕ {{ currentId ? 'Подпапка' : 'Папка' }}
      </button>

      <template v-if="current">
        <div class="folder-head">
          <span class="hint" v-if="current.hint && !unlocked">Подсказка: {{ current.hint }}</span>
          <span v-if="current.owner_name" class="owner">от {{ current.owner_name }}</span>
          <span class="grow"></span>
          <button v-if="unlocked" class="icon-btn" title="Запереть папку" @click="lockCurrent">🔒</button>
          <button class="icon-btn" title="Настройки папки" @click="folderModal = { folder: current }">
            ⚙️
          </button>
        </div>

        <!-- заперто: ключа в памяти нет, показывать нечего -->
        <form v-if="!unlocked" class="unlock" @submit.prevent="submitPassword">
          <input v-model="password" type="password" placeholder="Пароль папки" autocomplete="off" />
          <button class="btn primary" :disabled="unlocking || !password">
            {{ unlocking ? 'Открываем…' : '🔓 Открыть' }}
          </button>
          <p class="note">
            Ключ живёт в памяти минуту и стирается при уходе со вкладки — потом пароль спросим снова.
          </p>
        </form>

        <template v-else>
          <div class="upload-row">
            <label class="btn ghost">
              ⬆️ Загрузить
              <input type="file" multiple hidden @change="onPick" />
            </label>
            <label class="btn ghost">
              📷 Снимок
              <input type="file" accept="image/*" capture="environment" hidden @change="onPick" />
            </label>
          </div>
          <p class="note">Файлы можно перетащить сюда или вставить из буфера.</p>

          <div v-if="uploading" class="progress">
            <div class="fill" :style="{ width: uploading.pct + '%' }"></div>
            <span class="p-name">{{ uploading.name }} — {{ uploading.pct }}%</span>
          </div>

          <div class="tools">
            <input v-model="query" class="search" type="search" placeholder="Поиск по именам" />
            <button class="icon-btn" :title="selectMode ? 'Выйти из выбора' : 'Выбрать файлы'"
                    @click="toggleSelectMode">
              {{ selectMode ? '✖' : '☑️' }}
            </button>
            <button class="icon-btn" title="Выгрузить папку архивом" :disabled="exporting"
                    @click="exportFolder">
              {{ exporting ? '⏳' : '📦' }}
            </button>
          </div>

          <div v-if="selectMode" class="actions">
            <span>{{ selected.size }} выбрано</span>
            <button v-if="selected.size < visibleFiles.length" @click="selectAll">Все</button>
            <template v-if="selected.size">
              <select v-if="moveTargets.length"
                      @change="moveSelected(Number(($event.target as HTMLSelectElement).value))">
                <option value="">Перенести в…</option>
                <option v-for="f in moveTargets" :key="f.id" :value="f.id">{{ f.name }}</option>
              </select>
              <button v-if="!confirmDelete" class="danger" @click="confirmDelete = true">🗑 Удалить</button>
              <button v-else class="danger" @click="deleteSelected">Точно? Навсегда</button>
            </template>
          </div>

          <p v-if="!currentFiles.length" class="state">Пока пусто — загрузите первый файл 👆</p>
          <p v-else-if="!visibleFiles.length" class="state">Ничего не нашлось.</p>

          <div class="grid">
            <div
              v-for="(file, i) in visibleFiles"
              :key="file.id"
              class="card"
              :class="{ on: selected.has(file.id) }"
            >
              <button
                class="thumb"
                @click="onCardClick(file, i)"
                @pointerdown="pressStart(file)"
                @pointerup="pressEnd"
                @pointerleave="pressEnd"
              >
                <img v-if="thumbs.get(file.id)" :src="thumbs.get(file.id)" alt="" />
                <span v-else class="ic">{{ icon(file) }}</span>
                <span v-if="selectMode" class="pick" :class="{ on: selected.has(file.id) }">
                  {{ selected.has(file.id) ? '✓' : '' }}
                </span>
                <span v-if="file.expires_at" class="tag" title="Файл удалится по сроку">⏳</span>
              </button>
              <div class="meta">
                <span class="fname">{{ metaOf(file).name }}</span>
                <span class="fsize">{{ fmtSize(file.plain_size) }}</span>
              </div>
              <div class="card-actions">
                <button class="icon-btn" title="Действия" @click="actionsFile = file">⋯</button>
                <button v-if="file.shared" class="icon-btn" title="Есть доступ у других"
                        @click="actionsFile = file">👥</button>
              </div>
            </div>
          </div>
        </template>
      </template>

      <p v-else-if="!children.length" class="state">
        Сейф пуст. Создайте папку и задайте ей пароль — файлы шифруются на вашем устройстве,
        сервер видит только зашифрованные байты.
      </p>
    </template>

    <FolderModal
      v-if="folderModal"
      :folder="folderModal.folder"
      :parent-id="folderModal.folder ? folderModal.folder.parent_id : currentId"
      @created="onFolderCreated"
      @saved="onFolderSaved"
      @deleted="onFolderDeleted"
      @close="folderModal = null"
    />

    <FileViewer
      v-if="viewerFile && currentId && keyFor(currentId)"
      :files="visibleFiles"
      :index="viewerIndex!"
      :metas="metas"
      :folder-key="keyFor(currentId)!"
      @update:index="viewerIndex = $event"
      @close="viewerIndex = null"
    />

    <FileActions
      v-if="actionsFile && currentId && keyFor(currentId)"
      :file="actionsFile"
      :meta="metaOf(actionsFile)"
      :folder-key="keyFor(currentId)!"
      :folders="folders"
      :lock-version="lockVersion"
      @saved="onFileSaved"
      @copied="onFileCopied"
      @deleted="onFileDeleted"
      @close="actionsFile = null"
    />
  </div>
</template>

<style scoped>
.state {
  text-align: center;
  color: var(--text-secondary);
  padding: 24px 0;
  font-size: 13px;
}

.bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}

.crumbs {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 4px;
  overflow-x: auto;
  white-space: nowrap;
}

.crumb {
  background: none;
  border: none;
  color: var(--accent-color);
  padding: 2px 0;
  font-size: 13px;
}

.sep {
  color: var(--text-secondary);
  font-size: 12px;
}

.icon-btn {
  background: none;
  border: none;
  padding: 4px 6px;
  font-size: 16px;
  color: var(--text-secondary);
}

.folders {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-bottom: 8px;
}

.folder {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 10px 12px;
  border: none;
  border-radius: 10px;
  background: var(--card-color);
  color: var(--text-color);
  font-size: 15px;
  text-align: left;
}

.f-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.f-badge {
  font-size: 13px;
}

.folder-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 12px 0 6px;
  font-size: 12px;
  color: var(--text-secondary);
}

.grow {
  flex: 1;
}

.unlock {
  background: var(--card-color);
  border-radius: 10px;
  padding: 12px;
}

.unlock input {
  width: 100%;
}

.note {
  margin: 6px 0 0;
  font-size: 11px;
  color: var(--text-secondary);
}

.upload-row {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}

.btn {
  display: block;
  width: 100%;
  margin-top: 8px;
  padding: 10px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
  text-align: center;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn.ghost {
  background: var(--card-color);
}

.btn:disabled {
  opacity: 0.5;
}

.progress {
  position: relative;
  height: 18px;
  margin-top: 8px;
  border-radius: 9px;
  background: var(--bg-secondary);
  overflow: hidden;
}

.progress .fill {
  height: 100%;
  background: var(--accent-color);
  opacity: 0.4;
  transition: width 0.2s;
}

.p-name {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
}

.actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 10px;
  font-size: 13px;
}

.actions button {
  background: var(--bg-secondary);
  color: var(--text-color);
  border: none;
  border-radius: 8px;
  padding: 6px 10px;
}

.actions .danger {
  background: #b91c1c;
  color: #fff;
  border: none;
  border-radius: 8px;
  padding: 6px 10px;
}

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
  gap: 10px;
  margin-top: 10px;
}

.card {
  position: relative;
  background: var(--card-color);
  border-radius: 10px;
  padding: 8px;
  border: 2px solid transparent;
}

.card.on {
  border-color: var(--accent-color);
}

.tools {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 10px;
}

.search {
  flex: 1;
  min-width: 0;
  padding: 8px 10px;
  border-radius: 8px;
  border: 1px solid var(--border-color, rgba(128, 128, 128, 0.3));
  background: var(--bg-secondary);
  color: var(--text-color);
  font: inherit;
  font-size: 13px;
}

.thumb {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  aspect-ratio: 1 / 1;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  overflow: hidden;
  padding: 0;
}

.thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.ic {
  font-size: 32px;
}

.meta {
  display: flex;
  flex-direction: column;
  margin-top: 6px;
  min-width: 0;
}

.fname {
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fsize {
  font-size: 11px;
  color: var(--text-secondary);
}

.card-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 4px;
}

/* кружок выбора поверх превью: в режиме выбора его видно сразу, в отличие
   от системной галочки, которую в углу карточки никто не находил */
.pick {
  position: absolute;
  top: 6px;
  left: 6px;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  border: 2px solid #fff;
  background: rgba(0, 0, 0, 0.35);
  color: #fff;
  font-size: 14px;
  line-height: 20px;
  text-align: center;
}

.pick.on {
  background: var(--accent-color);
  border-color: var(--accent-color);
}

.tag {
  position: absolute;
  top: 6px;
  right: 6px;
  font-size: 13px;
  filter: drop-shadow(0 1px 2px rgba(0, 0, 0, 0.5));
}
</style>
