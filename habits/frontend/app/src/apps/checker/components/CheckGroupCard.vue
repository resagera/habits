<script setup lang="ts">
import { computed, ref } from 'vue'
import { showToast } from '../../../shared/toast'
import * as checkerApi from '../api'
import { groupRelevant, highlightParts, normQuery, visibleItems } from '../search'
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

// прямые подгруппы (порядок сохраняется из ответа сервера — по position)
const children = computed(() => props.groups.filter((g) => g.parent_id === props.group.id))

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
const visibleChildren = computed(() =>
  children.value.filter((c) => groupRelevant(c, props.groups, activeFilter.value)),
)

const newItemName = ref('')
const newItemEl = ref<HTMLTextAreaElement | null>(null)
const confirmDeleteItemId = ref<number | null>(null)

// поле нового пункта: однострочное, но растёт вниз, когда текст не влезает
function growNewItem() {
  const el = newItemEl.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = el.scrollHeight + 'px'
}

// долгое нажатие по пункту → окно редактирования
const pressTimer = ref<ReturnType<typeof setTimeout> | null>(null)
const longPressFired = ref(false)
const editingItem = ref<CheckItem | null>(null)
const editName = ref('')
const editGroupId = ref<number>(0)

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

function startPress(item: CheckItem) {
  cancelPress()
  longPressFired.value = false
  pressTimer.value = setTimeout(() => {
    longPressFired.value = true
    editingItem.value = item
    editName.value = item.name
    editGroupId.value = props.group.id
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
  const nameChanged = !!name && name !== item.name
  const groupChanged = editGroupId.value !== props.group.id
  if (!nameChanged && !groupChanged) {
    editingItem.value = null
    return
  }
  const patch: { name?: string; group_id?: number } = {}
  if (nameChanged) patch.name = name
  if (groupChanged) patch.group_id = editGroupId.value
  try {
    const { item: updated } = await checkerApi.updateItem(item.id, patch)
    if (nameChanged) item.name = updated.name
    if (groupChanged) {
      // переносим объект пункта в целевую группу локально (списки реактивны)
      const target = props.groups.find((g) => g.id === editGroupId.value)
      props.group.items = props.group.items.filter((i) => i.id !== item.id)
      if (target) {
        item.position = updated.position
        target.items.push(item)
      }
    }
    editingItem.value = null
  } catch {
    showToast('Не удалось сохранить')
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
  item.done = !item.done // оптимистично
  try {
    const { item: updated } = await checkerApi.updateItem(itemId, { done: item.done })
    item.done = updated.done
  } catch {
    item.done = !item.done
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
  <div class="check-group" :class="{ sub: isSub }">
    <div class="group-header">
      <button class="group-name collapse-toggle" @click="emit('toggleCollapse', group.id)">
        <span class="chevron" :class="{ open: expanded }">▸</span>
        <span class="gname-text">
          <template v-if="activeFilter">
            <span v-for="(p, i) in highlightParts(group.name, activeFilter)" :key="i" :class="{ hit: p.hit }">{{ p.text }}</span>
          </template>
          <template v-else>{{ group.name }}</template>
        </span>
        <span v-if="group.items.length" class="done-count">
          {{ doneCount }}/{{ group.items.length }}
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

    <input
      v-if="searchOpen && !inheritedFilter"
      v-model="localSearch"
      class="group-search"
      placeholder="Поиск пунктов и подгрупп…"
    />

    <div v-for="item in shownItems" :key="item.id" class="check-item">
      <label
        class="check-label"
        :class="{ done: item.done }"
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
        <span class="check-text">
          <template v-if="activeFilter">
            <span v-for="(p, i) in highlightParts(item.name, activeFilter)" :key="i" :class="{ hit: p.hit }">{{ p.text }}</span>
          </template>
          <template v-else>{{ item.name }}</template>
        </span>
      </label>
      <button
        class="icon-btn delete-btn"
        :class="{ confirming: confirmDeleteItemId === item.id }"
        @click="removeItem(item.id)"
      >
        {{ confirmDeleteItemId === item.id ? 'точно?' : '✖' }}
      </button>
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
        <input
          v-model="editName"
          class="item-edit-input"
          maxlength="500"
          @keyup.enter="saveItemEdit"
        />
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
