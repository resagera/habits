<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { showToast } from '../../shared/toast'
import * as contactsApi from './api'
import {
  contactLabel,
  contactSub,
  inBot,
  KIND_LABELS,
  KIND_PAGES,
  userLabel,
  type Contact,
  type ContactPhoto,
  type IncomingShare,
  type Suggestion,
} from './types'

const contacts = ref<Contact[]>([])
const suggestions = ref<Suggestion[]>([])
const incoming = ref<IncomingShare[]>([])
const loading = ref(true)

const addTo = ref('')
const adding = ref(false)
const confirmDeleteId = ref<number | null>(null)
const busyShareIds = ref(new Set<number>())

// черновики примечаний: id контакта → текст (сохранение по кнопке/blur)
const noteDrafts = ref(new Map<number, string>())

onMounted(load)

async function load() {
  try {
    const [c, inc] = await Promise.all([contactsApi.fetchContacts(), contactsApi.fetchIncoming()])
    contacts.value = c.contacts
    suggestions.value = c.suggestions
    incoming.value = inc.shares
    noteDrafts.value = new Map(c.contacts.map((x) => [x.id, x.note]))
  } catch {
    showToast('Не удалось загрузить контакты')
  } finally {
    loading.value = false
  }
}

async function add(to: string) {
  const target = to.trim()
  if (!target) return
  adding.value = true
  try {
    const { contact } = await contactsApi.addContact(target)
    if (!contacts.value.some((c) => c.id === contact.id)) contacts.value.unshift(contact)
    noteDrafts.value.set(contact.id, contact.note)
    suggestions.value = suggestions.value.filter((s) => s.id !== contact.contact_id)
    addTo.value = ''
    if (inBot(contact)) {
      showToast(`Контакт ${contactLabel(contact)} добавлен 👥`)
    } else {
      showToast('Добавлен контакт «не в боте» — заполнится, когда человек откроет бота ⏳')
    }
  } catch {
    showToast('Не удалось добавить (id или @логин, 4-64 символа)')
  } finally {
    adding.value = false
  }
}

async function toggleAuto(c: Contact) {
  try {
    const { contact } = await contactsApi.updateContact(c.id, { auto_accept: !c.auto_accept })
    Object.assign(c, contact)
  } catch {
    showToast('Не удалось сохранить')
  }
}

function noteDirty(c: Contact): boolean {
  return (noteDrafts.value.get(c.id) ?? '') !== c.note
}

async function saveNote(c: Contact) {
  const note = noteDrafts.value.get(c.id) ?? ''
  if (note === c.note) return
  try {
    const { contact } = await contactsApi.updateContact(c.id, { note })
    Object.assign(c, contact)
    showToast('Примечание сохранено')
  } catch {
    showToast('Не удалось сохранить примечание')
  }
}

async function remove(c: Contact) {
  if (confirmDeleteId.value !== c.id) {
    confirmDeleteId.value = c.id
    setTimeout(() => {
      if (confirmDeleteId.value === c.id) confirmDeleteId.value = null
    }, 3000)
    return
  }
  confirmDeleteId.value = null
  try {
    await contactsApi.deleteContact(c.id)
    contacts.value = contacts.value.filter((x) => x.id !== c.id)
  } catch {
    showToast('Не удалось удалить')
  }
}

// просмотрщик фото (лайтбокс) с удалением
const viewer = ref<{ contact: Contact; photo: ContactPhoto } | null>(null)

async function onPhotoPick(c: Contact, e: Event) {
  const input = e.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  if (!files.length) return
  let ok = 0
  for (const file of files) {
    try {
      c.photos.push(await contactsApi.uploadPhoto(c.id, file))
      ok++
    } catch {
      showToast('Не удалось загрузить фото (jpeg/png/webp, до 5 МБ, максимум 20)')
      break
    }
  }
  if (ok) showToast(ok === 1 ? 'Фото добавлено 📷' : `Добавлено фото: ${ok} 📷`)
}

async function removePhoto(c: Contact, photo: ContactPhoto) {
  try {
    await contactsApi.deletePhoto(c.id, photo.id)
    c.photos = c.photos.filter((p) => p.id !== photo.id)
    viewer.value = null
  } catch {
    showToast('Не удалось удалить фото')
  }
}

// --- входящие ---

async function accept(s: IncomingShare) {
  busyShareIds.value.add(s.id)
  busyShareIds.value = new Set(busyShareIds.value)
  try {
    const { name } = await contactsApi.acceptShare(s.id)
    incoming.value = incoming.value.filter((x) => x.id !== s.id)
    showToast(`«${name}» — принято, смотрите на вкладке ${KIND_PAGES[s.kind]} ✅`)
  } catch (e) {
    if (e instanceof Error && e.message.includes('not')) {
      incoming.value = incoming.value.filter((x) => x.id !== s.id)
      showToast('Источник удалён отправителем')
    } else {
      showToast('Не удалось принять')
    }
  } finally {
    busyShareIds.value.delete(s.id)
    busyShareIds.value = new Set(busyShareIds.value)
  }
}

async function decline(s: IncomingShare) {
  try {
    await contactsApi.declineShare(s.id)
    incoming.value = incoming.value.filter((x) => x.id !== s.id)
  } catch {
    showToast('Не удалось отклонить')
  }
}

function fromLabel(s: IncomingShare): string {
  return s.from_first_name || (s.from_username ? '@' + s.from_username : `#${s.from_id}`)
}

function initial(c: Contact): string {
  return contactLabel(c).replace('@', '').slice(0, 1).toUpperCase() || '?'
}
</script>

<template>
  <div v-if="loading" class="hint">Загрузка…</div>

  <template v-else>
    <!-- входящие шаринги -->
    <div v-if="incoming.length" class="inbox">
      <div class="inbox-title">📥 Входящие ({{ incoming.length }})</div>
      <div v-for="s in incoming" :key="s.id" class="inbox-row">
        <div class="inbox-info">
          <div class="inbox-what">{{ KIND_LABELS[s.kind] }} «{{ s.title }}»</div>
          <div class="inbox-from">от {{ fromLabel(s) }}</div>
        </div>
        <span class="inbox-actions">
          <button class="btn small primary" :disabled="busyShareIds.has(s.id)" @click="accept(s)">
            Принять
          </button>
          <button class="btn small" :disabled="busyShareIds.has(s.id)" @click="decline(s)">
            Отклонить
          </button>
        </span>
      </div>
    </div>

    <p v-if="contacts.length === 0" class="hint">
      Здесь появятся те, с кем вы делитесь данными. Добавьте контакт по id или
      @логину — или отметьте галочку «принимать сразу», чтобы данные от него
      попадали к вам без подтверждения 👇
    </p>

    <!-- контакты -->
    <div v-for="c in contacts" :key="c.id" class="contact">
      <div class="contact-head">
        <span class="ava-wrap">
          <img v-if="c.photos.length" :src="c.photos[0].url" class="ava" alt="" />
          <span v-else class="ava ava-letter">{{ initial(c) }}</span>
        </span>
        <div class="contact-info">
          <div class="contact-name">
            {{ contactLabel(c) }}
            <span v-if="!inBot(c)" class="ext-badge" title="Заполнится, когда человек откроет бота">не в боте</span>
          </div>
          <div class="contact-sub">{{ contactSub(c) }}</div>
        </div>
        <span class="contact-actions">
          <button
            class="icon-btn del"
            :class="{ confirming: confirmDeleteId === c.id }"
            @click="remove(c)"
          >
            {{ confirmDeleteId === c.id ? 'точно?' : '🗑' }}
          </button>
        </span>
      </div>

      <!-- галерея фото -->
      <div class="photos">
        <button
          v-for="p in c.photos"
          :key="p.id"
          class="photo-thumb"
          @click="viewer = { contact: c, photo: p }"
        >
          <img :src="p.url" alt="" />
        </button>
        <label class="photo-add" title="Добавить фото (можно несколько)">
          📷＋
          <input
            type="file"
            accept="image/jpeg,image/png,image/webp"
            multiple
            class="file-input"
            @change="onPhotoPick(c, $event)"
          />
        </label>
      </div>

      <label class="auto-row" :class="{ disabled: !inBot(c) }">
        <input type="checkbox" :checked="c.auto_accept" :disabled="!inBot(c)" @change="toggleAuto(c)" />
        Принимать расшаренные данные сразу
        <span v-if="!inBot(c)" class="auto-hint">(станет доступно после его первого входа)</span>
      </label>

      <div class="note-row">
        <textarea
          :value="noteDrafts.get(c.id) ?? ''"
          class="note-input"
          rows="2"
          maxlength="2000"
          placeholder="Примечание…"
          @input="noteDrafts.set(c.id, ($event.target as HTMLTextAreaElement).value)"
          @blur="saveNote(c)"
        ></textarea>
        <button v-if="noteDirty(c)" class="btn small primary note-save" @click="saveNote(c)">💾</button>
      </div>
    </div>

    <!-- добавление -->
    <form class="add-row" @submit.prevent="add(addTo)">
      <input v-model="addTo" placeholder="id или @логин пользователя…" spellcheck="false" />
      <button type="submit" :disabled="adding || !addTo.trim()">＋ Добавить</button>
    </form>
    <div v-if="suggestions.length" class="suggest">
      <span class="suggest-hint">Вы делились с:</span>
      <button v-for="s in suggestions" :key="s.id" class="chip" @click="add(s.username ? '@' + s.username : String(s.id))">
        ＋ {{ userLabel(s) }}
      </button>
    </div>
  </template>

  <!-- просмотр фото -->
  <Teleport to="body">
    <div v-if="viewer" class="modal viewer" @click.self="viewer = null">
      <div class="viewer-box">
        <img :src="viewer.photo.url" class="viewer-img" alt="" />
        <div class="viewer-actions">
          <button class="btn small danger" @click="removePhoto(viewer.contact, viewer.photo)">🗑 Удалить</button>
          <button class="btn small" @click="viewer = null">Закрыть</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.hint {
  text-align: center;
  color: var(--text-secondary);
  padding: 24px 0;
}

.inbox {
  background: var(--card-color);
  border-radius: 12px;
  padding: 10px 12px;
  margin-bottom: 14px;
  border-left: 3px solid var(--accent-color);
}

.inbox-title {
  font-weight: 700;
  font-size: 14px;
  margin-bottom: 4px;
}

.inbox-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 6px;
  padding: 6px 0;
  border-top: 1px solid var(--bg-secondary);
}

.inbox-info {
  min-width: 0;
}

.inbox-what {
  font-size: 13px;
  font-weight: 600;
  overflow-wrap: anywhere;
}

.inbox-from {
  font-size: 11px;
  color: var(--text-secondary);
}

.inbox-actions {
  display: flex;
  gap: 6px;
  flex: none;
}

.contact {
  background: var(--card-color);
  border-radius: 12px;
  padding: 10px 12px;
  margin-bottom: 10px;
}

.contact-head {
  display: flex;
  align-items: center;
  gap: 10px;
}

.ava-wrap {
  position: relative;
  flex: none;
  cursor: pointer;
}

.ava {
  width: 46px;
  height: 46px;
  border-radius: 50%;
  object-fit: cover;
  display: block;
}

.ava-letter {
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--accent-color);
  color: #fff;
  font-size: 20px;
  font-weight: 700;
}

.file-input {
  display: none;
}

.ext-badge {
  display: inline-block;
  vertical-align: middle;
  margin-left: 4px;
  padding: 1px 7px;
  border-radius: 9px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: 10px;
  font-weight: 500;
  white-space: nowrap;
}

.photos {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}

.photo-thumb {
  width: 52px;
  height: 52px;
  border: none;
  border-radius: 8px;
  padding: 0;
  overflow: hidden;
  background: var(--bg-secondary);
}

.photo-thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.photo-add {
  width: 52px;
  height: 52px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px dashed var(--text-secondary);
  border-radius: 8px;
  color: var(--text-secondary);
  font-size: 14px;
  cursor: pointer;
}

.viewer-box {
  max-width: min(92vw, 480px);
}

.viewer-img {
  width: 100%;
  max-height: 70vh;
  object-fit: contain;
  border-radius: 10px;
  display: block;
}

.viewer-actions {
  display: flex;
  gap: 8px;
  justify-content: center;
  margin-top: 10px;
}

.btn.danger {
  background: #b91c1c;
  color: #fff;
}

.auto-row.disabled {
  opacity: 0.6;
}

.auto-hint {
  font-size: 11px;
  color: var(--text-secondary);
}

.contact-info {
  flex: 1;
  min-width: 0;
}

.contact-name {
  font-weight: 700;
  overflow-wrap: anywhere;
}

.contact-sub {
  font-size: 11px;
  color: var(--text-secondary);
}

.contact-actions {
  flex: none;
  display: flex;
  gap: 2px;
}

.icon-btn {
  background: none;
  border: none;
  padding: 4px 5px;
  font-size: 14px;
}

.icon-btn.del.confirming {
  color: #ef4444;
  font-size: 11px;
  font-weight: 600;
}

.auto-row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  margin-top: 8px;
}

.note-row {
  display: flex;
  align-items: flex-end;
  gap: 6px;
  margin-top: 6px;
}

.note-input {
  flex: 1;
  min-width: 0;
  resize: vertical;
  font-size: 13px;
}

.note-save {
  flex: none;
}

.btn {
  border: none;
  border-radius: 8px;
  padding: 8px 12px;
  background: var(--bg-secondary);
  color: var(--text-color);
}

.btn.small {
  padding: 6px 10px;
  font-size: 12px;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.add-row {
  display: flex;
  gap: 6px;
  margin-top: 12px;
}

.add-row input {
  flex: 1;
  min-width: 0;
}

.add-row button {
  flex: none;
  border: none;
  border-radius: 8px;
  padding: 8px 12px;
  background: var(--accent-color);
  color: #fff;
  white-space: nowrap;
}

.suggest {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  margin-top: 10px;
}

.suggest-hint {
  font-size: 11px;
  color: var(--text-secondary);
}

.chip {
  background: var(--bg-secondary);
  border: none;
  border-radius: 12px;
  padding: 5px 11px;
  font-size: 12px;
  color: var(--text-color);
}

/* карточки-«стекло»: контактные карточки и входящие */
:root[data-card-glass] .contact,
:root[data-card-glass] .inbox {
  backdrop-filter: blur(var(--card-blur, 0px));
  -webkit-backdrop-filter: blur(var(--card-blur, 0px));
}
</style>
