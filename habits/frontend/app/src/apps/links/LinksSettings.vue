<script setup lang="ts">
// Индивидуальные настройки страницы «Ссылки»: выбор хранилища (локально/сервер)
// с переносом данных, отображение блоков и импорт/экспорт.
// Открывается из шестерёнки в шапке (см. PageSettingsModal).
import { onMounted, ref } from 'vue'
import { showToast } from '../../shared/toast'
import {
  getLinksMode,
  loadLinksMode,
  getLinksPrefs,
  linksBackend,
  setLinksMode,
  setLinksPrefs,
  transferLinksData,
  type LinksMode,
} from './storage'
import type { LinkFolder } from './types'

const linksMode = ref<LinksMode>(getLinksMode())
const transferring = ref(false)
const confirmTransfer = ref(false)

const linksPrefs = ref(getLinksPrefs())
const linksFolders = ref<LinkFolder[]>([])
const importFileInput = ref<HTMLInputElement>()
const confirmImportJSON = ref<string | null>(null)
const importText = ref('')
const importFolderId = ref<number | null>(null)
const importCommonTags = ref('')
const importSmartTags = ref(false)
const importing = ref(false)

onMounted(async () => {
  try {
    linksMode.value = await loadLinksMode()
    linksFolders.value = (await linksBackend(linksMode.value).loadTree()).folders
  } catch {
    /* хранилище недоступно — селектор папок останется пустым */
  }
})

function onLinksPrefs() {
  setLinksPrefs(linksPrefs.value)
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
</script>

<template>
  <section class="section">
    <h3>Хранилище</h3>
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
    <h3>Отображение и импорт</h3>
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

.hidden-input {
  display: none;
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
</style>
