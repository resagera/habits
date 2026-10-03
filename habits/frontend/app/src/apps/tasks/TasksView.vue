<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '../../shared/api/client'
import { loadCollapsed, saveCollapsed } from '../../shared/collapsed'
import { useTasks } from './composables/useTasks'
import { fmtDateGroup } from './types'
import type { Task, TaskCategory } from './types'
import TaskRow from './components/TaskRow.vue'
import TaskCardModal from './components/TaskCardModal.vue'
import CategorySettingsModal from './components/CategorySettingsModal.vue'

const tasks = useTasks()

type Tab = 'today' | 'planned' | 'all' | 'done'
const tab = ref<Tab>('all')
const TABS: { key: Tab; label: string }[] = [
  { key: 'today', label: 'Сегодня' },
  { key: 'planned', label: 'Планы' },
  { key: 'all', label: 'Все' },
  { key: 'done', label: 'Выполненные' },
]

const quickTitle = ref('')
const quickCategory = ref<number | null>(null)
const newCategoryName = ref('')
const addingCategory = ref(false)

const openTaskId = ref<number | null>(null)
const settingsCategory = ref<TaskCategory | null>(null)
const myId = ref(0)

// свёрнутые категории (вкладка «Все»); 0 = «Входящие»
const collapsed = ref(new Set<number>())

function toggleCollapse(id: number) {
  if (collapsed.value.has(id)) collapsed.value.delete(id)
  else collapsed.value.add(id)
  collapsed.value = new Set(collapsed.value)
  saveCollapsed('tasks', collapsed.value)
}

onMounted(async () => {
  loadCollapsed('tasks').then((s) => (collapsed.value = s))
  api.get<{ id: number }>('/me').then((me) => (myId.value = me.id)).catch(() => {})
  await tasks.load()
})

watch(tab, (t) => {
  if (t === 'done' && !tasks.doneLoaded.value) tasks.loadDone()
})

async function onQuickAdd() {
  const title = quickTitle.value.trim()
  if (!title) return
  if (await tasks.quickAdd(title, quickCategory.value)) quickTitle.value = ''
}

async function onAddCategory() {
  const name = newCategoryName.value.trim()
  if (!name) return
  if (await tasks.addCategory(name)) {
    newCategoryName.value = ''
    addingCategory.value = false
  }
}

// «Все»: Входящие + категории по порядку
const allGroups = computed(() => {
  const groups: { id: number; name: string; color: string; category: TaskCategory | null; tasks: Task[] }[] = []
  const inbox = tasks.tasks.value.filter((t) => t.project_id === null || !tasks.categoryOf(t))
  groups.push({ id: 0, name: '📥 Входящие', color: 'transparent', category: null, tasks: inbox })
  for (const p of tasks.categories.value) {
    groups.push({
      id: p.id,
      name: p.name,
      color: p.color,
      category: p,
      tasks: tasks.tasks.value.filter((t) => t.project_id === p.id),
    })
  }
  return groups
})

function categoryName(t: Task): string {
  return tasks.categoryOf(t)?.name ?? ''
}

// «Выполненные»: группировка по дню завершения
const doneGroups = computed(() => {
  const groups = new Map<string, Task[]>()
  for (const t of tasks.doneTasks.value) {
    const day = t.completed_at ? t.completed_at.slice(0, 10) : '—'
    if (!groups.has(day)) groups.set(day, [])
    groups.get(day)!.push(t)
  }
  return [...groups.entries()].map(([day, list]) => ({
    day,
    label: day === '—' ? 'Без даты' : new Date(day + 'T00:00:00').toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' }),
    tasks: list,
  }))
})

async function onSaveCategory(updated: TaskCategory) {
  tasks.replaceCategory(updated)
  settingsCategory.value = null
}

async function onRemoveCategory() {
  const p = settingsCategory.value
  if (p && (await tasks.removeCategory(p.id))) settingsCategory.value = null
}

async function onLeaveCategory() {
  const p = settingsCategory.value
  if (p && myId.value && (await tasks.leaveCategory(p.id, myId.value))) settingsCategory.value = null
}

/** Вернуть выполненную в работу: первый open-статус её категории. */
async function reopen(t: Task) {
  const p = tasks.categoryOf(t)
  const open = (p?.statuses?.length ? p.statuses : [{ name: 'Открыта', kind: 'open' as const }])
    .find((s) => s.kind === 'open')
  await tasks.patch(t.id, { status: open?.name ?? 'Открыта' })
}
</script>

<template>
  <div v-if="tasks.loading.value" class="loading">Загрузка…</div>

  <template v-else>
    <form class="quick-add" @submit.prevent="onQuickAdd">
      <input v-model="quickTitle" placeholder="Новая задача…" maxlength="300" />
      <select v-model="quickCategory" title="Куда добавить">
        <option :value="null">📥</option>
        <option v-for="p in tasks.categories.value" :key="p.id" :value="p.id">{{ p.name }}</option>
      </select>
      <button type="submit" :disabled="!quickTitle.trim()">＋</button>
    </form>

    <div class="tabs">
      <button
        v-for="t in TABS"
        :key="t.key"
        class="tab"
        :class="{ on: tab === t.key }"
        @click="tab = t.key"
      >
        {{ t.label }}
      </button>
    </div>

    <!-- Сегодня -->
    <template v-if="tab === 'today'">
      <p v-if="tasks.todayTasks.value.length === 0" class="empty">
        На сегодня задач нет 🎉
      </p>
      <TaskRow
        v-for="t in tasks.todayTasks.value"
        :key="t.id"
        :task="t"
        :category-name="categoryName(t)"
        @complete="tasks.complete(t)"
        @open="openTaskId = t.id"
      />
    </template>

    <!-- Планы -->
    <template v-else-if="tab === 'planned'">
      <p v-if="tasks.plannedGroups.value.length === 0" class="empty">
        Нет задач со сроком — задайте срок в карточке задачи
      </p>
      <div v-for="g in tasks.plannedGroups.value" :key="g.key" class="date-group">
        <div class="date-label" :class="{ overdue: g.key === '0-overdue' }">
          {{ g.key === '0-overdue' ? 'Просрочено' : fmtDateGroup(g.date) }}
        </div>
        <TaskRow
          v-for="t in g.tasks"
          :key="t.id"
          :task="t"
          :category-name="categoryName(t)"
          @complete="tasks.complete(t)"
          @open="openTaskId = t.id"
        />
      </div>
    </template>

    <!-- Все -->
    <template v-else-if="tab === 'all'">
      <div v-for="g in allGroups" :key="g.id" class="category-group">
        <div class="group-header">
          <button class="collapse-btn" @click="toggleCollapse(g.id)">
            <span class="chevron" :class="{ open: !collapsed.has(g.id) }">▸</span>
            <span v-if="g.category" class="dot" :style="{ backgroundColor: g.color }"></span>
            {{ g.name }}
            <span v-if="g.category?.shared" title="Совместная категория">👥</span>
            <span class="count">{{ g.tasks.length }}</span>
          </button>
          <button v-if="g.category" class="gear" title="Настройки" @click="settingsCategory = g.category">⚙️</button>
        </div>
        <template v-if="!collapsed.has(g.id)">
          <TaskRow
            v-for="t in g.tasks"
            :key="t.id"
            :task="t"
            reorderable
            @complete="tasks.complete(t)"
            @open="openTaskId = t.id"
            @move-up="tasks.move(t, -1, g.tasks)"
            @move-down="tasks.move(t, 1, g.tasks)"
          />
          <p v-if="g.tasks.length === 0" class="group-empty">Нет задач</p>
        </template>
      </div>

      <form v-if="addingCategory" class="add-category" @submit.prevent="onAddCategory">
        <input v-model="newCategoryName" placeholder="Название категории…" maxlength="100" />
        <button type="submit" :disabled="!newCategoryName.trim()">Создать</button>
        <button type="button" @click="addingCategory = false">✕</button>
      </form>
      <button v-else class="new-category-btn" @click="addingCategory = true">＋ Новая категория</button>
    </template>

    <!-- Выполненные -->
    <template v-else>
      <p v-if="tasks.doneTasks.value.length === 0" class="empty">Лог пуст</p>
      <div v-for="g in doneGroups" :key="g.day" class="date-group">
        <div class="date-label">{{ g.label }}</div>
        <TaskRow
          v-for="t in g.tasks"
          :key="t.id"
          :task="t"
          :category-name="categoryName(t)"
          done
          @complete="reopen(t)"
          @open="openTaskId = t.id"
        />
      </div>
    </template>
  </template>

  <TaskCardModal
    v-if="openTaskId !== null"
    :key="openTaskId"
    :task-id="openTaskId"
    :tasks="tasks"
    @close="openTaskId = null"
  />

  <CategorySettingsModal
    v-if="settingsCategory"
    :key="settingsCategory.id"
    :category="settingsCategory"
    @saved="onSaveCategory"
    @removed="onRemoveCategory"
    @left="onLeaveCategory"
    @close="settingsCategory = null"
  />
</template>

<style scoped>
.loading,
.empty {
  text-align: center;
  color: var(--text-secondary);
  padding: 32px 0;
}

.quick-add {
  display: flex;
  gap: 6px;
  margin-bottom: 12px;
}

.quick-add input {
  flex: 1;
  min-width: 0;
}

.quick-add select {
  flex: none;
  max-width: 110px;
}

.quick-add button {
  flex: none;
  width: 42px;
  border: none;
  border-radius: 8px;
  background: var(--accent-color);
  color: #fff;
  font-size: 18px;
}

.quick-add button:disabled {
  opacity: 0.5;
}

.tabs {
  display: flex;
  gap: 4px;
  margin-bottom: 12px;
  overflow-x: auto;
}

.tab {
  flex: 1;
  padding: 7px 10px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: 13px;
  white-space: nowrap;
}

.tab.on {
  background: var(--accent-color);
  color: #fff;
}

.date-group {
  margin-bottom: 14px;
}

.date-label {
  font-size: 12px;
  font-weight: 700;
  color: var(--text-secondary);
  margin-bottom: 6px;
  text-transform: capitalize;
}

.date-label.overdue {
  color: #f44336;
}

.category-group {
  background: var(--card-color);
  border-radius: 10px;
  padding: 10px;
  margin-bottom: 12px;
}

.group-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
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
  font-size: 15px;
  text-align: left;
  padding: 0;
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

.dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  flex: none;
}

.count {
  font-weight: 400;
  font-size: 12px;
  color: var(--text-secondary);
}

.gear {
  background: none;
  border: none;
  padding: 2px 6px;
  font-size: 15px;
  flex: none;
}

.group-empty {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0;
}

.add-category {
  display: flex;
  gap: 6px;
}

.add-category input {
  flex: 1;
  min-width: 0;
}

.add-category button {
  flex: none;
  border: none;
  border-radius: 8px;
  padding: 0 12px;
  background: var(--bg-secondary);
  color: var(--text-color);
}

.new-category-btn {
  display: block;
  width: 100%;
  padding: 10px;
  border: none;
  border-radius: 10px;
  background: var(--card-color);
  color: var(--text-secondary);
}

/* карточки-«стекло»: размытие фона под .group (класс неоднозначный —
   правило scoped, чтобы не задеть одноимённые не-карточки) */
:root[data-card-glass] .group {
  backdrop-filter: blur(var(--card-blur, 0px));
  -webkit-backdrop-filter: blur(var(--card-blur, 0px));
}
</style>
