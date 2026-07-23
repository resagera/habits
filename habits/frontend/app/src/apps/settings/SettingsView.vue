<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { api } from '../../shared/api/client'
import {
  applyCardStyle,
  deleteBackgroundImage,
  loadBackground,
  resolveBgUrl,
  setBackground,
  uploadBackground,
  type BackgroundState,
  type BgPosition,
} from '../../shared/background'
import {
  getThemePreference,
  setThemePreference,
  type ThemePreference,
} from '../../shared/telegram'
import { showToast } from '../../shared/toast'
import { APP_VERSION } from '../../shared/version'
import AdminPages from './components/AdminPages.vue'
import AdminUsers from './components/AdminUsers.vue'
import AdminLimits from './components/AdminLimits.vue'
import { fetchEntries } from '../diary/api'
import type { DiaryEntry } from '../diary/types'
import {
  getLinksMode,
  loadLinksMode,
  getLinksPrefs,
  linksBackend,
  setLinksMode,
  setLinksPrefs,
  transferLinksData,
  type LinksMode,
} from '../links/storage'
import type { LinkFolder } from '../links/types'
import {
  decryptImport,
  exportEncryptedFile,
  exportPlainFile,
  parseImportFile,
  type ParsedImport,
} from '../passwords/crypto'
import {
  deleteVaultEverywhere,
  entries as vaultEntries,
  folders as vaultFolders,
  initVault,
  lock,
  persistSession,
  sessionData,
  unlockWithPassword,
  unlocked as vaultUnlocked,
} from '../passwords/session'

const theme = ref<ThemePreference>(getThemePreference())

const exportFrom = ref('')
const exportTo = ref('')
const exporting = ref(false)

const hasVault = ref(true) // уточняется асинхронно в onMounted
const confirmVaultReset = ref(false)

// --- пароли: экспорт/импорт ---
const passMasterInput = ref('')
const passBusy = ref(false)
const exportEncrypt = ref(true)
const exportFilePassword = ref('')
const passImportInput = ref<HTMLInputElement>()
const pendingImport = ref<ParsedImport | null>(null)
const importFilePassword = ref('')
const confirmApplyImport = ref(false)

const me = ref<{ id: number; username?: string; first_name?: string; is_admin?: boolean } | null>(null)

const linksMode = ref<LinksMode>(getLinksMode())
const transferring = ref(false)
const confirmTransfer = ref(false)

// --- links: отображение и импорт/экспорт ---
const linksPrefs = ref(getLinksPrefs())
const linksFolders = ref<LinkFolder[]>([])
const importFileInput = ref<HTMLInputElement>()
const confirmImportJSON = ref<string | null>(null)
const importText = ref('')
const importFolderId = ref<number | null>(null)
const importCommonTags = ref('')
const importSmartTags = ref(false)
const importing = ref(false)

// --- фон ---
const bg = ref<BackgroundState | null>(null)
const bgPosition = ref<BgPosition>('cover')
const bgBlur = ref(0)
const bgDim = ref(0)
const bgUrlInput = ref('')
const bgBusy = ref(false)
const bgFileInput = ref<HTMLInputElement>()
const confirmDeleteImageId = ref<number | null>(null)
const cardOpacity = ref(100)
const cardBlur = ref(0)

onMounted(async () => {
  try {
    me.value = await api.get('/me')
  } catch {
    /* вне Telegram — просто не показываем блок */
  }
  initVault().then((s) => (hasVault.value = s === 'exists'))
  bg.value = await loadBackground()
  if (bg.value) {
    bgPosition.value = bg.value.position
    bgBlur.value = bg.value.blur
    bgDim.value = bg.value.dim
    bgTextDark.value = bg.value.text_dark
    bgTextLight.value = bg.value.text_light
    cardOpacity.value = bg.value.card_opacity
    cardBlur.value = bg.value.card_blur
    if (bg.value.kind === 'url') bgUrlInput.value = bg.value.url
  }
  try {
    // источник истины по выбору хранилища — сервер (кэш в webview очищается)
    linksMode.value = await loadLinksMode()
    linksFolders.value = (await linksBackend(linksMode.value).loadTree()).folders
  } catch {
    /* хранилище недоступно — селектор папок останется пустым */
  }
})

// --- links: отображение / импорт / экспорт ---
function onLinksPrefs() {
  setLinksPrefs(linksPrefs.value)
}

async function exportLinksJSON() {
  try {
    const data = await linksBackend().loadTree()
    const url = URL.createObjectURL(
      new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }),
    )
    const a = document.createElement('a')
    a.href = url
    a.download = 'links_export.json'
    a.click()
    URL.revokeObjectURL(url)
  } catch {
    showToast('Не удалось выгрузить')
  }
}

function onImportJSONFile(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  const reader = new FileReader()
  reader.onload = () => {
    confirmImportJSON.value = String(reader.result)
    showToast('Файл прочитан — подтвердите замену данных')
  }
  reader.readAsText(file)
  ;(e.target as HTMLInputElement).value = ''
}

async function applyImportJSON() {
  const raw = confirmImportJSON.value
  if (!raw) return
  try {
    const data = JSON.parse(raw)
    if (!Array.isArray(data.folders) || !Array.isArray(data.links)) throw new Error('bad format')
    await linksBackend().replaceAll(data)
    confirmImportJSON.value = null
    showToast(`Импортировано: ${data.links.length} ссылок ✅`)
  } catch {
    showToast('Некорректный файл экспорта')
  }
}

/** «Умный» тег из URL: домен без www и зоны (github.com → github). */
function smartTag(url: string): string | null {
  try {
    const host = new URL(url).hostname.replace(/^www\./, '')
    return host.split('.')[0] || null
  } catch {
    return null
  }
}

async function importLinksByText() {
  const lines = importText.value.split('\n').map((s) => s.trim()).filter(Boolean)
  if (lines.length === 0) {
    showToast('Вставьте ссылки — по одной на строку')
    return
  }
  const commonTags = importCommonTags.value
    .split(',')
    .map((t) => t.trim().replace(/^#/, ''))
    .filter(Boolean)
  importing.value = true
  let ok = 0
  try {
    for (const line of lines) {
      // формат строки: "url" или "url название через пробел"
      const [rawUrl, ...nameParts] = line.split(/\s+/)
      let url = rawUrl
      if (!/^[a-z][a-z0-9+.-]*:/i.test(url)) url = 'https://' + url
      const tags = [...commonTags]
      if (importSmartTags.value) {
        const t = smartTag(url)
        if (t && !tags.includes(t)) tags.push(t)
      }
      const name = nameParts.join(' ') || url.replace(/^https?:\/\//, '').slice(0, 80)
      try {
        await linksBackend().createLink({
          name,
          url,
          tags,
          pinned: false,
          folder_id: importFolderId.value,
        })
        ok++
      } catch {
        /* пропускаем битую строку */
      }
    }
    importText.value = ''
    showToast(`Добавлено ссылок: ${ok} из ${lines.length} ✅`)
  } finally {
    importing.value = false
  }
}

// --- фон ---
async function bgAction(fn: () => Promise<void>, errText: string) {
  bgBusy.value = true
  try {
    await fn()
  } catch {
    showToast(errText)
  } finally {
    bgBusy.value = false
  }
}

const bgTextDark = ref('')
const bgTextLight = ref('')

/** Общие для всех обновлений параметры эффектов. */
function bgEffects() {
  return {
    position: bgPosition.value,
    blur: bgBlur.value,
    dim: bgDim.value,
    text_dark: bgTextDark.value,
    text_light: bgTextLight.value,
    card_opacity: cardOpacity.value,
    card_blur: cardBlur.value,
  }
}

/** Живой предпросмотр «стекла» карточек при перетаскивании ползунков. */
function previewCardStyle() {
  applyCardStyle(cardOpacity.value, cardBlur.value)
}

/** Сохранение параметров карточек — работает и без выбранного фона. */
function pushCardStyle() {
  const current = bg.value
  bgAction(async () => {
    if (!current || current.kind === 'none') {
      bg.value = await setBackground({ kind: 'none', ...bgEffects() })
    } else if (current.kind === 'file') {
      bg.value = await setBackground({ kind: 'file', image_id: currentImageId()!, ...bgEffects() })
    } else {
      bg.value = await setBackground({ kind: 'url', url: current.url, ...bgEffects() })
    }
  }, 'Не удалось применить карточки')
}

/** Смена цвета текста — работает и без выбранного фона. */
function pushTextColors() {
  const current = bg.value
  bgAction(async () => {
    if (!current || current.kind === 'none') {
      bg.value = await setBackground({ kind: 'none', ...bgEffects() })
    } else if (current.kind === 'file') {
      bg.value = await setBackground({ kind: 'file', image_id: currentImageId()!, ...bgEffects() })
    } else {
      bg.value = await setBackground({ kind: 'url', url: current.url, ...bgEffects() })
    }
  }, 'Не удалось применить цвет текста')
}

function resetTextColor(which: 'dark' | 'light') {
  if (which === 'dark') bgTextDark.value = ''
  else bgTextLight.value = ''
  pushTextColors()
}

function onBgUpload(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  bgAction(async () => {
    const image = await uploadBackground(file)
    bg.value = await setBackground({ kind: 'file', image_id: image.id, ...bgEffects() })
    showToast('Фон загружен и применён ✅')
  }, 'Не удалось загрузить (jpeg/png/webp/gif, до 5 МБ)')
  ;(e.target as HTMLInputElement).value = ''
}

function selectBgImage(id: number) {
  bgAction(async () => {
    bg.value = await setBackground({ kind: 'file', image_id: id, ...bgEffects() })
    showToast('Фон применён ✅')
  }, 'Не удалось применить фон')
}

function applyBgUrl() {
  const url = bgUrlInput.value.trim()
  if (!url) {
    showToast('Введите URL изображения')
    return
  }
  bgAction(async () => {
    bg.value = await setBackground({ kind: 'url', url, ...bgEffects() })
    showToast('Фон применён ✅')
  }, 'Не удалось применить фон')
}

/** Смена позиции/блюра/затемнения — переприменяем текущий фон. */
function pushBgEffects() {
  const current = bg.value
  if (!current || current.kind === 'none') return
  bgAction(async () => {
    bg.value = await setBackground(
      current.kind === 'file'
        ? { kind: 'file', image_id: currentImageId()!, ...bgEffects() }
        : { kind: 'url', url: current.url, ...bgEffects() },
    )
  }, 'Не удалось применить эффекты')
}

function currentImageId(): number | null {
  const current = bg.value
  if (!current || current.kind !== 'file') return null
  return current.images.find((i) => i.url === current.url)?.id ?? null
}

function resetBg() {
  bgAction(async () => {
    bg.value = await setBackground({ kind: 'none', ...bgEffects() })
    showToast('Фон убран')
  }, 'Не удалось убрать фон')
}

// --- фон из чата бота: шлёшь картинку боту — она появляется в галерее ---
const botModal = ref(false)
let botPollTimer: ReturnType<typeof setInterval> | undefined

function openBotModal() {
  botModal.value = true
  const startCount = bg.value?.images.length ?? 0
  clearInterval(botPollTimer)
  botPollTimer = setInterval(async () => {
    if (!botModal.value) {
      clearInterval(botPollTimer)
      return
    }
    try {
      const fresh = await loadBackground()
      if (fresh && fresh.images.length > startCount) {
        bg.value = fresh
        clearInterval(botPollTimer)
        botModal.value = false
        showToast('Картинка получена — выберите её в галерее 🖼')
      }
    } catch {
      /* следующая попытка через 4 с */
    }
  }, 4000)
  // страховка: не поллим дольше 3 минут
  setTimeout(() => clearInterval(botPollTimer), 180_000)
}

onUnmounted(() => clearInterval(botPollTimer))

function removeBgImage(id: number) {
  if (confirmDeleteImageId.value !== id) {
    confirmDeleteImageId.value = id
    setTimeout(() => {
      if (confirmDeleteImageId.value === id) confirmDeleteImageId.value = null
    }, 3000)
    return
  }
  confirmDeleteImageId.value = null
  bgAction(async () => {
    await deleteBackgroundImage(id)
    bg.value = await loadBackground()
    showToast('Изображение удалено')
  }, 'Не удалось удалить изображение')
}

function onTheme(pref: ThemePreference) {
  theme.value = pref
  setThemePreference(pref)
}

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

function onLinksMode(mode: LinksMode) {
  linksMode.value = mode
  setLinksMode(mode)
  confirmTransfer.value = false
  showToast(mode === 'local' ? 'Ссылки: локальное хранилище' : 'Ссылки: серверное хранилище')
}

async function transferLinks() {
  if (!confirmTransfer.value) {
    confirmTransfer.value = true
    setTimeout(() => (confirmTransfer.value = false), 4000)
    return
  }
  confirmTransfer.value = false
  transferring.value = true
  try {
    const n = await transferLinksData(linksMode.value)
    showToast(`Перенесено ссылок: ${n} ✅`)
  } catch {
    showToast('Не удалось перенести данные')
  } finally {
    transferring.value = false
  }
}

// --- пароли: разблокировка/экспорт/импорт ---
async function unlockForSettings() {
  if (!passMasterInput.value) return
  passBusy.value = true
  try {
    const res = await unlockWithPassword(passMasterInput.value)
    if (res === 'wrong') {
      showToast('Неверный мастер-пароль')
      return
    }
    if (res === 'empty') {
      hasVault.value = false
      return
    }
    passMasterInput.value = ''
  } finally {
    passBusy.value = false
  }
}

async function exportPasswords() {
  passBusy.value = true
  try {
    let content: string
    if (exportEncrypt.value) {
      if (exportFilePassword.value.length < 6) {
        showToast('Пароль файла — минимум 6 символов')
        return
      }
      content = await exportEncryptedFile(sessionData(), exportFilePassword.value)
    } else {
      content = exportPlainFile(sessionData())
    }
    const url = URL.createObjectURL(new Blob([content], { type: 'application/json' }))
    const a = document.createElement('a')
    a.href = url
    a.download = exportEncrypt.value ? 'passwords_backup_encrypted.json' : 'passwords_backup_PLAIN.json'
    a.click()
    URL.revokeObjectURL(url)
    exportFilePassword.value = ''
    showToast(exportEncrypt.value ? 'Экспортировано (зашифровано) 🔐' : 'Экспортировано БЕЗ шифрования ⚠️')
  } catch {
    showToast('Не удалось экспортировать')
  } finally {
    passBusy.value = false
  }
}

function onPassImportFile(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  const reader = new FileReader()
  reader.onload = () => {
    const parsed = parseImportFile(String(reader.result).trim())
    if (!parsed) {
      showToast('Не похоже на файл экспорта паролей')
      return
    }
    pendingImport.value = parsed
    importFilePassword.value = ''
    confirmApplyImport.value = false
  }
  reader.readAsText(file)
  ;(e.target as HTMLInputElement).value = ''
}

async function applyImport() {
  const parsed = pendingImport.value
  if (!parsed) return
  if (!confirmApplyImport.value) {
    confirmApplyImport.value = true
    setTimeout(() => (confirmApplyImport.value = false), 4000)
    return
  }
  confirmApplyImport.value = false
  passBusy.value = true
  try {
    let data
    if (parsed.kind === 'encrypted') {
      if (!importFilePassword.value) {
        showToast('Введите пароль файла')
        return
      }
      data = await decryptImport(parsed.container, importFilePassword.value)
      if (data === null) {
        showToast('Неверный пароль файла')
        return
      }
    } else {
      data = parsed.data
    }
    vaultFolders.value = data.folders
    vaultEntries.value = data.entries
    await persistSession()
    pendingImport.value = null
    importFilePassword.value = ''
    showToast(`Импортировано: ${data.entries.length} паролей ✅`)
  } catch {
    showToast('Не удалось импортировать')
  } finally {
    passBusy.value = false
  }
}

function resetVault() {
  if (!confirmVaultReset.value) {
    confirmVaultReset.value = true
    setTimeout(() => (confirmVaultReset.value = false), 4000)
    return
  }
  // сброс и на сервере, и в локальном кэше
  api.delete('/passwords/vault').catch(() => {})
  deleteVaultEverywhere()
  lock()
  hasVault.value = false
  confirmVaultReset.value = false
  showToast('Хранилище паролей удалено')
}
</script>

<template>
  <section class="section">
    <h3>Фон приложения</h3>
    <div v-if="bg" class="bg-controls">
      <div class="row">
        <button class="btn" :disabled="bgBusy" @click="bgFileInput?.click()">📤 Загрузить своё</button>
        <button class="btn" :disabled="bgBusy" @click="openBotModal">🤖 Из чата бота</button>
        <button class="btn" :disabled="bgBusy || bg.kind === 'none'" @click="resetBg">Убрать фон</button>
      </div>
      <input
        ref="bgFileInput"
        type="file"
        accept="image/*"
        class="hidden-input"
        @change="onBgUpload"
      />

      <div class="row bg-url-row">
        <input v-model="bgUrlInput" type="text" placeholder="…или URL изображения" />
        <button class="btn slim" :disabled="bgBusy" @click="applyBgUrl">Применить</button>
      </div>

      <label class="bg-pos">
        <span>Размещение:</span>
        <select v-model="bgPosition" @change="pushBgEffects">
          <option value="cover">Заполнить</option>
          <option value="repeat">Замостить</option>
          <option value="center">По центру</option>
        </select>
      </label>

      <label class="bg-slider">
        <span>Размытие: {{ bgBlur }}px</span>
        <input
          v-model.number="bgBlur"
          type="range"
          min="0"
          max="30"
          :disabled="bg.kind === 'none'"
          @change="pushBgEffects"
        />
      </label>

      <label class="bg-slider">
        <span>
          {{ bgDim < 0 ? `Темнее: ${-bgDim}%` : bgDim > 0 ? `Светлее: ${bgDim}%` : 'Яркость: без изменений' }}
        </span>
        <input
          v-model.number="bgDim"
          type="range"
          min="-70"
          max="70"
          step="5"
          :disabled="bg.kind === 'none'"
          @change="pushBgEffects"
        />
      </label>

      <label class="bg-pos">
        <span>Цвет текста (тёмная тема):</span>
        <span class="color-wrap">
          <input v-model="bgTextDark" type="color" class="color-mini" @change="pushTextColors" />
          <button v-if="bgTextDark" class="mini-x" title="Сбросить" @click="resetTextColor('dark')">✕</button>
        </span>
      </label>

      <label class="bg-pos">
        <span>Цвет текста (светлая тема):</span>
        <span class="color-wrap">
          <input v-model="bgTextLight" type="color" class="color-mini" @change="pushTextColors" />
          <button v-if="bgTextLight" class="mini-x" title="Сбросить" @click="resetTextColor('light')">✕</button>
        </span>
      </label>

      <div v-if="bg.images.length" class="bg-gallery">
        <div v-for="img in bg.images" :key="img.id" class="bg-thumb-wrap">
          <img
            :src="resolveBgUrl(img.url)"
            class="bg-thumb"
            :class="{ current: bg.kind === 'file' && bg.url === img.url }"
            alt=""
            @click="selectBgImage(img.id)"
          />
          <button class="bg-thumb-del" @click="removeBgImage(img.id)">
            {{ confirmDeleteImageId === img.id ? '?' : '✕' }}
          </button>
        </div>
      </div>
      <p class="hint-text" style="margin-top: 8px">
        Фон синхронизируется через сервер и работает на iOS (отдельный слой вместо
        фиксированного background).
      </p>
    </div>
    <p v-else class="hint-text">Недоступно вне Telegram</p>
  </section>

  <section class="section">
    <h3>Карточки — стекло</h3>
    <p class="hint-text">
      Полупрозрачные карточки с размытием фона под ними (эффект «матового
      стекла»). Хорошо смотрится поверх своего фона.
    </p>
    <label class="bg-slider">
      <span>
        {{ cardOpacity >= 100 ? 'Непрозрачность: сплошные' : `Непрозрачность: ${cardOpacity}%` }}
      </span>
      <input
        v-model.number="cardOpacity"
        type="range"
        min="20"
        max="100"
        step="5"
        @input="previewCardStyle"
        @change="pushCardStyle"
      />
    </label>
    <label class="bg-slider">
      <span>Размытие под карточкой: {{ cardBlur }}px</span>
      <input
        v-model.number="cardBlur"
        type="range"
        min="0"
        max="30"
        @input="previewCardStyle"
        @change="pushCardStyle"
      />
    </label>
  </section>

  <section class="section">
    <h3>Тема интерфейса</h3>
    <label class="radio">
      <input type="radio" :checked="theme === 'auto'" @change="onTheme('auto')" />
      <span>Как в Telegram (авто)</span>
    </label>
    <label class="radio">
      <input type="radio" :checked="theme === 'light'" @change="onTheme('light')" />
      <span>Светлая 🌞</span>
    </label>
    <label class="radio">
      <input type="radio" :checked="theme === 'dark'" @change="onTheme('dark')" />
      <span>Тёмная 🌙</span>
    </label>
  </section>

  <section class="section">
    <h3>Дневник — экспорт</h3>
    <div class="date-range">
      <input v-model="exportFrom" type="date" />
      <input v-model="exportTo" type="date" />
    </div>
    <div class="row">
      <button class="btn" :disabled="exporting" @click="exportDiary('txt')">Экспорт .txt</button>
      <button class="btn" :disabled="exporting" @click="exportDiary('csv')">Экспорт .csv</button>
    </div>
  </section>

  <section class="section">
    <h3>Ссылки — хранилище</h3>
    <label class="radio">
      <input type="radio" :checked="linksMode === 'local'" @change="onLinksMode('local')" />
      <span>📱 Локально (только это устройство)</span>
    </label>
    <label class="radio">
      <input type="radio" :checked="linksMode === 'server'" @change="onLinksMode('server')" />
      <span>☁️ На сервере (доступно отовсюду)</span>
    </label>
    <p class="hint-text">
      Переключение не переносит данные автоматически. Кнопка ниже скопирует ссылки из
      другого хранилища в выбранное (текущее содержимое будет заменено).
    </p>
    <button class="btn" :disabled="transferring" @click="transferLinks">
      {{
        transferring
          ? 'Перенос…'
          : confirmTransfer
            ? 'Точно заменить данные в выбранном хранилище?'
            : linksMode === 'server'
              ? 'Перенести локальные ссылки на сервер'
              : 'Скачать ссылки с сервера в локальное'
      }}
    </button>
  </section>

  <section class="section">
    <h3>Ссылки — отображение и импорт</h3>
    <label class="radio">
      <input v-model="linksPrefs.showFavorites" type="checkbox" @change="onLinksPrefs" />
      <span>Показывать избранное ⭐</span>
    </label>
    <label class="radio">
      <input v-model="linksPrefs.showTop10" type="checkbox" @change="onLinksPrefs" />
      <span>Показывать топ-10 📈</span>
    </label>

    <div class="row" style="margin-top: 10px">
      <button class="btn" @click="exportLinksJSON">📤 Экспорт JSON</button>
      <button class="btn" @click="importFileInput?.click()">📥 Импорт JSON</button>
    </div>
    <input
      ref="importFileInput"
      type="file"
      accept=".json,application/json"
      class="hidden-input"
      @change="onImportJSONFile"
    />
    <button v-if="confirmImportJSON" class="btn danger" style="margin-top: 8px" @click="applyImportJSON">
      Заменить все ссылки данными из файла?
    </button>

    <p class="hint-text" style="margin-top: 12px">Импорт списком: по одной ссылке на строку
      (можно «url название»).</p>
    <textarea
      v-model="importText"
      rows="3"
      class="import-textarea"
      placeholder="https://example.com Мой сайт&#10;github.com"
    ></textarea>
    <select v-model="importFolderId" class="full-w">
      <option :value="null">🏠 В корень</option>
      <option v-for="f in linksFolders" :key="f.id" :value="f.id">📂 {{ f.name }}</option>
    </select>
    <input v-model="importCommonTags" class="full-w" placeholder="Теги всем (через запятую)" />
    <label class="radio">
      <input v-model="importSmartTags" type="checkbox" />
      <span>Умный подбор тегов (по домену)</span>
    </label>
    <button class="btn" :disabled="importing" style="margin-top: 8px" @click="importLinksByText">
      {{ importing ? 'Импорт…' : 'Импортировать список' }}
    </button>
  </section>

  <section class="section">
    <h3>Пароли — экспорт и импорт</h3>

    <template v-if="!hasVault">
      <p class="hint-text">Хранилище ещё не создано — откройте вкладку Passwords.</p>
    </template>

    <template v-else-if="!vaultUnlocked">
      <p class="hint-text">Для экспорта/импорта разблокируйте хранилище:</p>
      <div class="row">
        <input
          v-model="passMasterInput"
          type="password"
          placeholder="Мастер-пароль"
          autocomplete="off"
          class="grow"
          @keyup.enter="unlockForSettings"
        />
        <button class="btn slim" :disabled="passBusy" @click="unlockForSettings">🔓</button>
      </div>
    </template>

    <template v-else>
      <label class="radio">
        <input v-model="exportEncrypt" type="checkbox" />
        <span>🔐 Зашифровать файл (AES-256-GCM)</span>
      </label>
      <input
        v-if="exportEncrypt"
        v-model="exportFilePassword"
        type="password"
        placeholder="Пароль файла (можно отличный от мастер-пароля)"
        autocomplete="off"
        class="full-w"
      />
      <p v-if="!exportEncrypt" class="hint-text warn">
        ⚠️ Файл будет содержать пароли открытым текстом.
      </p>
      <button class="btn" :disabled="passBusy" @click="exportPasswords">📤 Экспортировать</button>

      <div class="row" style="margin-top: 12px">
        <button class="btn" @click="passImportInput?.click()">📥 Импорт из файла</button>
      </div>
      <input
        ref="passImportInput"
        type="file"
        accept=".json,application/json"
        class="hidden-input"
        @change="onPassImportFile"
      />
      <template v-if="pendingImport">
        <input
          v-if="pendingImport.kind === 'encrypted'"
          v-model="importFilePassword"
          type="password"
          placeholder="Пароль файла"
          autocomplete="off"
          class="full-w"
          style="margin-top: 8px"
        />
        <button class="btn danger" :disabled="passBusy" style="margin-top: 8px" @click="applyImport">
          {{
            confirmApplyImport
              ? 'Точно ЗАМЕНИТЬ все текущие пароли данными из файла?'
              : pendingImport.kind === 'encrypted'
                ? 'Расшифровать и заменить хранилище'
                : 'Заменить хранилище данными из файла'
          }}
        </button>
      </template>
    </template>
  </section>

  <section class="section">
    <h3>Пароли</h3>
    <p class="hint-text">
      Хранилище живёт только на этом устройстве. Сброс удалит все сохранённые пароли безвозвратно.
    </p>
    <button class="btn danger" :disabled="!hasVault" @click="resetVault">
      {{ !hasVault ? 'Хранилище не создано' : confirmVaultReset ? 'Точно удалить все пароли?' : 'Сбросить хранилище паролей' }}
    </button>
  </section>

  <section class="section">
    <h3>О приложении</h3>
    <p class="hint-text">
      Версия {{ APP_VERSION }}<template v-if="me">
        · {{ me.first_name || me.username || 'пользователь' }} (id {{ me.id }})</template
      >
    </p>
  </section>

  <AdminUsers v-if="me?.is_admin" />
  <AdminLimits v-if="me?.is_admin" />
  <AdminPages v-if="me?.is_admin" />

  <!-- фон из чата бота -->
  <div v-if="botModal" class="modal" @click.self="botModal = false">
    <div class="modal-content">
      <h3>🤖 Фон из чата бота</h3>
      <p class="bot-hint">
        Откройте чат с ботом (тот, в котором запущено это приложение) и просто
        отправьте ему картинку — она автоматически появится в галерее фонов.
      </p>
      <p class="bot-hint waiting">⏳ Жду картинку…</p>
      <button class="btn" @click="botModal = false">Закрыть</button>
    </div>
  </div>
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

.radio {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 0;
  cursor: pointer;
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

.color-wrap {
  display: flex;
  align-items: center;
  gap: 4px;
}

.color-mini {
  width: 46px !important;
  height: 32px;
  padding: 2px;
  margin: 0 !important;
}

.mini-x {
  background: var(--bg-secondary);
  border: none;
  border-radius: 6px;
  padding: 4px 8px;
  color: var(--text-color);
}

.bot-hint {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 8px 0;
  text-align: left;
}

.bot-hint.waiting {
  text-align: center;
  color: var(--accent-color);
}

.btn {
  flex: 1;
  padding: 10px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
}

.btn.danger {
  width: 100%;
  background: #b91c1c;
  color: #fff;
}

.btn:disabled {
  opacity: 0.5;
}

.hint-text {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 10px;
}

.bg-controls .row {
  margin-bottom: 8px;
}

.bg-url-row input {
  flex: 1;
  min-width: 0;
}

.btn.slim {
  flex: none;
}

.bg-pos {
  display: flex;
  align-items: center;
  gap: 8px;
}

.bg-slider {
  display: block;
  margin-top: 10px;
  font-size: 13px;
  color: var(--text-secondary);
}

.bg-slider input {
  width: 100%;
  padding: 0;
  border: none;
  background: none;
  accent-color: var(--accent-color);
}

.bg-pos select {
  flex: 1;
}

.bg-gallery {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(76px, 1fr));
  gap: 8px;
  margin-top: 10px;
}

.bg-thumb-wrap {
  position: relative;
}

.bg-thumb {
  width: 100%;
  aspect-ratio: 1 / 1;
  object-fit: cover;
  border-radius: 8px;
  cursor: pointer;
  border: 2px solid transparent;
}

.bg-thumb.current {
  border-color: var(--accent-color);
}

.bg-thumb-del {
  position: absolute;
  top: 2px;
  right: 2px;
  background: rgba(0, 0, 0, 0.6);
  color: #fff;
  border: none;
  border-radius: 50%;
  width: 20px;
  height: 20px;
  font-size: 11px;
  line-height: 1;
}

.hidden-input {
  display: none;
}

.grow {
  flex: 1;
  min-width: 0;
}

.hint-text.warn {
  color: #f59e0b;
}

.import-textarea,
.full-w {
  width: 100%;
  margin-bottom: 8px;
  font: inherit;
  background: var(--bg-secondary);
  color: var(--text-color);
  border: 1px solid var(--hover-bg-color);
  border-radius: 6px;
  padding: 8px;
  resize: vertical;
}

/* карточки-«стекло»: размытие фона под .section (класс неоднозначный —
   правило scoped, чтобы не задеть одноимённые не-карточки) */
:root[data-card-glass] .section {
  backdrop-filter: blur(var(--card-blur, 0px));
  -webkit-backdrop-filter: blur(var(--card-blur, 0px));
}
</style>
