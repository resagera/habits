<script setup lang="ts">
/**
 * Просмотр расшифрованного файла. Файл скачивается шифротекстом,
 * расшифровывается в браузере и живёт как blob-URL, который отзывается при
 * закрытии и при листании — иначе расшифрованное осталось бы доступным
 * после «замка».
 *
 * Открывается на весь экран: в маленьком окне картинку и документ смотреть
 * бессмысленно — они всё равно упираются в его края. Содержимое занимает всё
 * место, действия собраны внизу.
 *
 * Листание идёт по файлам папки: стрелками, свайпом и кнопками. Картинку
 * можно приблизить — колесом, двойным щелчком и кнопками; на телефоне
 * работает и щипок (два пальца).
 *
 * PDF в вебвью Telegram не рисуется (ограничение платформы), поэтому там —
 * кнопка «Открыть в браузере»: веб-версия попросит пароль заново и покажет.
 */
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { openExternalLink, tg } from '../../../shared/telegram'
import { showToast } from '../../../shared/toast'
import * as vaultApi from '../api'
import { decryptFile } from '../crypto'
import { docxToHtml, isDocx, isZip, zipList, type ZipEntry } from '../office'
import type { FileMeta, VaultFile } from '../types'

const props = defineProps<{
  files: VaultFile[]
  index: number
  metas: Map<number, FileMeta>
  folderKey: CryptoKey
}>()
const emit = defineEmits<{ close: []; 'update:index': [number] }>()

const url = ref('')
const text = ref('')
const html = ref('')
const entries = ref<ZipEntry[]>([])
const error = ref('')
const loading = ref(true)
const zoom = ref(1)

const file = computed(() => props.files[props.index] ?? null)
const meta = computed<FileMeta>(
  () =>
    (file.value && props.metas.get(file.value.id)) ?? {
      name: '…',
      type: '',
      size: file.value?.plain_size ?? 0,
    },
)

const kind = computed(() => {
  const t = meta.value.type || ''
  const name = meta.value.name.toLowerCase()
  if (t.startsWith('image/')) return 'image'
  if (t.startsWith('audio/')) return 'audio'
  if (t.startsWith('video/')) return 'video'
  if (t === 'application/pdf' || name.endsWith('.pdf')) return 'pdf'
  if (isDocx(meta.value)) return 'docx'
  if (isZip(meta.value)) return 'zip'
  if (
    t.startsWith('text/') ||
    t === 'application/json' ||
    /\.(txt|md|csv|json|log|ya?ml|ini|conf|ts|js|go|py|sh|sql)$/.test(name)
  ) {
    return 'text'
  }
  return 'other'
})

const inTelegram = computed(() => !!tg()?.initData)
const MAX_TEXT = 1 << 20

function revoke() {
  if (url.value) URL.revokeObjectURL(url.value)
  url.value = ''
  text.value = ''
  html.value = ''
  entries.value = []
  zoom.value = 1
}

async function load() {
  const f = file.value
  if (!f) return
  revoke()
  loading.value = true
  error.value = ''
  try {
    const data = await vaultApi.fetchBlob(f.id)
    const blob = await decryptFile(data, props.folderKey, f.key_env, meta.value)
    if (!blob) {
      error.value = 'Не удалось расшифровать: файл повреждён или ключ не тот'
      return
    }
    url.value = URL.createObjectURL(blob)
    if (kind.value === 'text') text.value = await blob.slice(0, MAX_TEXT).text()
    if (kind.value === 'docx') html.value = await docxToHtml(blob)
    if (kind.value === 'zip') entries.value = await zipList(blob)
  } catch (e) {
    error.value = e instanceof Error && e.message ? e.message : 'Не удалось загрузить файл'
  } finally {
    loading.value = false
  }
}

watch(() => file.value?.id, load, { immediate: true })
onUnmounted(revoke)

// --- листание ---

function step(delta: number) {
  const next = props.index + delta
  if (next < 0 || next >= props.files.length) return
  emit('update:index', next)
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'ArrowLeft') step(-1)
  else if (e.key === 'ArrowRight') step(1)
  else if (e.key === 'Escape') emit('close')
}

let touchX = 0
let touchY = 0

function onTouchStart(e: TouchEvent) {
  if (e.touches.length !== 1) return
  touchX = e.touches[0].clientX
  touchY = e.touches[0].clientY
}

function onTouchEnd(e: TouchEvent) {
  // приближённую картинку не листаем: там свайп двигает саму картинку
  if (zoom.value > 1 || !e.changedTouches.length) return
  const dx = e.changedTouches[0].clientX - touchX
  const dy = e.changedTouches[0].clientY - touchY
  if (Math.abs(dx) > 60 && Math.abs(dx) > Math.abs(dy)) step(dx < 0 ? 1 : -1)
}

onMounted(() => window.addEventListener('keydown', onKey))
onUnmounted(() => window.removeEventListener('keydown', onKey))

// --- зум картинки ---

function setZoom(value: number) {
  zoom.value = Math.min(6, Math.max(1, Number(value.toFixed(2))))
}

function onWheel(e: WheelEvent) {
  if (kind.value !== 'image') return
  e.preventDefault()
  setZoom(zoom.value * (e.deltaY < 0 ? 1.15 : 1 / 1.15))
}

function toggleZoom() {
  setZoom(zoom.value > 1 ? 1 : 2.5)
}

function fmtSize(bytes: number): string {
  if (bytes >= 1 << 20) return (bytes / (1 << 20)).toFixed(1) + ' МБ'
  if (bytes >= 1 << 10) return Math.round(bytes / (1 << 10)) + ' КБ'
  return bytes + ' Б'
}

function download() {
  if (!url.value) return
  const a = document.createElement('a')
  a.href = url.value
  a.download = meta.value.name
  a.click()
  if (inTelegram.value) {
    showToast('Внутри Telegram скачивание работает не всегда — откройте в браузере')
  }
}

/** Открыть эту же страницу в обычном браузере: там PDF показывается. */
function openInBrowser() {
  if (!file.value) return
  openExternalLink(`${location.origin}${import.meta.env.BASE_URL}vault?file=${file.value.id}`)
}
</script>

<template>
  <div class="modal">
    <div
      class="modal-content viewer"
      @wheel="onWheel"
      @touchstart.passive="onTouchStart"
      @touchend.passive="onTouchEnd"
    >
      <div class="head">
        <h3 class="name">{{ meta.name }}</h3>
        <span v-if="files.length > 1" class="counter">{{ index + 1 }}/{{ files.length }}</span>
        <button class="close" aria-label="Закрыть" @click="emit('close')">✕</button>
      </div>
      <p v-if="meta.note" class="note">{{ meta.note }}</p>

      <div class="body">
        <p v-if="loading" class="state">Расшифровываем…</p>
        <p v-else-if="error" class="state err">{{ error }}</p>

        <template v-else>
          <div v-if="kind === 'image'" class="stage" :class="{ zoomed: zoom > 1 }">
            <img :src="url" class="media" :style="{ transform: `scale(${zoom})` }" alt=""
                 @dblclick="toggleZoom" />
          </div>
          <audio v-else-if="kind === 'audio'" :src="url" controls class="media"></audio>
          <video v-else-if="kind === 'video'" :src="url" controls class="media"></video>
          <pre v-else-if="kind === 'text'" class="text">{{ text }}</pre>
          <!-- разметка собрана нами из экранированного текста документа -->
          <div v-else-if="kind === 'docx'" class="doc" v-html="html"></div>
          <div v-else-if="kind === 'zip'" class="zip">
            <p class="state">В архиве {{ entries.length }} файл(ов). Показать содержимое нечем —
              распакуйте после скачивания.</p>
            <ul>
              <li v-for="e in entries" :key="e.name">
                <span class="z-name">{{ e.name }}</span>
                <span class="z-size">{{ fmtSize(e.size) }}</span>
              </li>
            </ul>
          </div>
          <template v-else-if="kind === 'pdf'">
            <iframe v-if="!inTelegram" :src="url" class="pdf"></iframe>
            <p v-else class="state">
              PDF внутри Telegram не открывается — это ограничение вебвью. Откройте в браузере:
              там понадобится снова ввести пароль папки.
            </p>
          </template>
          <p v-else class="state">Такой файл показать нечем — его можно скачать.</p>
        </template>
      </div>

      <div class="foot">
        <div v-if="files.length > 1" class="row">
          <button class="btn" :disabled="index === 0" @click="step(-1)">‹ Назад</button>
          <button class="btn" :disabled="index >= files.length - 1" @click="step(1)">Вперёд ›</button>
        </div>
        <div v-if="kind === 'image' && !loading && !error" class="row">
          <button class="btn" :disabled="zoom <= 1" @click="setZoom(zoom / 1.5)">➖</button>
          <button class="btn" @click="setZoom(1)">1:1</button>
          <button class="btn" :disabled="zoom >= 6" @click="setZoom(zoom * 1.5)">➕</button>
        </div>
        <div class="row">
          <button v-if="kind === 'pdf' && inTelegram" class="btn primary" @click="openInBrowser">
            🌐 В браузере
          </button>
          <button class="btn" :disabled="!url" @click="download">⬇️ Скачать</button>
          <button class="btn" @click="emit('close')">Закрыть</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* Просмотр во весь экран: перекрываем общие .modal-content (окно 340px) */
.viewer {
  width: 100vw;
  max-width: 100vw;
  height: 100vh;
  height: 100dvh;
  max-height: 100dvh;
  border-radius: 0;
  padding: 10px 12px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.head {
  display: flex;
  align-items: baseline;
  gap: 8px;
  flex: none;
}

.name {
  overflow-wrap: anywhere;
  font-size: 15px;
  flex: 1;
  min-width: 0;
  text-align: left;
  margin: 0;
}

.counter {
  color: var(--text-secondary);
  font-size: 12px;
}

.close {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 18px;
  padding: 0 2px;
  align-self: center;
}

.note {
  font-size: 12px;
  color: var(--text-secondary);
  white-space: pre-wrap;
  margin: 0;
  text-align: left;
  flex: none;
}

/* min-height: 0 обязателен: без него содержимое распирает колонку и кнопки
   уезжают за нижний край экрана */
.body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  justify-content: center;
}

.state {
  color: var(--text-secondary);
  font-size: 13px;
  padding: 16px 0;
}

.state.err {
  color: #ef4444;
}

.stage {
  flex: 1;
  min-height: 0;
  overflow: auto;
  display: flex;
  align-items: center;
  justify-content: center;
}

.stage.zoomed {
  cursor: move;
}

.media {
  display: block;
  max-width: 100%;
  max-height: 100%;
  margin: 0 auto;
  border-radius: 8px;
  transform-origin: center center;
}

img.media {
  object-fit: contain;
}

audio.media {
  width: 100%;
}

video.media {
  flex: 1;
  min-height: 0;
}

.text,
.doc,
.zip {
  flex: 1;
  min-height: 0;
  overflow: auto;
  text-align: left;
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 10px 12px;
}

.text {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: 12px;
  margin: 0;
}

.doc {
  font-size: 14px;
}

.doc :deep(h1),
.doc :deep(h2),
.doc :deep(h3) {
  font-size: 16px;
  margin: 10px 0 4px;
}

.doc :deep(p) {
  margin: 0 0 6px;
}

.zip ul {
  list-style: none;
  margin: 0;
  padding: 0;
}

.zip li {
  display: flex;
  gap: 8px;
  font-size: 12px;
  padding: 3px 0;
  border-bottom: 1px solid var(--border-color, rgba(128, 128, 128, 0.2));
}

.z-name {
  flex: 1;
  overflow-wrap: anywhere;
}

.z-size {
  color: var(--text-secondary);
}

.pdf {
  flex: 1;
  min-height: 0;
  width: 100%;
  border: none;
  border-radius: 8px;
  background: #fff;
}

.foot {
  flex: none;
  display: flex;
  flex-direction: column;
  gap: 6px;
  /* вырез и домашняя полоса на телефонах: иначе нижняя кнопка под ними */
  padding-bottom: env(safe-area-inset-bottom, 0px);
}

.row {
  display: flex;
  gap: 6px;
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

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}
</style>
