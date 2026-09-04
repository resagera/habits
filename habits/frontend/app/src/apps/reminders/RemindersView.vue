<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import RecipientPicker from '../../components/RecipientPicker.vue'
import { loadCollapsed, saveCollapsed } from '../../shared/collapsed'
import { showToast } from '../../shared/toast'
import { fetchCategories } from '../tracker/api'
import type { Category } from '../tracker/types'
import * as remApi from './api'
import ReminderModal from './components/ReminderModal.vue'
import { buildExport, parseJson, toJson, toText } from './transfer'
import {
  fmtDateTime,
  scheduleText,
  type Reminder,
  type ReminderCategory,
  type ReminderDraft,
} from './types'

const reminders = ref<Reminder[]>([])
const categories = ref<Category[]>([])
const groups = ref<ReminderCategory[]>([])
const loading = ref(true)

const modal = ref(false)
const editing = ref<Reminder | null>(null)
const createInGroup = ref<number | null>(null)

// компактный режим — кнопкой вверху, запоминается локально
const COMPACT_KEY = 'reminders_compact'
const compact = ref(localStorage.getItem(COMPACT_KEY) === '1')

function toggleCompact() {
  compact.value = !compact.value
  localStorage.setItem(COMPACT_KEY, compact.value ? '1' : '0')
}

// свёрнутые категории (0 = «Без категории»), хранятся на сервере
const collapsed = ref(new Set<number>())

function toggleCollapse(id: number) {
  if (collapsed.value.has(id)) collapsed.value.delete(id)
  else collapsed.value.add(id)
  collapsed.value = new Set(collapsed.value)
  saveCollapsed('reminders', collapsed.value)
}

const categoryNames = computed(() => new Map(categories.value.map((c) => [c.id, c.name])))

// группы: свои категории + «Без категории»; без категорий — плоский список
const grouped = computed(() => {
  if (groups.value.length === 0) return null
  const result: { id: number; name: string; items: Reminder[] }[] = groups.value.map((g) => ({
    id: g.id,
    name: g.name,
    items: reminders.value.filter((r) => r.group_id === g.id),
  }))
  const loose = reminders.value.filter((r) => !r.group_id || !groups.value.some((g) => g.id === r.group_id))
  result.push({ id: 0, name: '📂 Без категории', items: loose })
  return result
})

onMounted(async () => {
  loadCollapsed('reminders').then((s) => (collapsed.value = s))
  try {
    const [rem, cats, grp] = await Promise.all([
      remApi.fetchReminders(),
      fetchCategories(),
      remApi.fetchCategories(),
    ])
    reminders.value = rem.reminders
    categories.value = cats.categories
    groups.value = grp.categories
  } catch {
    showToast('Не удалось загрузить напоминания')
  } finally {
    loading.value = false
  }
})

function openCreate(groupId: number | null = null) {
  editing.value = null
  createInGroup.value = groupId
  modal.value = true
}

function openEdit(r: Reminder) {
  editing.value = r
  modal.value = true
}

async function save(draft: ReminderDraft) {
  try {
    if (editing.value) {
      const { reminder } = await remApi.updateReminder(editing.value.id, draft)
      const i = reminders.value.findIndex((x) => x.id === reminder.id)
      if (i >= 0) reminders.value[i] = reminder
    } else {
      const { reminder } = await remApi.createReminder(draft)
      reminders.value.push(reminder)
    }
    modal.value = false
  } catch {
    showToast('Не удалось сохранить')
  }
}

async function remove() {
  if (!editing.value) return
  try {
    await remApi.deleteReminder(editing.value.id)
    reminders.value = reminders.value.filter((x) => x.id !== editing.value!.id)
    modal.value = false
  } catch {
    showToast('Не удалось удалить')
  }
}

async function toggle(r: Reminder) {
  try {
    const { reminder } = await remApi.toggleReminder(r.id, !r.enabled)
    const i = reminders.value.findIndex((x) => x.id === reminder.id)
    if (i >= 0) reminders.value[i] = reminder
  } catch {
    showToast('Не удалось переключить')
  }
}

// --- управление своими категориями ---
const groupModal = ref(false)
const editingGroup = ref<ReminderCategory | null>(null)
const groupName = ref('')
const confirmDeleteGroup = ref(false)

function openAddGroup() {
  editingGroup.value = null
  groupName.value = ''
  confirmDeleteGroup.value = false
  groupModal.value = true
}

function openEditGroup(id: number) {
  const g = groups.value.find((x) => x.id === id)
  if (!g) return
  editingGroup.value = g
  groupName.value = g.name
  confirmDeleteGroup.value = false
  groupModal.value = true
}

async function saveGroup() {
  const name = groupName.value.trim()
  if (!name) return
  try {
    if (editingGroup.value) {
      const { category } = await remApi.renameCategory(editingGroup.value.id, name)
      const i = groups.value.findIndex((x) => x.id === category.id)
      if (i >= 0) groups.value[i] = category
    } else {
      const { category } = await remApi.createCategory(name)
      groups.value.push(category)
    }
    groupModal.value = false
  } catch {
    showToast('Не удалось сохранить категорию')
  }
}

async function removeGroup() {
  const g = editingGroup.value
  if (!g) return
  if (!confirmDeleteGroup.value) {
    confirmDeleteGroup.value = true
    setTimeout(() => (confirmDeleteGroup.value = false), 3500)
    return
  }
  try {
    await remApi.deleteCategory(g.id)
    groups.value = groups.value.filter((x) => x.id !== g.id)
    for (const r of reminders.value) {
      if (r.group_id === g.id) r.group_id = undefined
    }
    groupModal.value = false
  } catch {
    showToast('Не удалось удалить категорию')
  }
}

// --- экспорт / импорт / шаринг категории ---
const exportModal = ref(false)
const exportText = ref('')
const exportJson = ref('')

const importModal = ref(false)
const importText = ref('')
const importing = ref(false)

const shareModal = ref<ReminderCategory | null>(null)
const shareSendTo = ref('')
const shareInviteLink = ref('')
const shareSending = ref(false)

function remindersOf(groupId: number): Reminder[] {
  return reminders.value.filter((r) => r.group_id === groupId)
}

function openExportCat() {
  const g = editingGroup.value
  if (!g) return
  const exp = buildExport(g.name, remindersOf(g.id))
  exportText.value = toText(exp, categoryNames.value)
  exportJson.value = toJson(exp)
  groupModal.value = false
  exportModal.value = true
}

async function copyExport(kind: 'text' | 'json') {
  try {
    await navigator.clipboard.writeText(kind === 'text' ? exportText.value : exportJson.value)
    showToast(kind === 'text' ? 'Текст скопирован 📋' : 'JSON скопирован 📋')
  } catch {
    showToast('Не удалось скопировать')
  }
}

function openImport() {
  importText.value = ''
  importModal.value = true
}

async function doImport() {
  const raw = importText.value.trim()
  if (!raw) return
  let exp
  try {
    exp = parseJson(raw)
  } catch {
    showToast('Не удалось разобрать JSON')
    return
  }
  if (!exp.name) {
    showToast('В JSON нет названия категории')
    return
  }
  importing.value = true
  try {
    const { category } = await remApi.importCategory(exp)
    groups.value.push(category)
    // подтянем созданные напоминания
    const { reminders: fresh } = await remApi.fetchReminders()
    reminders.value = fresh
    importModal.value = false
    showToast(`Категория «${category.name}» импортирована 📥`)
  } catch {
    showToast('Не удалось импортировать')
  } finally {
    importing.value = false
  }
}

async function openShareCat() {
  const g = editingGroup.value
  if (!g) return
  shareModal.value = g
  groupModal.value = false
  shareSendTo.value = ''
  shareInviteLink.value = ''
  try {
    const { link, token } = await remApi.shareCategoryToken(g.id)
    shareInviteLink.value = link || `rem_${token}`
  } catch {
    showToast('Не удалось получить ссылку')
  }
}

async function copyInvite() {
  try {
    await navigator.clipboard.writeText(shareInviteLink.value)
    showToast('Ссылка-приглашение скопирована 🔗')
  } catch {
    showToast('Не удалось скопировать')
  }
}

async function sendCat() {
  const g = shareModal.value
  const to = shareSendTo.value.trim()
  if (!g || !to) return
  shareSending.value = true
  try {
    const { sent_to } = await remApi.sendCategory(g.id, to)
    showToast(`Отправлено ${sent_to.first_name || '@' + sent_to.username || '#' + sent_to.id} 📤`)
    shareModal.value = null
  } catch (e) {
    showToast(e instanceof Error && e.message.includes('not') ? 'Пользователь не найден' : 'Не удалось отправить')
  } finally {
    shareSending.value = false
  }
}
</script>

<template>
  <div class="top-row">
    <p class="notice">🔔 Напоминания приходят от бота. Время — ваше локальное.</p>
    <button class="mode-btn" :title="compact ? 'Обычный режим' : 'Компактный режим'" @click="toggleCompact">
      {{ compact ? '☰' : '≡' }}
    </button>
  </div>

  <div v-if="loading" class="hint">Загрузка…</div>

  <template v-else>
    <p v-if="reminders.length === 0 && groups.length === 0" class="hint">
      Пока нет напоминаний. Создайте первое — разовое, повторяющееся, ежегодное или привязанное к привычке из Tracker 👇
    </p>

    <!-- с категориями: группы со сворачиванием -->
    <template v-if="grouped">
      <div v-for="g in grouped" :key="g.id" class="group">
        <div class="group-header">
          <button class="collapse-btn" @click="toggleCollapse(g.id)">
            <span class="chevron" :class="{ open: !collapsed.has(g.id) }">▸</span>
            {{ g.name }}
            <span class="count">{{ g.items.length }}</span>
          </button>
          <span class="group-actions">
            <button v-if="g.id !== 0" class="icon-btn" title="Настройки категории" @click="openEditGroup(g.id)">⚙️</button>
            <button class="icon-btn" title="Добавить сюда" @click="openCreate(g.id || null)">＋</button>
          </span>
        </div>
        <template v-if="!collapsed.has(g.id)">
          <div
            v-for="r in g.items"
            :key="r.id"
            class="rem-card"
            :class="{ off: !r.enabled, compact }"
            @click="openEdit(r)"
          >
            <div class="rem-info">
              <div class="rem-title">{{ r.kind === 'tracker' ? '📊 ' : '' }}{{ r.title }}</div>
              <div v-if="!compact" class="rem-schedule">
                {{ scheduleText(r, r.category_id ? categoryNames.get(r.category_id) : undefined) }}
              </div>
              <div v-if="r.enabled && r.next_fire_at" class="rem-next">→ {{ fmtDateTime(r.next_fire_at) }}</div>
              <div v-else-if="!r.enabled" class="rem-next">выключено</div>
            </div>
            <button class="switch" :class="{ on: r.enabled }" @click.stop="toggle(r)">
              <span class="knob" />
            </button>
          </div>
          <p v-if="g.items.length === 0" class="group-empty">Пусто</p>
        </template>
      </div>
    </template>

    <!-- без категорий: плоский список -->
    <template v-else>
      <div
        v-for="r in reminders"
        :key="r.id"
        class="rem-card"
        :class="{ off: !r.enabled, compact }"
        @click="openEdit(r)"
      >
        <div class="rem-info">
          <div class="rem-title">{{ r.kind === 'tracker' ? '📊 ' : '' }}{{ r.title }}</div>
          <div v-if="!compact" class="rem-schedule">
            {{ scheduleText(r, r.category_id ? categoryNames.get(r.category_id) : undefined) }}
          </div>
          <div v-if="r.enabled && r.next_fire_at" class="rem-next">→ {{ fmtDateTime(r.next_fire_at) }}</div>
          <div v-else-if="!r.enabled" class="rem-next">выключено</div>
        </div>
        <button class="switch" :class="{ on: r.enabled }" @click.stop="toggle(r)">
          <span class="knob" />
        </button>
      </div>
    </template>

    <button class="add-btn" @click="openCreate(null)">＋ Новое напоминание</button>
    <button class="add-group-btn" @click="openAddGroup">＋ Категория</button>
    <button class="add-group-btn" @click="openImport">⬆️ Импорт категории (JSON)</button>
  </template>

  <ReminderModal
    v-if="modal"
    :key="editing?.id ?? 'new'"
    :reminder="editing"
    :categories="categories"
    :groups="groups"
    :default-group-id="createInGroup"
    @save="save"
    @remove="remove"
    @close="modal = false"
  />

  <!-- модалка категории -->
  <div v-if="groupModal" class="modal" @click.self="groupModal = false">
    <div class="modal-content group-modal">
      <h3>{{ editingGroup ? 'Категория' : 'Новая категория' }}</h3>
      <input v-model="groupName" placeholder="Название" maxlength="100" @keyup.enter="saveGroup" />
      <button class="btn primary" @click="saveGroup">💾 Сохранить</button>
      <template v-if="editingGroup">
        <button class="btn" @click="openShareCat">📤 Поделиться категорией</button>
        <button class="btn" @click="openExportCat">⬇️ Экспорт (текст / JSON)</button>
      </template>
      <button v-if="editingGroup" class="btn danger" @click="removeGroup">
        {{ confirmDeleteGroup ? 'Удалить? Напоминания останутся' : '🗑 Удалить' }}
      </button>
      <button class="btn" @click="groupModal = false">Отмена</button>
    </div>
  </div>

  <!-- поделиться категорией -->
  <div v-if="shareModal" class="modal" @click.self="shareModal = null">
    <div class="modal-content group-modal">
      <h3>Поделиться «{{ shareModal.name }}»</h3>
      <label class="io-label">Пользователю приложения</label>
      <RecipientPicker v-model="shareSendTo" />
      <button class="btn primary" :disabled="shareSending || !shareSendTo.trim()" @click="sendCat">
        {{ shareSending ? 'Отправка…' : '📤 Отправить' }}
      </button>
      <label class="io-label">Или ссылка-приглашение (для друга в Telegram)</label>
      <div class="invite-box">{{ shareInviteLink || 'получаем ссылку…' }}</div>
      <button class="btn" :disabled="!shareInviteLink" @click="copyInvite">🔗 Копировать ссылку</button>
      <p class="io-hint">Копируются напоминания категории, кроме привязанных к привычкам Tracker.</p>
      <button class="btn" @click="shareModal = null">Закрыть</button>
    </div>
  </div>

  <!-- экспорт категории -->
  <div v-if="exportModal" class="modal" @click.self="exportModal = false">
    <div class="modal-content group-modal">
      <h3>Экспорт категории</h3>
      <label class="io-label">Читаемая сводка</label>
      <textarea class="io-box" rows="5" readonly :value="exportText"></textarea>
      <button class="btn" @click="copyExport('text')">📋 Копировать текст</button>
      <label class="io-label">JSON (для импорта)</label>
      <textarea class="io-box mono" rows="6" readonly :value="exportJson"></textarea>
      <button class="btn" @click="copyExport('json')">📋 Копировать JSON</button>
      <button class="btn primary" @click="exportModal = false">Готово</button>
    </div>
  </div>

  <!-- импорт категории -->
  <div v-if="importModal" class="modal" @click.self="importModal = false">
    <div class="modal-content group-modal">
      <h3>Импорт категории</h3>
      <p class="io-hint">Вставьте JSON, полученный из экспорта. Привычки Tracker пропускаются.</p>
      <textarea v-model="importText" class="io-box mono" rows="8" placeholder='{ "name": "…", "reminders": [ … ] }'></textarea>
      <button class="btn primary" :disabled="importing || !importText.trim()" @click="doImport">
        {{ importing ? 'Импорт…' : '⬆️ Импортировать' }}
      </button>
      <button class="btn" @click="importModal = false">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.top-row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin-bottom: 12px;
}

.notice {
  flex: 1;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0;
}

.mode-btn {
  flex: none;
  width: 34px;
  height: 30px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
  font-size: 15px;
}

.hint {
  text-align: center;
  color: var(--text-secondary);
  padding: 24px 0;
}

.group {
  background: var(--card-color);
  border-radius: 10px;
  padding: 8px 10px;
  margin-bottom: 10px;
}

.group-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.collapse-btn {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 6px;
  background: none;
  border: none;
  color: var(--text-color);
  font-weight: 700;
  font-size: 14px;
  text-align: left;
  padding: 4px 0;
}

.chevron {
  display: inline-block;
  transition: transform 0.15s;
  color: var(--text-secondary);
  font-size: 12px;
}

.chevron.open {
  transform: rotate(90deg);
}

.count {
  font-weight: 400;
  font-size: 12px;
  color: var(--text-secondary);
}

.group-actions {
  flex: none;
}

.icon-btn {
  background: none;
  border: none;
  padding: 2px 6px;
  font-size: 14px;
  color: var(--text-color);
}

.group-empty {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 4px 0;
}

.group .rem-card {
  background: var(--bg-secondary);
}

.rem-card {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
  background: var(--card-color);
  border-radius: 10px;
  padding: 10px 12px;
  margin-bottom: 8px;
  cursor: pointer;
}

.rem-card.compact {
  padding: 6px 10px;
  margin-bottom: 5px;
}

.rem-card.compact .rem-title {
  font-size: 13px;
}

.rem-card.compact .rem-next {
  margin-top: 0;
  font-size: 11px;
}

.rem-card.off {
  opacity: 0.55;
}

.rem-info {
  min-width: 0;
}

.rem-title {
  font-weight: 600;
  overflow-wrap: anywhere;
}

.rem-schedule {
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 2px;
}

.rem-next {
  font-size: 12px;
  color: var(--accent-color);
  margin-top: 2px;
}

.switch {
  flex: none;
  width: 44px;
  height: 24px;
  border: none;
  border-radius: 12px;
  background: var(--bg-secondary);
  position: relative;
  transition: background 0.15s;
}

.group .switch {
  background: var(--bg-color);
}

.switch.on,
.group .switch.on {
  background: var(--accent-color);
}

.knob {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: #fff;
  transition: transform 0.15s;
}

.switch.on .knob {
  transform: translateX(20px);
}

.add-btn {
  display: block;
  width: 100%;
  margin-top: 10px;
  padding: 11px;
  border: none;
  border-radius: 8px;
  background: var(--accent-color);
  color: #fff;
}

.add-group-btn {
  display: block;
  width: 100%;
  margin-top: 8px;
  padding: 9px;
  border: none;
  border-radius: 8px;
  background: var(--card-color);
  color: var(--text-secondary);
  font-size: 13px;
}

.group-modal {
  text-align: left;
}

.group-modal h3 {
  text-align: center;
}

.group-modal input {
  width: 100%;
  margin-top: 8px;
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

.io-label {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 12px;
}

.io-box {
  width: 100%;
  margin-top: 6px;
  resize: vertical;
}

.io-box.mono {
  font-family: monospace;
  font-size: 12px;
}

.io-hint {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 8px 0 0;
}

.invite-box {
  margin-top: 6px;
  padding: 8px 10px;
  background: var(--bg-secondary);
  border-radius: 8px;
  font-size: 12px;
  font-family: monospace;
  overflow-wrap: anywhere;
  color: var(--text-secondary);
}

/* карточки-«стекло»: размытие фона под .group (класс неоднозначный —
   правило scoped, чтобы не задеть одноимённые не-карточки) */
:root[data-card-glass] .group {
  backdrop-filter: blur(var(--card-blur, 0px));
  -webkit-backdrop-filter: blur(var(--card-blur, 0px));
}
</style>
