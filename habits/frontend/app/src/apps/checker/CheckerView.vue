<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import RecipientPicker from '../../components/RecipientPicker.vue'
import { loadCollapsed, saveCollapsed } from '../../shared/collapsed'
import { showToast } from '../../shared/toast'
import * as checkerApi from './api'
import type { TrashGroup } from './api'
import type { CheckGroup } from './types'
import CheckGroupCard from './components/CheckGroupCard.vue'
import HistoryCalendar from './components/HistoryCalendar.vue'
import TemplatesSection from './components/TemplatesSection.vue'
import { isoToLocalInput, localInputToIso } from './datetime'
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

// группы верхнего уровня (по position — для живой перестановки DnD);
// при активном поиске — только релевантные
const topGroups = computed(() =>
  groups.value.filter((g) => g.parent_id === null).sort((a, b) => a.position - b.position || a.id - b.id),
)
const visibleTopGroups = computed(() =>
  topGroups.value.filter((g) => groupRelevant(g, groups.value, searchFilter.value)),
)

const settingsGroup = ref<CheckGroup | null>(null)
const settingsName = ref('')
const settingsHideDone = ref(false)
const settingsProgressMode = ref(false)
const settingsParentId = ref<number | null>(null)
const confirmDeleteGroup = ref(false)

// повторяющийся список
const recurPeriod = ref('none')
const recurTime = ref('06:00')
const recurDow = ref(1)
const recurDom = ref(1)
const recurSaving = ref(false)
const confirmResetNow = ref(false)
const calendarGroupId = ref<number | null>(null)
const groupRemind = ref('') // datetime-local для напоминания о списке

function minuteToTime(m: number): string {
  return `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`
}
function timeToMinute(t: string): number {
  const [h, m] = t.split(':').map(Number)
  return (h || 0) * 60 + (m || 0)
}
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

// совместный доступ (участники общего списка)
interface ShareUser {
  id: number
  username: string
  first_name: string
}
const collabTo = ref('')
const collabBusy = ref(false)
const participants = ref<ShareUser[]>([])

function participantLabel(u: ShareUser): string {
  return u.first_name || (u.username ? '@' + u.username : '#' + u.id)
}

async function openShareGroup() {
  const group = settingsGroup.value
  if (!group) return
  shareGroup.value = group
  settingsGroup.value = null
  shareSendTo.value = ''
  shareInviteLink.value = ''
  collabTo.value = ''
  participants.value = []
  try {
    const { link, token } = await checkerApi.groupShareToken(group.id)
    shareInviteLink.value = link || `chg_${token}`
  } catch {
    showToast('Не удалось получить ссылку')
  }
  try {
    participants.value = (await checkerApi.listGroupShares(group.id)).users
  } catch {
    /* нет участников или ошибка — блок просто пустой */
  }
}

async function openCollab() {
  const group = shareGroup.value
  const to = collabTo.value.trim()
  if (!group || !to || collabBusy.value) return
  collabBusy.value = true
  try {
    const { shared_with, queued } = await checkerApi.shareGroupAccess(group.id, to)
    if (!queued && !participants.value.some((u) => u.id === shared_with.id)) {
      participants.value.push(shared_with)
    }
    collabTo.value = ''
    showToast(queued ? 'Приглашение отправлено 📨' : `Доступ открыт для ${participantLabel(shared_with)} 👥`)
  } catch (e) {
    showToast(e instanceof Error && e.message.includes('not') ? 'Пользователь не найден' : 'Не удалось открыть доступ')
  } finally {
    collabBusy.value = false
  }
}

async function revokeCollab(u: ShareUser) {
  const group = shareGroup.value
  if (!group) return
  try {
    await checkerApi.revokeGroupShare(group.id, u.id)
    participants.value = participants.value.filter((x) => x.id !== u.id)
  } catch {
    showToast('Не удалось отозвать доступ')
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
    // сохраняем всё поддерево (с подгруппами) в шаблон
    const tree = buildExport(group, groups.value)
    await checkerApi.createTemplate(group.name, { items: tree.items, subgroups: tree.subgroups })
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
  settingsProgressMode.value = group.progress_mode
  settingsParentId.value = group.parent_id
  confirmDeleteGroup.value = false
  confirmBulkDelete.value = false
  confirmResetNow.value = false
  subName.value = ''
  recurPeriod.value = group.reset_period || 'none'
  recurTime.value = minuteToTime(group.reset_minute ?? 360)
  recurDow.value = group.reset_dow ?? 1
  recurDom.value = group.reset_dom ?? 1
  groupRemind.value = group.remind_at ? isoToLocalInput(group.remind_at) : ''
}

async function saveGroupReminder() {
  const group = settingsGroup.value
  if (!group) return
  const iso = groupRemind.value ? localInputToIso(groupRemind.value) : null
  try {
    await checkerApi.setGroupReminder(group.id, iso)
    group.remind_at = iso
    showToast(iso ? 'Напоминание о списке сохранено ⏰' : 'Напоминание снято')
  } catch {
    showToast('Не удалось сохранить напоминание')
  }
}

async function saveRecurrence() {
  const group = settingsGroup.value
  if (!group || recurSaving.value) return
  recurSaving.value = true
  try {
    const { group: updated } = await checkerApi.setRecurring(group.id, {
      period: recurPeriod.value,
      minute: timeToMinute(recurTime.value),
      dow: recurDow.value,
      dom: recurDom.value,
      tz_off: -new Date().getTimezoneOffset(), // минут к востоку от UTC
    })
    group.reset_period = updated.reset_period
    group.reset_minute = updated.reset_minute
    group.reset_dow = updated.reset_dow
    group.reset_dom = updated.reset_dom
    showToast(recurPeriod.value === 'none' ? 'Повтор отключён' : 'Расписание сохранено 🔁')
  } catch {
    showToast('Не удалось сохранить расписание')
  } finally {
    recurSaving.value = false
  }
}

async function resetNowAction() {
  const group = settingsGroup.value
  if (!group) return
  if (!confirmResetNow.value) {
    confirmResetNow.value = true
    setTimeout(() => (confirmResetNow.value = false), 3000)
    return
  }
  confirmResetNow.value = false
  try {
    await checkerApi.resetNow(group.id)
    settingsGroup.value = null
    await load()
    showToast('Список сброшен, снимок сохранён 🔄')
  } catch {
    showToast('Не удалось сбросить')
  }
}

function openCalendar() {
  const group = settingsGroup.value
  if (!group) return
  calendarGroupId.value = group.id
  settingsGroup.value = null
}

// id группы и всех её потомков — их нельзя выбрать новым родителем (цикл)
function descendantIds(id: number): Set<number> {
  const out = new Set<number>([id])
  let grew = true
  while (grew) {
    grew = false
    for (const g of groups.value) {
      if (g.parent_id !== null && out.has(g.parent_id) && !out.has(g.id)) {
        out.add(g.id)
        grew = true
      }
    }
  }
  return out
}

// варианты родителя: «верхний уровень» + все группы полным путём, кроме
// самой группы и её поддерева
const parentOptions = computed<{ id: number | null; label: string }[]>(() => {
  const g = settingsGroup.value
  if (!g) return [{ id: null, label: '🏠 Верхний уровень' }]
  const excluded = descendantIds(g.id)
  const out: { id: number | null; label: string }[] = [{ id: null, label: '🏠 Верхний уровень' }]
  const walk = (parentId: number | null, prefix: string) => {
    for (const x of groups.value.filter((z) => z.parent_id === parentId)) {
      if (excluded.has(x.id)) continue // всё поддерево исключаем
      const label = prefix ? prefix + ' › ' + x.name : x.name
      out.push({ id: x.id, label })
      walk(x.id, label)
    }
  }
  walk(null, '')
  return out
})

// --- история изменений списка ---
const historyModal = ref(false)
const historyList = ref<import('./api').HistoryEntry[]>([])
const historyLoading = ref(false)

async function openHistory() {
  const group = settingsGroup.value
  if (!group) return
  historyModal.value = true
  historyLoading.value = true
  historyList.value = []
  settingsGroup.value = null
  try {
    historyList.value = (await checkerApi.groupHistory(group.id)).history
  } catch {
    showToast('Не удалось загрузить историю')
  } finally {
    historyLoading.value = false
  }
}

function fmtHistoryTime(iso: string): string {
  const d = new Date(iso)
  const now = Date.now()
  const mins = Math.floor((now - d.getTime()) / 60000)
  if (mins < 1) return 'только что'
  if (mins < 60) return `${mins} мин назад`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours} ч назад`
  return d.toLocaleDateString('ru-RU', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
}

// участник покидает общий список (пропадает у него, у владельца остаётся)
async function leaveGroup() {
  const group = settingsGroup.value
  if (!group) return
  try {
    await checkerApi.leaveSharedGroup(group.id)
    const doomed = descendantIds(group.id)
    groups.value = groups.value.filter((g) => !doomed.has(g.id))
    settingsGroup.value = null
    showToast('Вы покинули список')
  } catch {
    showToast('Не удалось покинуть список')
  }
}

async function duplicateGroup() {
  const group = settingsGroup.value
  if (!group) return
  try {
    await checkerApi.duplicateGroup(group.id)
    await load()
    settingsGroup.value = null
    showToast('Дубликат создан 📑')
  } catch {
    showToast('Не удалось дублировать')
  }
}

// массовые действия над прямыми пунктами группы
const confirmBulkDelete = ref(false)
async function bulkAction(action: 'check_all' | 'uncheck_all' | 'delete_done') {
  const group = settingsGroup.value
  if (!group) return
  if (action === 'delete_done' && !confirmBulkDelete.value) {
    confirmBulkDelete.value = true
    setTimeout(() => (confirmBulkDelete.value = false), 3000)
    return
  }
  confirmBulkDelete.value = false
  try {
    const { items } = await checkerApi.bulkGroupItems(group.id, action)
    group.items = items
  } catch {
    showToast('Не удалось выполнить действие')
  }
}

async function saveSettings() {
  const group = settingsGroup.value
  if (!group) return
  const name = settingsName.value.trim()
  const patch: { name?: string; hide_done?: boolean; progress_mode?: boolean } = {}
  if (name && name !== group.name) patch.name = name
  if (settingsHideDone.value !== group.hide_done) patch.hide_done = settingsHideDone.value
  if (settingsProgressMode.value !== group.progress_mode) {
    patch.progress_mode = settingsProgressMode.value
  }
  const parentChanged = settingsParentId.value !== group.parent_id
  const fieldsChanged =
    patch.name !== undefined || patch.hide_done !== undefined || patch.progress_mode !== undefined
  if (!fieldsChanged && !parentChanged) {
    settingsGroup.value = null
    return
  }
  try {
    if (fieldsChanged) {
      const { group: updated } = await checkerApi.updateGroup(group.id, patch)
      group.name = updated.name
      group.hide_done = updated.hide_done
      group.progress_mode = updated.progress_mode
      // режим выключили — сервер погасил флаги пунктов, повторяем это у себя
      if (!group.progress_mode) for (const i of group.items) i.in_progress = false
    }
    if (parentChanged) {
      // смена родителя меняет дерево и позиции — проще перечитать список
      await checkerApi.moveGroup(group.id, settingsParentId.value)
      await load()
    }
    settingsGroup.value = null
  } catch (e) {
    const code = (e as { code?: string }).code
    showToast(
      code === 'too_deep'
        ? `Предел вложенности — ${MAX_DEPTH} уровней`
        : code === 'conflict'
          ? 'Нельзя вложить группу в саму себя'
          : 'Не удалось сохранить',
    )
  }
}

async function removeGroup() {
  const group = settingsGroup.value
  if (!group) return
  try {
    const { name } = await checkerApi.deleteGroup(group.id)
    // мягкое удаление: локально убираем поддерево из активного списка
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
    // баннер «Отменить» (группа в корзине)
    undoDelete.value = { id: group.id, name }
    clearTimeout(undoTimer)
    undoTimer = setTimeout(() => (undoDelete.value = null), 8000)
  } catch {
    showToast('Не удалось удалить группу')
  }
}

// --- корзина ---
const undoDelete = ref<{ id: number; name: string } | null>(null)
let undoTimer: ReturnType<typeof setTimeout> | undefined

async function undoLastDelete() {
  const d = undoDelete.value
  if (!d) return
  undoDelete.value = null
  clearTimeout(undoTimer)
  try {
    await checkerApi.restoreGroup(d.id)
    await load()
  } catch {
    showToast('Не удалось восстановить')
  }
}

const trashModal = ref(false)
const trashList = ref<TrashGroup[]>([])
const trashDays = ref(30)
const trashLoading = ref(false)
const confirmEmptyTrash = ref(false)
const confirmPurgeId = ref<number | null>(null)

async function openTrash() {
  trashModal.value = true
  trashLoading.value = true
  confirmEmptyTrash.value = false
  confirmPurgeId.value = null
  try {
    const { trashed, retention_days } = await checkerApi.listTrash()
    trashList.value = trashed
    trashDays.value = retention_days
  } catch {
    showToast('Не удалось открыть корзину')
  } finally {
    trashLoading.value = false
  }
}

async function restoreFromTrash(t: TrashGroup) {
  try {
    await checkerApi.restoreGroup(t.id)
    trashList.value = trashList.value.filter((x) => x.id !== t.id)
    await load()
    showToast(`«${t.name}» восстановлена ↩️`)
  } catch {
    showToast('Не удалось восстановить')
  }
}

async function purgeFromTrash(t: TrashGroup) {
  if (confirmPurgeId.value !== t.id) {
    confirmPurgeId.value = t.id
    setTimeout(() => {
      if (confirmPurgeId.value === t.id) confirmPurgeId.value = null
    }, 3000)
    return
  }
  confirmPurgeId.value = null
  try {
    await checkerApi.purgeTrashGroup(t.id)
    trashList.value = trashList.value.filter((x) => x.id !== t.id)
  } catch {
    showToast('Не удалось удалить')
  }
}

async function doEmptyTrash() {
  if (!confirmEmptyTrash.value) {
    confirmEmptyTrash.value = true
    setTimeout(() => (confirmEmptyTrash.value = false), 3000)
    return
  }
  confirmEmptyTrash.value = false
  try {
    await checkerApi.emptyTrash()
    trashList.value = []
  } catch {
    showToast('Не удалось очистить')
  }
}

async function saveTrashDays() {
  const d = Math.max(1, Math.min(365, Math.round(trashDays.value || 30)))
  trashDays.value = d
  try {
    await checkerApi.setTrashDays(d)
  } catch {
    showToast('Не удалось сохранить срок')
  }
}

function fmtDeletedAt(iso: string): string {
  const days = Math.floor((Date.now() - new Date(iso).getTime()) / 86400000)
  if (days <= 0) return 'сегодня'
  if (days === 1) return 'вчера'
  return `${days} дн. назад`
}
</script>

<template>
  <div v-if="loading" class="loading">Загрузка…</div>

  <template v-else>
    <TemplatesSection ref="templatesSection" @started="load" />

    <div v-if="undoDelete" class="undo-bar">
      <span class="undo-text">«{{ undoDelete.name }}» в корзине</span>
      <button class="undo-btn" @click="undoLastDelete">Отменить</button>
    </div>

    <div v-if="groups.length" class="page-search">
      <input v-model="search" placeholder="🔍 Поиск по группам и пунктам…" />
      <button v-if="search" class="clear-search" title="Очистить" @click="search = ''">✕</button>
    </div>

    <p v-if="groups.length === 0" class="empty">Пока нет ни одного списка — создайте первый 👇</p>
    <p v-else-if="searchFilter && visibleTopGroups.length === 0" class="empty">
      Ничего не найдено по «{{ search.trim() }}»
    </p>

    <div class="group-list">
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
    </div>

    <form class="add-group" @submit.prevent="addGroup">
      <input v-model="newGroupName" placeholder="Новая группа…" maxlength="200" />
      <button type="submit">Добавить группу</button>
    </form>
    <button class="import-btn" @click="openImport">⬆️ Импорт группы (текст / JSON)</button>
    <button class="import-btn" @click="openTrash">🗑 Корзина</button>
  </template>

  <!-- корзина -->
  <div v-if="trashModal" class="modal" @click.self="trashModal = false">
    <div class="modal-content">
      <h3>Корзина</h3>
      <p v-if="trashLoading" class="hint">Загрузка…</p>
      <template v-else>
        <p v-if="!trashList.length" class="hint">Корзина пуста</p>
        <div v-for="t in trashList" :key="t.id" class="trash-row">
          <div class="trash-info">
            <div class="trash-name">{{ t.name }}</div>
            <div class="trash-meta">
              {{ fmtDeletedAt(t.deleted_at) }} · {{ t.groups }} гр. · {{ t.items }} пунктов
            </div>
          </div>
          <button class="icon-btn" title="Восстановить" @click="restoreFromTrash(t)">↩️</button>
          <button
            class="icon-btn del"
            :class="{ confirming: confirmPurgeId === t.id }"
            title="Удалить навсегда"
            @click="purgeFromTrash(t)"
          >
            {{ confirmPurgeId === t.id ? 'точно?' : '✖' }}
          </button>
        </div>

        <label class="retention">
          <span>Хранить в корзине, дней</span>
          <input v-model.number="trashDays" type="number" min="1" max="365" @change="saveTrashDays" />
        </label>

        <button v-if="trashList.length" class="btn danger" @click="doEmptyTrash">
          {{ confirmEmptyTrash ? 'Точно очистить всё?' : '🗑 Очистить корзину' }}
        </button>
      </template>
      <button class="btn" @click="trashModal = false">Закрыть</button>
    </div>
  </div>

  <div v-if="settingsGroup" class="modal" @click.self="settingsGroup = null">
    <div class="modal-content">
      <h3 v-if="settingsGroup.mine">
        {{ settingsGroup.parent_id === null ? 'Настройки группы' : 'Настройки подгруппы' }}
      </h3>
      <h3 v-else>Общий список</h3>

      <!-- владелец: полные настройки -->
      <template v-if="settingsGroup.mine">
        <input v-model="settingsName" type="text" maxlength="200" class="name-input" />
        <label class="parent-field">
          <span>Родитель</span>
          <select v-model="settingsParentId">
            <option v-for="o in parentOptions" :key="o.id ?? 'root'" :value="o.id">{{ o.label }}</option>
          </select>
        </label>
        <label class="hide-done-line">
          <input v-model="settingsHideDone" type="checkbox" />
          <span>Скрывать выполненное</span>
        </label>
        <label class="hide-done-line">
          <input v-model="settingsProgressMode" type="checkbox" />
          <span>🚧 Промежуточный статус «в работе»</span>
        </label>
        <p class="mode-hint">
          Клик по пункту идёт по кругу: сначала «в работе», вторым — сделано,
          третьим снимает. Взятые в работу видно в списке и в полоске прогресса.
        </p>
        <button class="btn primary" @click="saveSettings">💾 Сохранить</button>
        <button class="btn" @click="duplicateGroup">📑 Дублировать</button>

        <div v-if="canAddSub" class="sub-add">
          <input v-model="subName" placeholder="Название подгруппы…" maxlength="200" @keyup.enter="addSubgroup" />
          <button class="btn ghost" :disabled="!subName.trim()" @click="addSubgroup">➕ Подгруппа</button>
        </div>
        <p v-else class="depth-hint">Достигнут предел вложенности — {{ MAX_DEPTH }} уровней</p>
      </template>
      <p v-else class="received-note">
        Список от <b>{{ settingsGroup.owner_name }}</b> — пункты можно менять вместе с владельцем.
      </p>

      <!-- массовые действия по пунктам: и владельцу, и участникам -->
      <div v-if="settingsGroup.items.length" class="bulk-actions">
        <button class="btn" @click="bulkAction('check_all')">✓ Отметить все</button>
        <button class="btn" @click="bulkAction('uncheck_all')">▢ Снять все</button>
        <button class="btn danger" @click="bulkAction('delete_done')">
          {{ confirmBulkDelete ? 'Точно?' : '🗑 Выполненные' }}
        </button>
      </div>

      <button v-if="settingsGroup.parent_id === null" class="btn" @click="openHistory">🕘 История</button>
      <button v-if="settingsGroup.parent_id === null" class="btn" @click="openCalendar">📅 История по дням</button>

      <!-- повторяющийся список (владелец, верхний уровень) -->
      <div v-if="settingsGroup.mine && settingsGroup.parent_id === null" class="recur-block">
        <label class="parent-field">
          <span>🔁 Повторять (авто-сброс отметок)</span>
          <select v-model="recurPeriod">
            <option value="none">Не повторять</option>
            <option value="daily">Ежедневно</option>
            <option value="weekly">Еженедельно</option>
            <option value="monthly">Ежемесячно</option>
          </select>
        </label>
        <div v-if="recurPeriod !== 'none'" class="recur-row">
          <input v-model="recurTime" type="time" />
          <select v-if="recurPeriod === 'weekly'" v-model.number="recurDow">
            <option :value="1">Пн</option>
            <option :value="2">Вт</option>
            <option :value="3">Ср</option>
            <option :value="4">Чт</option>
            <option :value="5">Пт</option>
            <option :value="6">Сб</option>
            <option :value="0">Вс</option>
          </select>
          <input v-if="recurPeriod === 'monthly'" v-model.number="recurDom" type="number" min="1" max="31" />
        </div>
        <button class="btn" :disabled="recurSaving" @click="saveRecurrence">💾 Сохранить расписание</button>

        <label class="parent-field">
          <span>⏰ Напомнить о списке (бот)</span>
          <div class="recur-row">
            <input v-model="groupRemind" type="datetime-local" />
            <button v-if="groupRemind" class="btn ghost" @click="groupRemind = ''">✕</button>
          </div>
        </label>
        <button class="btn" @click="saveGroupReminder">💾 Сохранить напоминание</button>
      </div>

      <!-- сбросить сейчас (владелец или участник) -->
      <button
        v-if="settingsGroup.parent_id === null && settingsGroup.items.length"
        class="btn"
        @click="resetNowAction"
      >
        {{ confirmResetNow ? 'Точно сбросить? (снимок сохранится)' : '🔄 Сбросить сейчас' }}
      </button>

      <template v-if="settingsGroup.mine">
        <template v-if="settingsGroup.parent_id === null">
          <button class="btn" @click="openShareGroup">📤 Поделиться группой</button>
          <button class="btn" @click="openExport">⬇️ Экспорт (текст / JSON)</button>
          <button class="btn" @click="saveAsTemplate">📋 Сохранить как шаблон</button>
        </template>
        <button v-if="!confirmDeleteGroup" class="btn danger" @click="confirmDeleteGroup = true">
          🗑 Удалить {{ settingsGroup.parent_id === null ? 'группу' : 'подгруппу' }}
        </button>
        <button v-else class="btn danger" @click="removeGroup">Точно удалить? Пункты будут потеряны</button>
      </template>
      <button
        v-else-if="settingsGroup.parent_id === null"
        class="btn danger"
        @click="leaveGroup"
      >
        🚪 Покинуть список
      </button>

      <button class="btn" @click="settingsGroup = null">Отмена</button>
    </div>
  </div>

  <!-- история изменений -->
  <div v-if="historyModal" class="modal" @click.self="historyModal = false">
    <div class="modal-content">
      <h3>История</h3>
      <p v-if="historyLoading" class="hint">Загрузка…</p>
      <template v-else>
        <p v-if="!historyList.length" class="hint">Пока пусто</p>
        <div v-for="(e, i) in historyList" :key="i" class="hist-row">
          <div class="hist-line"><b>{{ e.user_name }}</b> {{ e.action }}</div>
          <div class="hist-time">{{ fmtHistoryTime(e.at) }}</div>
        </div>
      </template>
      <button class="btn" @click="historyModal = false">Закрыть</button>
    </div>
  </div>

  <!-- календарь / история по дням -->
  <HistoryCalendar v-if="calendarGroupId" :group-id="calendarGroupId" @close="calendarGroupId = null" />

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

      <div class="collab-block">
        <label class="field-label">👥 Совместный доступ (общий список)</label>
        <div v-if="participants.length" class="participants">
          <span v-for="u in participants" :key="u.id" class="participant">
            {{ participantLabel(u) }}
            <button class="p-x" title="Отозвать доступ" @click="revokeCollab(u)">✕</button>
          </span>
        </div>
        <RecipientPicker v-model="collabTo" />
        <button class="btn" :disabled="collabBusy || !collabTo.trim()" @click="openCollab">
          👥 Открыть доступ
        </button>
        <p class="share-hint">
          Участники видят этот список у себя и меняют пункты вместе с вами (это не копия).
        </p>
      </div>

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

.parent-field {
  display: block;
  text-align: left;
  margin-top: 12px;
}

.parent-field span {
  display: block;
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 4px;
}

.parent-field select {
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

/* баннер отмены удаления */
.undo-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 8px 12px;
  margin-bottom: 12px;
}

.undo-text {
  font-size: 13px;
  min-width: 0;
  overflow-wrap: anywhere;
}

.undo-btn {
  flex: none;
  background: none;
  border: none;
  color: var(--accent-color);
  font-weight: 600;
}

/* корзина */
.hint {
  text-align: center;
  color: var(--text-secondary);
  padding: 10px 0;
}

.trash-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 0;
  text-align: left;
  border-bottom: 1px solid var(--bg-secondary);
}

.trash-info {
  flex: 1;
  min-width: 0;
}

.trash-name {
  overflow-wrap: anywhere;
}

.trash-meta {
  font-size: 11px;
  color: var(--text-secondary);
}

.trash-row .icon-btn {
  flex: none;
  background: none;
  border: none;
  padding: 4px 8px;
  color: var(--text-secondary);
}

.trash-row .icon-btn.del.confirming {
  color: #ef4444;
  font-weight: 600;
}

.retention {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-top: 12px;
  font-size: 13px;
}

.retention input {
  width: 80px;
}

.sub-add {
  display: flex;
  gap: 6px;
  margin-top: 10px;
}

.mode-hint {
  margin: 4px 0 0;
  font-size: 11px;
  color: var(--text-secondary);
  text-align: left;
}

.depth-hint {
  margin-top: 10px;
  font-size: 12px;
  color: var(--text-secondary);
}

.received-note {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 4px 0 0;
}

.recur-block {
  margin-top: 12px;
  padding-top: 10px;
  border-top: 1px solid var(--bg-secondary);
}

.recur-row {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}

.recur-row input,
.recur-row select {
  flex: 1;
  min-width: 0;
}

.hist-row {
  text-align: left;
  padding: 6px 0;
  border-bottom: 1px solid var(--bg-secondary);
}

.hist-line {
  font-size: 13px;
  overflow-wrap: anywhere;
}

.hist-time {
  font-size: 11px;
  color: var(--text-secondary);
}

.collab-block {
  margin-top: 14px;
  padding-top: 10px;
  border-top: 1px solid var(--bg-secondary);
}

.participants {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin: 6px 0;
}

.participant {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: var(--bg-secondary);
  border-radius: 12px;
  padding: 3px 6px 3px 10px;
  font-size: 12px;
}

.p-x {
  background: none;
  border: none;
  color: var(--text-secondary);
  padding: 0 2px;
}

.bulk-actions {
  display: flex;
  gap: 6px;
  margin-top: 10px;
}

.bulk-actions .btn {
  flex: 1;
  margin-top: 0;
  padding: 8px 4px;
  font-size: 12px;
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
