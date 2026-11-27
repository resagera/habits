<script setup lang="ts">
/**
 * Действия над одним файлом: имя и заметка, копия в другую папку, временная
 * ссылка, срок жизни, журнал доступа, общий доступ и удаление.
 *
 * Имя и заметка живут внутри meta_env, то есть шифруются вместе — сервер их
 * не видит. Поэтому переименование это перезапись конверта, а не поле в базе.
 */
import { computed, nextTick, onMounted, ref } from 'vue'
import { autoGrow } from '../../../shared/autoGrow'
import { showToast } from '../../../shared/toast'
import * as vaultApi from '../api'
import { createLinkEnvelopes, rewrapContentKey, sealMeta } from '../crypto'
import { isUnlocked, keyFor, passwordFor } from '../session'
import type { AccessEntry, FileMeta, VaultFile, VaultFolder, VaultLink } from '../types'
import ShareBox from './ShareBox.vue'

const props = defineProps<{
  file: VaultFile
  meta: FileMeta
  folderKey: CryptoKey
  folders: VaultFolder[]
  lockVersion: number
}>()
const emit = defineEmits<{
  saved: [file: VaultFile, meta: FileMeta]
  copied: [file: VaultFile, meta: FileMeta]
  deleted: [id: number]
  close: []
}>()

const name = ref(props.meta.name)
const note = ref(props.meta.note ?? '')
const noteEl = ref<HTMLTextAreaElement | null>(null)
const saving = ref(false)
const confirmDelete = ref(false)

const days = ref(0)
const linkPassword = ref('')
const linkTTL = ref(60)
const linkMax = ref(0)
const linkURL = ref('')
const links = ref<VaultLink[]>([])
const access = ref<AccessEntry[]>([])
const tab = ref<'main' | 'link' | 'access'>('main')

// копировать можно только в открытую паролем свою папку: ключ нужен здесь
const targets = computed(() =>
  props.folders.filter(
    (f) => (props.lockVersion, f.mine && f.id !== props.file.folder_id && isUnlocked(f.id)),
  ),
)

onMounted(() => nextTick(() => autoGrow(noteEl.value)))

async function save() {
  const clean = name.value.trim()
  if (!clean || saving.value) return
  saving.value = true
  try {
    const meta: FileMeta = { ...props.meta, name: clean, note: note.value.trim() || undefined }
    const { file } = await vaultApi.updateFile(props.file.id, {
      meta_env: await sealMeta(props.folderKey, meta),
    })
    emit('saved', file, meta)
    showToast('Сохранено')
  } catch {
    showToast('Не удалось сохранить')
  } finally {
    saving.value = false
  }
}

async function copyTo(targetId: number) {
  const to = keyFor(targetId)
  if (!to) return showToast('Откройте папку паролем')
  try {
    const keyEnv = await rewrapContentKey(props.folderKey, to, props.file.key_env)
    if (!keyEnv) return showToast('Не удалось перевернуть ключ')
    const { file } = await vaultApi.copyFile(props.file.id, {
      folder_id: targetId,
      key_env: keyEnv,
      meta_env: await sealMeta(to, props.meta),
    })
    emit('copied', file, props.meta)
    showToast('Скопировано')
  } catch (e) {
    showToast(e instanceof Error && e.message ? e.message : 'Не удалось скопировать')
  }
}

async function applyExpiry() {
  try {
    await vaultApi.setExpiry([props.file.id], days.value)
    const at = days.value ? new Date(Date.now() + days.value * 86_400_000).toISOString() : null
    emit('saved', { ...props.file, expires_at: at }, props.meta)
    showToast(days.value ? `Удалится через ${days.value} дн.` : 'Срок снят')
  } catch {
    showToast('Не удалось поставить срок')
  }
}

// --- временная ссылка ---

async function loadLinks() {
  try {
    links.value = (await vaultApi.fetchLinks(props.file.id)).links
  } catch {
    /* список ссылок не критичен */
  }
}

async function makeLink() {
  if (linkPassword.value.length < 4) return showToast('Пароль ссылки — от 4 символов')
  try {
    const env = await createLinkEnvelopes(
      props.folderKey,
      props.file.key_env,
      // заметка — своя, в ссылку не уходит
      { name: props.meta.name, type: props.meta.type, size: props.meta.size },
      linkPassword.value,
    )
    if (!env) return showToast('Не удалось собрать ссылку')
    const { path } = await vaultApi.createLink(props.file.id, {
      ...env,
      ttl_minutes: linkTTL.value,
      max_views: linkMax.value,
    })
    linkURL.value = `${location.origin}${import.meta.env.BASE_URL}${path}`
    linkPassword.value = ''
    await loadLinks()
  } catch {
    showToast('Не удалось создать ссылку')
  }
}

async function copyLink() {
  try {
    await navigator.clipboard.writeText(linkURL.value)
    showToast('Ссылка скопирована. Пароль передайте отдельно')
  } catch {
    showToast('Скопируйте ссылку вручную')
  }
}

async function revoke(id: number) {
  try {
    await vaultApi.revokeLink(id)
    links.value = links.value.filter((l) => l.id !== id)
  } catch {
    showToast('Не удалось отозвать')
  }
}

function useFolderPassword() {
  const p = passwordFor(props.file.folder_id)
  if (p) linkPassword.value = p
  else showToast('Пароль папки уже забыт')
}

async function loadAccess() {
  try {
    access.value = (await vaultApi.fetchAccessLog(props.file.id)).entries
  } catch {
    /* журнал не критичен */
  }
}

async function remove() {
  try {
    await vaultApi.deleteFiles([props.file.id])
    emit('deleted', props.file.id)
  } catch {
    showToast('Не удалось удалить')
  }
}

function openTab(next: 'main' | 'link' | 'access') {
  tab.value = next
  if (next === 'link') void loadLinks()
  if (next === 'access') void loadAccess()
}

function fmtDate(s: string): string {
  return new Date(s).toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' })
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content">
      <div class="tabs">
        <button :class="{ on: tab === 'main' }" @click="openTab('main')">Файл</button>
        <button v-if="file.mine" :class="{ on: tab === 'link' }" @click="openTab('link')">Ссылка</button>
        <button v-if="file.mine" :class="{ on: tab === 'access' }" @click="openTab('access')">Журнал</button>
      </div>

      <template v-if="tab === 'main'">
        <label class="lbl">Имя</label>
        <input v-model="name" :disabled="!file.mine" placeholder="Имя файла" />

        <label class="lbl">Заметка (шифруется вместе с именем)</label>
        <textarea
          ref="noteEl"
          v-model="note"
          :disabled="!file.mine"
          rows="1"
          placeholder="Например, откуда файл"
          @input="autoGrow(noteEl)"
        ></textarea>

        <button v-if="file.mine" class="btn primary" :disabled="saving || !name.trim()" @click="save">
          Сохранить
        </button>

        <template v-if="file.mine">
          <label class="lbl">Копия в другую папку</label>
          <select v-if="targets.length" @change="copyTo(Number(($event.target as HTMLSelectElement).value))">
            <option value="">Копировать в…</option>
            <option v-for="f in targets" :key="f.id" :value="f.id">{{ f.name }}</option>
          </select>
          <p v-else class="hint">Откройте другую папку паролем — тогда её можно выбрать.</p>

          <label class="lbl">Самоуничтожение</label>
          <p v-if="file.expires_at" class="hint">Сейчас удалится {{ fmtDate(file.expires_at) }}.</p>
          <div class="row">
            <select v-model.number="days">
              <option :value="0">не удалять</option>
              <option :value="1">через день</option>
              <option :value="7">через неделю</option>
              <option :value="30">через месяц</option>
              <option :value="90">через 3 месяца</option>
            </select>
            <button class="btn" @click="applyExpiry">Применить</button>
          </div>

          <label class="lbl">Общий доступ</label>
          <ShareBox kind="file" :id="file.id" />

          <button v-if="!confirmDelete" class="btn danger" @click="confirmDelete = true">
            🗑 Удалить файл
          </button>
          <button v-else class="btn danger" @click="remove">Точно? Навсегда</button>
        </template>
      </template>

      <template v-else-if="tab === 'link'">
        <p class="hint">
          Ссылка ведёт на этот один файл. Открывший её всё равно вводит пароль — задайте
          отдельный и передайте его другим каналом. Ключ папки в ссылку не уходит.
        </p>
        <div class="row">
          <input v-model="linkPassword" type="password" placeholder="Пароль ссылки" autocomplete="new-password" />
          <button class="btn" @click="useFolderPassword">= папки</button>
        </div>
        <div class="row">
          <select v-model.number="linkTTL">
            <option :value="15">15 минут</option>
            <option :value="60">1 час</option>
            <option :value="1440">сутки</option>
            <option :value="10080">неделя</option>
          </select>
          <select v-model.number="linkMax">
            <option :value="0">без лимита</option>
            <option :value="1">1 открытие</option>
            <option :value="5">5 открытий</option>
            <option :value="20">20 открытий</option>
          </select>
        </div>
        <button class="btn primary" @click="makeLink">Создать ссылку</button>

        <template v-if="linkURL">
          <p class="hint">Ссылка показывается один раз — сохраните её сейчас:</p>
          <input :value="linkURL" readonly />
          <button class="btn" @click="copyLink">📋 Скопировать</button>
        </template>

        <ul v-if="links.length" class="links">
          <li v-for="l in links" :key="l.id">
            <span>до {{ fmtDate(l.expires_at) }} · открытий {{ l.views }}<template v-if="l.max_views">/{{ l.max_views }}</template></span>
            <button class="icon-btn" title="Отозвать" @click="revoke(l.id)">✖</button>
          </li>
        </ul>
      </template>

      <template v-else>
        <p class="hint">Кто открывал файл. Записываются только чужие обращения.</p>
        <ul v-if="access.length" class="links">
          <li v-for="(a, i) in access" :key="i">
            <span>{{ a.via === 'link' ? 'по ссылке' : a.user_name || 'участник' }}</span>
            <span class="when">{{ fmtDate(a.at) }}</span>
          </li>
        </ul>
        <p v-else class="hint">Пока никто не открывал.</p>
      </template>

      <button class="btn" @click="emit('close')">Закрыть</button>
    </div>
  </div>
</template>

<style scoped>
.tabs {
  display: flex;
  gap: 6px;
  margin-bottom: 10px;
}

.tabs button {
  flex: 1;
  padding: 8px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: 13px;
}

.tabs button.on {
  background: var(--accent-color);
  color: #fff;
}

.lbl {
  display: block;
  text-align: left;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 10px 0 4px;
}

.hint {
  font-size: 12px;
  color: var(--text-secondary);
  text-align: left;
  margin: 8px 0;
}

input,
select,
textarea {
  width: 100%;
  padding: 9px 10px;
  border-radius: 8px;
  border: 1px solid var(--border-color, rgba(128, 128, 128, 0.3));
  background: var(--bg-secondary);
  color: var(--text-color);
  font: inherit;
  resize: none;
}

.row {
  display: flex;
  gap: 6px;
  align-items: center;
}

.row .btn {
  width: auto;
  margin-top: 0;
  white-space: nowrap;
}

.links {
  list-style: none;
  margin: 8px 0 0;
  padding: 0;
  text-align: left;
}

.links li {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--text-secondary);
  padding: 4px 0;
}

.links li span:first-child {
  flex: 1;
}

.icon-btn {
  background: none;
  border: none;
  color: var(--text-secondary);
  padding: 2px 6px;
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

.btn:disabled {
  opacity: 0.5;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn.danger {
  background: #ef4444;
  color: #fff;
}
</style>
