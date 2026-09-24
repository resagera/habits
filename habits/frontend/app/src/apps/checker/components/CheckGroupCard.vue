<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { autoGrow } from '../../../shared/autoGrow'
import { showToast } from '../../../shared/toast'
import * as checkerApi from '../api'
import { fmtRemind, isoToLocalInput, localInputToIso } from '../datetime'
import { groupRelevant, highlightParts, normQuery, visibleItems } from '../search'
import { useGroupReorder } from '../useGroupReorder'
import type { CheckGroup, CheckItem } from './../types'

const props = defineProps<{
  group: CheckGroup
  groups: CheckGroup[]
  collapsedSet: Set<number>
  // унаследованный фильтр (глобальный поиск или поиск родительской группы)
  filter?: string
  depth?: number
}>()
const emit = defineEmits<{
  openSettings: [group: CheckGroup]
  toggleCollapse: [id: number]
}>()

const collapsed = computed(() => props.collapsedSet.has(props.group.id))
const isSub = computed(() => props.group.parent_id !== null)
const depth = computed(() => props.depth ?? 0)

// прямые подгруппы, отсортированные по position (нужно для живой перестановки DnD)
const children = computed(() =>
  props.groups
    .filter((g) => g.parent_id === props.group.id)
    .sort((a, b) => a.position - b.position || a.id - b.id),
)

// перетаскивание группы за заголовок среди соседей
const rootEl = ref<HTMLElement | null>(null)
const { dragging: isDragging, onPointerDown: startDrag, consumeClick } = useGroupReorder(
  () => props.group,
  () => props.groups,
  () => rootEl.value,
)
function onHeaderPointerDown(e: PointerEvent) {
  // перестановка — только у своих групп и вне поиска (у чужих порядок меняет владелец)
  if (!activeFilter.value && props.group.mine) startDrag(e)
}
function onHeaderClick() {
  if (consumeClick()) return // был drag — не переключаем сворачивание
  emit('toggleCollapse', props.group.id)
}

// --- поиск ---
// локальный поиск по этой группе (и её подгруппам); показываем, только если
// сверху фильтр не задан. Активный фильтр = унаследованный ИЛИ локальный.
const localSearch = ref('')
const searchOpen = ref(false)
const inheritedFilter = computed(() => normQuery(props.filter ?? ''))
const activeFilter = computed(() => inheritedFilter.value || normQuery(localSearch.value))
const showSearchToggle = computed(() => !inheritedFilter.value && (depth.value === 0 || children.value.length > 0))

// при активном фильтре карточка всегда раскрыта (иначе — по свёрнутости)
const expanded = computed(() => !collapsed.value || !!activeFilter.value)
// показываемые пункты: фильтр поиска, затем «скрывать выполненное»
const shownItems = computed(() => {
  if (!expanded.value) return []
  let items = visibleItems(props.group, activeFilter.value)
  if (props.group.hide_done) items = items.filter((i) => !i.done)
  return items
})
const doneCount = computed(() => props.group.items.filter((i) => i.done).length)
const totalCount = computed(() => props.group.items.length)
const progressPct = computed(() =>
  totalCount.value ? Math.round((doneCount.value / totalCount.value) * 100) : 0,
)
// «в работе» видно и в полоске: приглушённый сегмент сразу за сделанным —
// иначе группа, где всё начато и ничего не закончено, выглядит нетронутой
const workCount = computed(() => props.group.items.filter((i) => i.in_progress && !i.done).length)
const workPct = computed(() =>
  totalCount.value ? Math.round((workCount.value / totalCount.value) * 100) : 0,
)

// агрегат по всему поддереву (эта группа + все вложенные) — «Σ done/total»
const subtreeStats = computed(() => {
  let done = 0
  let total = 0
  const walk = (g: CheckGroup) => {
    for (const it of g.items) {
      total++
      if (it.done) done++
    }
    for (const c of props.groups.filter((x) => x.parent_id === g.id)) walk(c)
  }
  walk(props.group)
  return { done, total }
})
const hasSubtreeExtra = computed(() => subtreeStats.value.total > totalCount.value)

const visibleChildren = computed(() =>
  children.value.filter((c) => groupRelevant(c, props.groups, activeFilter.value)),
)

const newItemName = ref('')
const newItemEl = ref<HTMLTextAreaElement | null>(null)
const confirmDeleteItemId = ref<number | null>(null)

// поле нового пункта: однострочное, но растёт вниз, когда текст не влезает
function growNewItem() {
  autoGrow(newItemEl.value)
}

// долгое нажатие по пункту → окно редактирования
const pressTimer = ref<ReturnType<typeof setTimeout> | null>(null)
const longPressFired = ref(false)
const editingItem = ref<CheckItem | null>(null)
const editName = ref('')
const editNote = ref('')
const editLabel = ref('')
const editRemind = ref('') // datetime-local
const editGroupId = ref<number>(0)
// поля названия и заметки в окне правки растут под текст: в однострочном
// input длинный пункт было видно только кусочком, а он бывает на пару строк
const editNameEl = ref<HTMLTextAreaElement | null>(null)
const editNoteEl = ref<HTMLTextAreaElement | null>(null)

// пресеты меток (приоритет/цвет/эмодзи) — тап переключает
const LABEL_PRESETS = ['🔴', '🟠', '🟡', '🟢', '🔵', '⭐', '🔥', '❗', '📌']
function pickLabel(l: string) {
  editLabel.value = editLabel.value === l ? '' : l
}

// все группы полным путём «Родитель › Подгруппа» — для переноса пункта; подгруппы
// идут сразу под своим родителем (DFS) и подписаны вместе с ним
const groupOptions = computed(() => {
  const out: { id: number; label: string }[] = []
  const walk = (parentId: number | null, prefix: string) => {
    for (const g of props.groups.filter((x) => x.parent_id === parentId)) {
      const label = prefix ? prefix + ' › ' + g.name : g.name
      out.push({ id: g.id, label })
      walk(g.id, label)
    }
  }
  walk(null, '')
  return out
})

// открыть окно редактирования пункта (из ✏️ или долгого нажатия)
function openEdit(item: CheckItem) {
  editingItem.value = item
  editName.value = item.name
  editNote.value = item.note
  editLabel.value = item.label
  editRemind.value = item.remind_at ? isoToLocalInput(item.remind_at) : ''
  editGroupId.value = props.group.id
  // высоту считаем после отрисовки модалки — до неё элементов ещё нет
  void nextTick(() => {
    autoGrow(editNameEl.value)
    autoGrow(editNoteEl.value)
  })
}

function startPress(item: CheckItem) {
  cancelPress()
  longPressFired.value = false
  pressTimer.value = setTimeout(() => {
    longPressFired.value = true
    openEdit(item)
  }, 500)
}

function cancelPress() {
  if (pressTimer.value) {
    clearTimeout(pressTimer.value)
    pressTimer.value = null
  }
}

async function saveItemEdit() {
  const item = editingItem.value
  if (!item) return
  const name = editName.value.trim()
  const note = editNote.value
  const label = editLabel.value.trim()
  const nameChanged = !!name && name !== item.name
  const noteChanged = note !== item.note
  const labelChanged = label !== item.label
  const groupChanged = editGroupId.value !== props.group.id
  const curRemind = item.remind_at ? isoToLocalInput(item.remind_at) : ''
  const remindChanged = editRemind.value !== curRemind
  if (!nameChanged && !noteChanged && !labelChanged && !groupChanged && !remindChanged) {
    editingItem.value = null
    return
  }
  const patch: { name?: string; note?: string; label?: string; group_id?: number } = {}
  if (nameChanged) patch.name = name
  if (noteChanged) patch.note = note
  if (labelChanged) patch.label = label
  if (groupChanged) patch.group_id = editGroupId.value
  try {
    if (remindChanged) {
      const iso = editRemind.value ? localInputToIso(editRemind.value) : null
      await checkerApi.setItemReminder(item.id, iso)
      item.remind_at = iso
    }
    if (nameChanged || noteChanged || labelChanged || groupChanged) {
      const { item: updated } = await checkerApi.updateItem(item.id, patch)
      if (nameChanged) item.name = updated.name
      if (noteChanged) item.note = updated.note
      if (labelChanged) item.label = updated.label
      if (groupChanged) applyMove(item, updated.position)
    }
    editingItem.value = null
  } catch {
    showToast('Не удалось сохранить')
  }
}

// перенос объекта пункта в целевую группу локально (списки реактивны)
function applyMove(item: CheckItem, newPosition: number) {
  const target = props.groups.find((g) => g.id === editGroupId.value)
  props.group.items = props.group.items.filter((i) => i.id !== item.id)
  if (target) {
    item.position = newPosition
    target.items.push(item)
  }
}

async function deleteItemFromEdit() {
  const item = editingItem.value
  if (!item) return
  try {
    await checkerApi.deleteItem(item.id)
    props.group.items = props.group.items.filter((i) => i.id !== item.id)
    editingItem.value = null
  } catch {
    showToast('Не удалось удалить')
  }
}

async function addItem() {
  const name = newItemName.value.trim()
  if (!name) return
  try {
    const { item } = await checkerApi.createItem(props.group.id, name)
    props.group.items.push(item)
    newItemName.value = ''
    if (newItemEl.value) newItemEl.value.style.height = 'auto'
  } catch {
    showToast('Не удалось добавить пункт')
  }
}

async function toggleItem(itemId: number) {
  // это было долгое нажатие (открытие редактора), а не тап — не переключаем
  if (longPressFired.value) {
    longPressFired.value = false
    return
  }
  const item = props.group.items.find((i) => i.id === itemId)
  if (!item) return
  const wasDone = item.done
  const wasWork = item.in_progress
  // При включённом у группы промежуточном статусе клик идёт по кругу:
  // пусто → в работе → сделано → пусто. Полная отметка — два клика.
  // Без режима — как было, одним кликом.
  let patch: { done?: boolean; in_progress?: boolean }
  if (props.group.progress_mode) {
    if (!item.done && !item.in_progress) patch = { in_progress: true }
    else if (item.in_progress) patch = { done: true }
    else patch = { done: false }
  } else {
    patch = { done: !item.done }
  }
  // оптимистично; «сделано» и «в работе» вместе не живут
  item.done = patch.done ?? item.done
  item.in_progress = patch.in_progress ?? (item.done ? false : item.in_progress)
  try {
    const { item: updated } = await checkerApi.updateItem(itemId, patch)
    item.done = updated.done
    item.in_progress = updated.in_progress
  } catch {
    item.done = wasDone
    item.in_progress = wasWork
    showToast('Не удалось сохранить')
  }
}

async function removeItem(itemId: number) {
  if (confirmDeleteItemId.value !== itemId) {
    confirmDeleteItemId.value = itemId
    setTimeout(() => {
      if (confirmDeleteItemId.value === itemId) confirmDeleteItemId.value = null
    }, 3000)
    return
  }
  confirmDeleteItemId.value = null
  try {
    await checkerApi.deleteItem(itemId)
    props.group.items = props.group.items.filter((i) => i.id !== itemId)
  } catch {
    showToast('Не удалось удалить')
  }
}
</script>

<template>
  <div ref="rootEl" class="check-group" :class="{ sub: isSub, dragging: isDragging }">
    <div class="group-header">
      <button
        class="group-name collapse-toggle"
        @pointerdown="onHeaderPointerDown"
        @click="onHeaderClick"
      >
        <span class="chevron" :class="{ open: expanded }">▸</span>
        <span class="gname-text">
          <template v-if="activeFilter">
            <span v-for="(p, i) in highlightParts(group.name, activeFilter)" :key="i" :class="{ hit: p.hit }">{{ p.text }}</span>
          </template>
          <template v-else>{{ group.name }}</template>
        </span>
        <span
          v-if="group.shared"
          class="share-badge"
          :title="group.mine ? 'Совместный доступ' : 'Общий список — владелец ' + group.owner_name"
        >👥<span v-if="!group.mine && group.owner_name" class="owner">{{ group.owner_name }}</span></span>
        <span v-if="group.reset_period && group.reset_period !== 'none'" class="share-badge" title="Повторяющийся список">🔁</span>
        <span v-if="totalCount" class="done-count">{{ doneCount }}/{{ totalCount }}</span>
        <span v-if="hasSubtreeExtra" class="subtree-count" title="Всего с подгруппами">
          Σ {{ subtreeStats.done }}/{{ subtreeStats.total }}
        </span>
      </button>
      <button
        v-if="showSearchToggle"
        class="icon-btn"
        :class="{ active: searchOpen }"
        title="Поиск в группе"
        @click="searchOpen = !searchOpen"
      >
        🔍
      </button>
      <button class="icon-btn" title="Настройки группы" @click="emit('openSettings', group)">⚙️</button>
    </div>

    <div
      v-if="totalCount"
      class="progress"
      :title="workCount ? `${progressPct}% готово, ${workCount} в работе` : progressPct + '%'"
    >
      <div class="progress-fill" :style="{ width: progressPct + '%' }"></div>
      <div class="progress-work" :style="{ width: workPct + '%' }"></div>
    </div>

    <input
      v-if="searchOpen && !inheritedFilter"
      v-model="localSearch"
      class="group-search"
      placeholder="Поиск пунктов и подгрупп…"
    />

    <div v-for="item in shownItems" :key="item.id" class="check-item">
      <label
        class="check-label"
        :class="{ done: item.done, work: item.in_progress && !item.done }"
        @touchstart.passive="startPress(item)"
        @touchend="cancelPress"
        @touchmove.passive="cancelPress"
        @touchcancel="cancelPress"
        @mousedown="startPress(item)"
        @mouseup="cancelPress"
        @mouseleave="cancelPress"
        @contextmenu.prevent
      >
        <input
          type="checkbox"
          :checked="item.done"
          class="check-input"
          @change="toggleItem(item.id)"
        />
        <span class="check-box">
          <svg viewBox="0 0 12 9" width="12" height="9">
            <polyline points="1 5 4 8 11 1"></polyline>
          </svg>
        </span>
        <span class="check-body">
          <span class="check-text">
            <span v-if="item.label" class="check-label-mark">{{ item.label }}</span>
            <template v-if="activeFilter">
              <span v-for="(p, i) in highlightParts(item.name, activeFilter)" :key="i" :class="{ hit: p.hit }">{{ p.text }}</span>
            </template>
            <template v-else>{{ item.name }}</template>
          </span>
          <span v-if="item.note" class="check-note">{{ item.note }}</span>
          <span v-if="item.remind_at" class="check-remind">⏰ {{ fmtRemind(item.remind_at) }}</span>
        </span>
      </label>
      <span class="item-actions">
        <button class="icon-btn" title="Изменить пункт" @click="openEdit(item)">✏️</button>
        <button
          class="icon-btn delete-btn"
          :class="{ confirming: confirmDeleteItemId === item.id }"
          @click="removeItem(item.id)"
        >
          {{ confirmDeleteItemId === item.id ? 'точно?' : '✖' }}
        </button>
      </span>
    </div>

    <form v-if="expanded && !activeFilter" class="add-item" @submit.prevent="addItem">
      <textarea
        ref="newItemEl"
        v-model="newItemName"
        placeholder="Новый пункт…"
        maxlength="500"
        rows="1"
        @input="growNewItem"
        @keydown.enter.prevent="addItem"
      ></textarea>
      <button type="submit">➕</button>
    </form>

    <!-- подгруппы (произвольная вложенность) -->
    <div v-if="expanded && visibleChildren.length" class="subgroups">
      <CheckGroupCard
        v-for="child in visibleChildren"
        :key="child.id"
        :group="child"
        :groups="groups"
        :collapsed-set="collapsedSet"
        :filter="activeFilter"
        :depth="depth + 1"
        @toggle-collapse="emit('toggleCollapse', $event)"
        @open-settings="emit('openSettings', $event)"
      />
    </div>
  </div>

  <!-- редактирование пункта (долгое нажатие) -->
  <Teleport to="body">
    <div v-if="editingItem" class="modal" @click.self="editingItem = null">
      <div class="modal-content">
        <h3>Пункт</h3>
        <!--
          Название — textarea, а не input: пункты бывают на две-три строки, и в
          однострочном поле правился видимый кусок вслепую. Enter здесь ставит
          перенос строки, сохранение — кнопкой или Ctrl/⌘+Enter.
        -->
        <textarea
          ref="editNameEl"
          v-model="editName"
          class="item-edit-input"
          maxlength="500"
          rows="1"
          @input="autoGrow(editNameEl)"
          @keydown.ctrl.enter.prevent="saveItemEdit"
          @keydown.meta.enter.prevent="saveItemEdit"
        ></textarea>
        <label class="ci-field">
          <span>Метка</span>
          <div class="label-picker">
            <button
              v-for="l in LABEL_PRESETS"
              :key="l"
              type="button"
              class="label-chip"
              :class="{ on: editLabel === l }"
              @click="pickLabel(l)"
            >
              {{ l }}
            </button>
            <input v-model="editLabel" class="label-input" maxlength="16" placeholder="или своё" />
          </div>
        </label>
        <label class="ci-field">
          <span>Заметка</span>
          <textarea
            ref="editNoteEl"
            v-model="editNote"
            class="ci-textarea"
            rows="3"
            maxlength="4000"
            placeholder="Описание, детали…"
            @input="autoGrow(editNoteEl)"
          ></textarea>
        </label>
        <label class="ci-field">
          <span>⏰ Напомнить (бот пришлёт в срок)</span>
          <div class="remind-row">
            <input v-model="editRemind" type="datetime-local" class="ci-select" />
            <button v-if="editRemind" type="button" class="remind-clear" @click="editRemind = ''">✕</button>
          </div>
        </label>
        <label class="ci-field">
          <span>Группа</span>
          <select v-model.number="editGroupId" class="ci-select">
            <option v-for="o in groupOptions" :key="o.id" :value="o.id">{{ o.label }}</option>
          </select>
        </label>
        <button class="ci-btn primary" @click="saveItemEdit">💾 Сохранить</button>
        <button class="ci-btn danger" @click="deleteItemFromEdit">🗑 Удалить</button>
        <button class="ci-btn" @click="editingItem = null">Отмена</button>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.check-group {
  background: var(--card-color);
  border-radius: 8px;
  padding: 10px 12px;
  margin-bottom: 16px;
}

/* перетаскивание группы */
.check-group.dragging {
  opacity: 0.65;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.35);
}

/* заголовок — «ручка» перетаскивания: none, чтобы вертикальный drag не
   перехватывался прокруткой страницы (тап/сворачивание при этом работают) */
.collapse-toggle {
  cursor: grab;
  touch-action: none;
}

/* подгруппа: вложенная карточка без собственной подложки — только левая
   линия-акцент и отступ (фон родителя-карточки просвечивает, в т.ч. «стекло») */
.check-group.sub {
  margin: 10px 0 4px;
  padding: 4px 0 4px 10px;
  border-left: 2px solid var(--accent-color);
  border-radius: 0;
  background: none;
}

.subgroups {
  margin-top: 6px;
}

.group-search {
  width: 100%;
  margin: 4px 0 8px;
  padding: 6px 8px;
  font: inherit;
  background: var(--bg-secondary);
  color: var(--text-color);
  border: 1px solid var(--hover-bg-color);
  border-radius: 6px;
}

.icon-btn.active {
  color: var(--accent-color);
}

.gname-text {
  overflow-wrap: anywhere;
}

/* подсветка совпадений при поиске */
.hit {
  background: var(--accent-color);
  color: #fff;
  border-radius: 3px;
  padding: 0 1px;
}

.check-group.sub .group-name {
  font-size: 15px;
}

.group-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 6px;
}

.group-name {
  font-weight: 700;
  font-size: larger;
}

.collapse-toggle {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 6px;
  background: none;
  border: none;
  color: var(--text-color);
  text-align: left;
  padding: 0;
}

.chevron {
  display: inline-block;
  transition: transform 0.15s;
  color: var(--text-secondary);
  font-size: 13px;
}

.chevron.open {
  transform: rotate(90deg);
}

.done-count {
  font-size: 12px;
  font-weight: 400;
  color: var(--text-secondary);
}

.share-badge {
  font-size: 12px;
  font-weight: 400;
  color: var(--text-secondary);
  display: inline-flex;
  align-items: center;
  gap: 3px;
}

.share-badge .owner {
  font-size: 11px;
}

.subtree-count {
  font-size: 11px;
  font-weight: 400;
  color: var(--text-secondary);
  opacity: 0.75;
}

.progress {
  display: flex;
  height: 4px;
  border-radius: 3px;
  background: var(--bg-secondary);
  overflow: hidden;
  margin: 0 0 8px;
}

.progress-fill {
  height: 100%;
  background: var(--accent-color);
  border-radius: 3px;
  transition: width 0.2s;
}

/* «в работе» — тем же цветом, но приглушённо: это ещё не готово */
.progress-work {
  height: 100%;
  background: var(--accent-color);
  opacity: 0.35;
  border-radius: 3px;
  transition: width 0.2s;
}

.icon-btn {
  background: none;
  border: none;
  padding: 4px 8px;
  color: var(--text-secondary);
}

.check-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 0;
}

.check-label {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  flex: 1;
  min-width: 0;
}

.check-body {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: 2px;
}

.check-note {
  font-size: 12px;
  color: var(--text-secondary);
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.check-remind {
  font-size: 11px;
  color: var(--accent-color);
}

.remind-row {
  display: flex;
  gap: 6px;
  align-items: center;
}

.remind-clear {
  flex: none;
  background: var(--bg-secondary);
  border: none;
  border-radius: 6px;
  padding: 6px 10px;
  color: var(--text-secondary);
}

.check-label-mark {
  margin-right: 5px;
}

.label-picker {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}

.label-chip {
  background: var(--bg-secondary);
  border: 1px solid transparent;
  border-radius: 8px;
  padding: 4px 8px;
  font-size: 16px;
  line-height: 1;
}

.label-chip.on {
  border-color: var(--accent-color);
}

.label-input {
  width: 90px;
  flex: none;
}

.item-actions {
  display: flex;
  flex: none;
  align-items: center;
}

.check-input {
  display: none;
}

.check-box {
  flex: none;
  width: 20px;
  height: 20px;
  border: 2px solid var(--text-secondary);
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s;
}

.check-box svg {
  stroke: #fff;
  stroke-width: 2;
  fill: none;
  stroke-linecap: round;
  stroke-linejoin: round;
  opacity: 0;
}

/*
  Пункт в работе: рамка акцентом и наполовину закрашенный квадрат — форма
  отличается от галочки, так что состояние читается и без цвета (важно для
  тем с приглушённым акцентом и для дальтоников).
*/
.check-label.work .check-box {
  border-color: var(--accent-color);
  position: relative;
}

.check-label.work .check-box::after {
  content: '';
  position: absolute;
  inset: 0;
  /* ровно половина квадрата, залитая в полную силу: приглушённая заливка на
     20 пикселях читалась почти как сплошная — «сделано» и «в работе» путались */
  background: linear-gradient(135deg, var(--accent-color) 0 50%, transparent 50% 100%);
}

.check-label.work .check-text {
  color: var(--accent-color);
}

.check-label.done .check-box {
  background: var(--accent-color);
  border-color: var(--accent-color);
}

.check-label.done .check-box svg {
  opacity: 1;
}

.check-text {
  overflow-wrap: anywhere;
}

.check-label.done .check-text {
  color: var(--text-secondary);
  text-decoration: line-through;
}

.delete-btn.confirming {
  color: #ef4444;
  font-weight: 600;
}

.add-item {
  display: flex;
  gap: 6px;
  margin-top: 8px;
}

.add-item textarea {
  flex: 1;
  min-width: 0;
  font: inherit;
  background: var(--bg-secondary);
  color: var(--text-color);
  border: 1px solid var(--hover-bg-color);
  border-radius: 6px;
  padding: 7px;
  resize: none;
  overflow: hidden;
  line-height: 1.35;
}

.add-item button {
  background: var(--bg-secondary);
  border: none;
  border-radius: 6px;
  padding: 0 12px;
}

.check-label {
  -webkit-user-select: none;
  user-select: none;
  -webkit-touch-callout: none;
}

.item-edit-input {
  width: 100%;
  font: inherit;
  line-height: 1.35;
  /* растёт скриптом под текст, поэтому ни ручного ресайза, ни своей прокрутки */
  resize: none;
  overflow: hidden;
}

.ci-field {
  display: block;
  text-align: left;
  margin-top: 10px;
}

.ci-field span {
  display: block;
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 4px;
}

.ci-select {
  width: 100%;
}

.ci-textarea {
  width: 100%;
  resize: none;
  overflow: hidden;
  font: inherit;
  line-height: 1.35;
}

.ci-btn {
  display: block;
  width: 100%;
  margin-top: 10px;
  padding: 10px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
}

.ci-btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.ci-btn.danger {
  background: #b91c1c;
  color: #fff;
}

/* карточки-«стекло»: размытие фона под .group (класс неоднозначный —
   правило scoped, чтобы не задеть одноимённые не-карточки) */
:root[data-card-glass] .group {
  backdrop-filter: blur(var(--card-blur, 0px));
  -webkit-backdrop-filter: blur(var(--card-blur, 0px));
}
</style>
