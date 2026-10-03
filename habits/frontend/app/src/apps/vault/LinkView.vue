<script setup lang="ts">
/**
 * Публичная страница временной ссылки на файл сейфа: открывается без
 * авторизации, как страница чтения статьи.
 *
 * Сервер отдаёт шифробайты и конверт ключа, но пароля не знает — расшифровка
 * целиком здесь. Ключ папки в ссылку не уходит: в конверте лежит ключ ровно
 * этого файла, завёрнутый под отдельный пароль ссылки.
 */
import { computed, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import * as vaultApi from './api'
import { decryptWithKey, openLink } from './crypto'
import { docxToHtml, isDocx, isZip, zipList, type ZipEntry } from './office'
import type { FileMeta } from './types'
import type { LinkEnvelopes } from './crypto'

const route = useRoute()
const token = String(route.params.token ?? '')

const envelopes = ref<LinkEnvelopes | null>(null)
const state = ref<'loading' | 'ask' | 'open' | 'gone'>('loading')
const password = ref('')
const busy = ref(false)
const error = ref('')

const meta = ref<FileMeta | null>(null)
const url = ref('')
const text = ref('')
const html = ref('')
const entries = ref<ZipEntry[]>([])

const kind = computed(() => {
  const m = meta.value
  if (!m) return 'other'
  const t = m.type || ''
  const name = m.name.toLowerCase()
  if (t.startsWith('image/')) return 'image'
  if (t.startsWith('audio/')) return 'audio'
  if (t.startsWith('video/')) return 'video'
  if (t === 'application/pdf' || name.endsWith('.pdf')) return 'pdf'
  if (isDocx(m)) return 'docx'
  if (isZip(m)) return 'zip'
  if (t.startsWith('text/') || t === 'application/json' || /\.(txt|md|csv|json|log|ya?ml)$/.test(name)) {
    return 'text'
  }
  return 'other'
})

async function loadLink() {
  try {
    const data = await vaultApi.fetchPublicLink(token)
    envelopes.value = {
      kdf_salt: data.kdf_salt,
      kdf_iter: data.kdf_iter,
      key_env: data.key_env,
      meta_env: data.meta_env,
    }
    state.value = 'ask'
  } catch {
    state.value = 'gone'
  }
}

void loadLink()

async function submit() {
  const env = envelopes.value
  if (!env || busy.value || !password.value) return
  busy.value = true
  error.value = ''
  try {
    const opened = await openLink(password.value, env)
    if (!opened) {
      error.value = 'Пароль не подходит'
      return
    }
    password.value = ''
    meta.value = opened.meta
    const data = await vaultApi.fetchPublicLinkBlob(token)
    const blob = await decryptWithKey(data, opened.key, opened.meta.type)
    if (!blob) {
      error.value = 'Файл повреждён'
      return
    }
    url.value = URL.createObjectURL(blob)
    if (kind.value === 'text') text.value = await blob.slice(0, 1 << 20).text()
    if (kind.value === 'docx') html.value = await docxToHtml(blob)
    if (kind.value === 'zip') entries.value = await zipList(blob)
    state.value = 'open'
  } catch {
    // истёкшую или исчерпанную ссылку сервер не отличает от несуществующей
    state.value = 'gone'
  } finally {
    busy.value = false
  }
}

onUnmounted(() => {
  if (url.value) URL.revokeObjectURL(url.value)
})

function download() {
  if (!url.value || !meta.value) return
  const a = document.createElement('a')
  a.href = url.value
  a.download = meta.value.name
  a.click()
}

function fmtSize(bytes: number): string {
  if (bytes >= 1 << 20) return (bytes / (1 << 20)).toFixed(1) + ' МБ'
  if (bytes >= 1 << 10) return Math.round(bytes / (1 << 10)) + ' КБ'
  return bytes + ' Б'
}
</script>

<template>
  <div class="link-page">
    <h1 class="title">🔐 Файл из сейфа</h1>

    <p v-if="state === 'loading'" class="hint">Загрузка…</p>

    <p v-else-if="state === 'gone'" class="hint">
      Ссылка не найдена, истекла или исчерпала число открытий.
    </p>

    <form v-else-if="state === 'ask'" class="ask" @submit.prevent="submit">
      <p class="hint">Файл зашифрован. Введите пароль, который вам передали отдельно.</p>
      <input v-model="password" type="password" placeholder="Пароль" autocomplete="off" />
      <p v-if="error" class="err">{{ error }}</p>
      <button class="btn primary" :disabled="busy || !password">
        {{ busy ? 'Расшифровываем…' : 'Открыть' }}
      </button>
      <p class="note">Пароль остаётся в браузере: сервер его не получает и не знает.</p>
    </form>

    <template v-else>
      <h2 class="name">{{ meta?.name }}</h2>
      <img v-if="kind === 'image'" :src="url" class="media" alt="" />
      <audio v-else-if="kind === 'audio'" :src="url" controls class="media"></audio>
      <video v-else-if="kind === 'video'" :src="url" controls class="media"></video>
      <pre v-else-if="kind === 'text'" class="text">{{ text }}</pre>
      <!-- разметка собрана из экранированного текста документа -->
      <div v-else-if="kind === 'docx'" class="doc" v-html="html"></div>
      <ul v-else-if="kind === 'zip'" class="zip">
        <li v-for="e in entries" :key="e.name">{{ e.name }} — {{ fmtSize(e.size) }}</li>
      </ul>
      <iframe v-else-if="kind === 'pdf'" :src="url" class="pdf"></iframe>
      <p v-else class="hint">Такой файл показать нечем — его можно скачать.</p>

      <button class="btn" @click="download">⬇️ Скачать</button>
    </template>
  </div>
</template>

<style scoped>
.link-page {
  max-width: 720px;
  margin: 0 auto;
  padding: 16px 12px 40px;
}

.title {
  font-size: 20px;
  margin: 0 0 12px;
}

.name {
  font-size: 15px;
  overflow-wrap: anywhere;
  margin: 0 0 10px;
}

.hint,
.note {
  color: var(--text-secondary);
  font-size: 13px;
}

.note {
  font-size: 11px;
}

.err {
  color: #ef4444;
  font-size: 13px;
}

.ask input {
  width: 100%;
  padding: 10px;
  border-radius: 8px;
  border: 1px solid var(--border-color, rgba(128, 128, 128, 0.3));
  background: var(--bg-secondary);
  color: var(--text-color);
  font: inherit;
}

.media {
  display: block;
  max-width: 100%;
  max-height: 70vh;
  margin: 0 auto;
  border-radius: 8px;
}

audio.media {
  width: 100%;
}

.text,
.doc {
  max-height: 70vh;
  overflow: auto;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 10px;
  font-size: 13px;
}

.doc {
  white-space: normal;
}

.zip {
  font-size: 13px;
  padding-left: 18px;
}

.pdf {
  width: 100%;
  height: 75vh;
  border: none;
  border-radius: 8px;
  background: #fff;
}

.btn {
  display: block;
  width: 100%;
  margin-top: 12px;
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

.btn:disabled {
  opacity: 0.5;
}
</style>
