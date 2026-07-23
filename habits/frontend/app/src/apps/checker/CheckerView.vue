<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import RecipientPicker from '../../components/RecipientPicker.vue'
import { loadCollapsed, saveCollapsed } from '../../shared/collapsed'
import { showToast } from '../../shared/toast'
import * as checkerApi from './api'
import type { CheckGroup } from './types'
import CheckGroupCard from './components/CheckGroupCard.vue'
import TemplatesSection from './components/TemplatesSection.vue'
import { groupRelevant, normQuery } from './search'
import { buildExport, parseAny, toJson, toText } from './transfer'

// предельная глубина вложенности (должна совпадать с backend MaxCheckerDepth)
const MAX_DEPTH = 20

const groups = ref<CheckGroup[]>([])
const loading = ref(true)
const newGroupName = ref('')

// глобальный поиск по странице: имена групп + пункты (включая подгруппы)
const search = ref('')
const searchFilter = computed(() => normQuery(search.value))

// группы верхнего уровня; при активном поиске — только релевантные
const topGroups = computed(() => groups.value.filter((g) => g.parent_id === null))
const visibleTopGroups = computed(() =>
  topGroups.value.filter((g) => groupRelevant(g, groups.value, searchFilter.value)),
)

const settingsGroup = ref<CheckGroup | null>(null)
const settingsName = ref('')
const settingsHideDone = ref(false)
const confirmDeleteGroup = ref(false)
const subName = ref('')
const templatesSection = ref<InstanceType<typeof TemplatesSection>>()

// шаринг группы (ссылка-приглашение + отправка пользователю)
const shareGroup = ref<CheckGroup | null>(null)
const shareSendTo = ref('')
const shareInviteLink = ref('')
const shareSending = ref(false)

// экспорт/импорт
const exportModal = ref(false)
const exportText = ref('')
const exportJson = ref('')
const importModal = ref(false)
const importText = ref('')
const importing = ref(false)

function openExport() {
  const group = settingsGroup.value
  if (!group) return
  const tree = buildExport(group, groups.value)
  exportText.value = toText(tree)
  exportJson.value = toJson(tree)
  settingsGroup.value = null
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
  let tree
  try {
    tree = parseAny(raw)
  } catch {
    showToast('Не удалось разобрать (проверьте текст/JSON)')
    return
  }
  if (!tree.name) {
    showToast('Не найдено название группы (первая строка)')
    return
  }
  importing.value = true
  try {
    await checkerApi.importGroup(tree)
    await load() // подгруппы тоже создались — перезагружаем список
    importModal.value = false
    showToast(`Группа «${tree.name}» импортирована 📥`)
  } catch {
    showToast('Не удалось импортировать')
  } finally {
    importing.value = false
  }
}

// глубина группы: группа верхнего уровня = 1, каждая вложенность +1
function depthOf(group: CheckGroup): number {
  const byId = new Map(groups.value.map((g) => [g.id, g]))
  let d = 1
  let cur: CheckGroup | undefined = group
  while (cur && cur.parent_id !== null) {
    cur = byId.get(cur.parent_id)
    d++
  }
  return d
}
const settingsDepth = computed(() => (settingsGroup.value ? depthOf(settingsGroup.value) : 0))
const canAddSub = computed(() => settingsDepth.value < MAX_DEPTH)

async function addSubgroup() {
  const parent = settingsGroup.value
  const name = subName.value.trim()
  if (!parent || !name) return
  if (!canAddSub.value) {
    showToast(`Предел вложенности — ${MAX_DEPTH} уровней`)
    return
  }
  try {
    const { group } = await checkerApi.createGroup(name, parent.id)
    groups.value.push(group)
    subName.value = ''
    settingsGroup.value = null
    showToast('Подгруппа добавлена')
  } catch (e) {
    const code = (e as { code?: string }).code
    showToast(code === 'too_deep' ? `Предел вложенности — ${MAX_DEPTH} уровней` : 'Не удалось добавить подгруппу')
  }
}

async function openShareGroup() {
  const group = settingsGroup.value
  if (!group) return
  shareGroup.value = group
  settingsGroup.value = null
  shareSendTo.value = ''
  shareInviteLink.value = ''
  try {
    const { link, token } = await checkerApi.groupShareToken(group.id)
    shareInviteLink.value = link || `chg_${token}`
  } catch {
    showToast('Не удалось получить ссылку')
  }
}

async function copyGroupInvite() {
  try {
    await navigator.clipboard.writeText(shareInviteLink.value)
    showToast('Ссылка-приглашение скопирована 🔗')
  } catch {
    showToast('Не удалось скопировать')
  }
}

async function sendGroupTo() {
  const group = shareGroup.value
  const to = shareSendTo.value.trim()
  if (!group || !to) return
  shareSending.value = true
  try {
    const { sent_to } = await checkerApi.sendGroup(group.id, to)
    showToast(`Отправлено ${sent_to.first_name || '@' + sent_to.username || '#' + sent_to.id} 📤`)
    shareGroup.value = null
  } catch (e) {
    showToast(e instanceof Error && e.message.includes('not') ? 'Пользователь не найден' : 'Не удалось отправить')
  } finally {
    shareSending.value = false
  }
}

/** Сохраняет текущую группу как многоразовый шаблон. */
async function saveAsTemplate() {
  const group = settingsGroup.value
  if (!group) return
  try {
    await checkerApi.createTemplate(
      group.name,
      group.items.map((i) => i.name),
    )
    await templatesSection.value?.reload()
    settingsGroup.value = null
    showToast('Сохранено как шаблон 📋')
  } catch {
    showToast('Не удалось сохранить шаблон')
  }
}

// свёрнутые группы — состояние на сервере
const collapsed = ref(new Set<number>())

function toggleCollapse(id: number) {
  if (collapsed.value.has(id)) collapsed.value.delete(id)
  else collapsed.value.add(id)
  collapsed.value = new Set(collapsed.value)
  saveCollapsed('checker', collapsed.value)
}

onMounted(() => {
  loadCollapsed('checker').then((s) => (collapsed.value = s))
  load()
})

async function load() {
  try {
    groups.value = (await checkerApi.fetchGroups()).groups
  } catch {
    showToast('Не удалось загрузить списки')
  } finally {
    loading.value = false
  }
}

async function addGroup() {
  const name = newGroupName.value.trim()
  if (!name) return
  try {
    const { group } = await checkerApi.createGroup(name)
    groups.value.push(group)
    newGroupName.value = ''
  } catch {
    showToast('Не удалось создать группу')
  }
}

function openSettings(group: CheckGroup) {
  settingsGroup.value = group
  settingsName.value = group.name
  settingsHideDone.value = group.hide_done
  confirmDeleteGroup.value = false
  subName.value = ''
}

async function saveSettings() {
  const group = settingsGroup.value
  if (!group) return
  const name = settingsName.value.trim()
  const patch: { name?: string; hide_done?: boolean } = {}
  if (name && name !== group.name) patch.name = name
  if (settingsHideDone.value !== group.hide_done) patch.hide_done = settingsHideDone.value
  if (patch.name === undefined && patch.hide_done === undefined) {
    settingsGroup.value = null
    return
  }
  try {
    const { group: updated } = await checkerApi.updateGroup(group.id, patch)
    group.name = updated.name
    group.hide_done = updated.hide_done
    settingsGroup.value = null
  } catch {
    showToast('Не удалось сохранить')
  }
}

async function removeGroup() {
  const group = settingsGroup.value
  if (!group) return
  try {
    await checkerApi.deleteGroup(group.id)
    // на бэкенде подгруппы удаляются каскадом (любой глубины) — убираем и их локально
    const doomed = new Set<number>([group.id])
    let grew = true
    while (grew) {
      grew = false
      for (const g of groups.value) {
        if (g.parent_id !== null && doomed.has(g.parent_id) && !doomed.has(g.id)) {
          doomed.add(g.id)
          grew = true
        }
      }
    }
    groups.value = groups.value.filter((g) => !doomed.has(g.id))
    settingsGroup.value = null
  } catch {
    showToast('Не удалось удалить группу')
  }
}
</script>

<template>
  <div v-if="loading" class="loading">Загрузка…</div>

  <template v-else>
    <TemplatesSection ref="templatesSection" @started="groups.push($event)" />

    <div v-if="groups.length" class="page-search">
      <input v-model="search" placeholder="🔍 Поиск по группам и пунктам…" />
      <button v-if="search" class="clear-search" title="Очистить" @click="search = ''">✕</button>
    </div>

    <p v-if="groups.length === 0" class="empty">Пока нет ни одного списка — создайте первый 👇</p>
    <p v-else-if="searchFilter && visibleTopGroups.length === 0" class="empty">
      Ничего не найдено по «{{ search.trim() }}»
    </p>

    <CheckGroupCard
      v-for="group in visibleTopGroups"
      :key="group.id"
      :group="group"
      :groups="groups"
      :collapsed-set="collapsed"
      :filter="searchFilter"
      @toggle-collapse="toggleCollapse"
      @open-settings="openSettings"
    />

    <form class="add-group" @submit.prevent="addGroup">
      <input v-model="newGroupName" placeholder="Новая группа…" maxlength="200" />
      <button type="submit">Добавить группу</button>
    </form>
    <button class="import-btn" @click="openImport">⬆️ Импорт группы (текст / JSON)</button>
  </template>

  <div v-if="settingsGroup" class="modal" @click.self="settingsGroup = null">
    <div class="modal-content">
      <h3>{{ settingsGroup.parent_id === null ? 'Настройки группы' : 'Настройки подгруппы' }}</h3>
      <input v-model="settingsName" type="text" maxlength="200" class="name-input" />
      <label class="hide-done-line">
        <input v-model="settingsHideDone" type="checkbox" />
        <span>Скрывать выполненное</span>
      </label>
      <button class="btn primary" @click="saveSettings">💾 Сохранить</button>

      <div v-if="canAddSub" class="sub-add">
        <input v-model="subName" placeholder="Название подгруппы…" maxlength="200" @keyup.enter="addSubgroup" />
        <button class="btn ghost" :disabled="!subName.trim()" @click="addSubgroup">➕ Подгруппа</button>
      </div>
      <p v-else class="depth-hint">Достигнут предел вложенности — {{ MAX_DEPTH }} уровней</p>

      <template v-if="settingsGroup.parent_id === null">
        <button class="btn" @click="openShareGroup">📤 Поделиться группой</button>
        <button class="btn" @click="openExport">⬇️ Экспорт (текст / JSON)</button>
        <button class="btn" @click="saveAsTemplate">📋 Сохранить как шаблон</button>
      </template>

      <button v-if="!confirmDeleteGroup" class="btn danger" @click="confirmDeleteGroup = true">
        🗑 Удалить {{ settingsGroup.parent_id === null ? 'группу' : 'подгруппу' }}
      </button>
      <button v-else class="btn danger" @click="removeGroup">
        Точно удалить? Пункты будут потеряны
      </button>
      <button class="btn" @click="settingsGroup = null">Отмена</button>
    </div>
  </div>

  <!-- поделиться группой -->
  <div v-if="shareGroup" class="modal" @click.self="shareGroup = null">
    <div class="modal-content share-modal">
      <h3>Поделиться «{{ shareGroup.name }}»</h3>

      <label class="field-label">Пользователю приложения</label>
      <RecipientPicker v-model="shareSendTo" />
      <button class="btn primary" :disabled="shareSending || !shareSendTo.trim()" @click="sendGroupTo">
        {{ shareSending ? 'Отправка…' : '📤 Отправить' }}
      </button>

      <label class="field-label">Или ссылка-приглашение (для любого друга в Telegram)</label>
      <div class="invite-box">{{ shareInviteLink || 'получаем ссылку…' }}</div>
      <button class="btn" :disabled="!shareInviteLink" @click="copyGroupInvite">🔗 Копировать ссылку</button>
      <p class="share-hint">
        Друг откроет ссылку, запустит приложение — и копия списка (с подгруппами)
        добавится ему автоматически.
      </p>

      <button class="btn" @click="shareGroup = null">Закрыть</button>
    </div>
  </div>

  <!-- экспорт -->
  <div v-if="exportModal" class="modal" @click.self="exportModal = false">
    <div class="modal-content share-modal">
      <h3>Экспорт группы</h3>
      <label class="field-label">Простой текст</label>
      <textarea class="io-box" rows="6" readonly :value="exportText"></textarea>
      <button class="btn" @click="copyExport('text')">📋 Копировать текст</button>
      <label class="field-label">JSON</label>
      <textarea class="io-box mono" rows="6" readonly :value="exportJson"></textarea>
      <button class="btn" @click="copyExport('json')">📋 Копировать JSON</button>
      <button class="btn primary" @click="exportModal = false">Готово</button>
    </div>
  </div>

  <!-- импорт -->
  <div v-if="importModal" class="modal" @click.self="importModal = false">
    <div class="modal-content share-modal">
      <h3>Импорт группы</h3>
      <p class="share-hint">
        Вставьте текст или JSON. Текст: первая строка — название, пункты «- …»,
        подгруппы «# …».
      </p>
      <textarea
        v-model="importText"
        class="io-box mono"
        rows="8"
        placeholder="Сборы в поездку&#10;- Паспорт&#10;- Зарядка&#10;# Документы&#10;- Билеты"
      ></textarea>
      <button class="btn primary" :disabled="importing || !importText.trim()" @click="doImport">
        {{ importing ? 'Импорт…' : '⬆️ Импортировать' }}
      </button>
      <button class="btn" @click="importModal = false">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.loading,
.empty {
  text-align: center;
  color: var(--text-secondary);
  padding: 32px 0;
}

.page-search {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 12px;
}

.page-search input {
  flex: 1;
  min-width: 0;
}

.clear-search {
  flex: none;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  padding: 8px 12px;
  color: var(--text-secondary);
}

.add-group {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 8px;
}

.add-group button {
  padding: 10px;
  background: var(--accent-color);
  border: none;
  border-radius: 8px;
  color: #fff;
}

.name-input {
  width: 100%;
}

.hide-done-line {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
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

.btn.ghost {
  margin-top: 0;
  flex: none;
  width: auto;
  padding: 10px 12px;
  white-space: nowrap;
}

.sub-add {
  display: flex;
  gap: 6px;
  margin-top: 10px;
}

.depth-hint {
  margin-top: 10px;
  font-size: 12px;
  color: var(--text-secondary);
}

.sub-add input {
  flex: 1;
  min-width: 0;
}

.share-modal {
  text-align: left;
}

.share-modal h3 {
  text-align: center;
}

.field-label {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 12px;
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

.share-hint {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 8px 0 0;
}

.import-btn {
  display: block;
  width: 100%;
  margin-top: 8px;
  padding: 9px;
  border: 1px dashed var(--text-secondary);
  border-radius: 8px;
  background: none;
  color: var(--text-secondary);
  font-size: 13px;
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
</style>
