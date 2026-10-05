<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import MarkdownView from '../../articles/components/MarkdownView.vue'
import { showToast } from '../../../shared/toast'
import * as tasksApi from '../api'
import { errorText, type Tasks } from '../composables/useTasks'
import type { ChecklistItem, ShareUser, Task } from '../types'
import {
  currentTzOffset,
  PRIORITY_NAMES,
  REPEAT_NAMES,
  statusesOf,
  type RepeatKind,
} from '../types'

const props = defineProps<{
  taskId: number
  tasks: Tasks
}>()

const emit = defineEmits<{ close: [] }>()

const task = ref<Task | null>(null)
const checklist = ref<ChecklistItem[]>([])
const loading = ref(true)

// поля формы
const title = ref('')
const note = ref('')
const editNote = ref(false)
const status = ref('')
const priority = ref(0)
const categoryId = ref<number | null>(null)
const dueDate = ref('')
const dueTime = ref('')
const remind = ref(false)
const remindBefore = ref(0)
const repeatKind = ref<'' | RepeatKind>('')
const repeatParam = ref(3)
const assigneeId = ref<number | null>(null)

const newItem = ref('')
const confirmDelete = ref(false)
const members = ref<ShareUser[]>([])

const category = computed(() =>
  props.tasks.categories.value.find((p) => p.id === categoryId.value) ?? null,
)
const statusList = computed(() => statusesOf(category.value))
const isShared = computed(() => category.value?.shared ?? false)

onMounted(async () => {
  try {
    const data = await tasksApi.fetchTask(props.taskId)
    task.value = data.task
    checklist.value = data.checklist
    const t = data.task
    title.value = t.title
    note.value = t.note
    status.value = t.status
    priority.value = t.priority
    categoryId.value = t.project_id
    dueDate.value = t.due_date ?? ''
    dueTime.value = t.due_time ?? ''
    remind.value = t.remind
    remindBefore.value = t.remind_before_min
    repeatKind.value = t.repeat_kind ?? ''
    repeatParam.value = t.repeat_param ?? 3
    assigneeId.value = t.assignee_id
    if (t.project_id) loadMembers(t.project_id)
  } catch (e) {
    showToast(errorText(e))
    emit('close')
  } finally {
    loading.value = false
  }
})

async function loadMembers(pid: number) {
  try {
    const users = (await tasksApi.fetchCategoryShares(pid)).users
    const p = props.tasks.categories.value.find((x) => x.id === pid)
    members.value = users
    // владелец категории — тоже возможный исполнитель
    if (p && !users.some((u) => u.id === p.owner_id)) {
      members.value = [{ id: p.owner_id, username: '', first_name: 'Владелец' }, ...users]
    }
  } catch {
    members.value = []
  }
}

function onCategoryChange() {
  // статусы другой категории могут отличаться — берём первый open
  status.value = statusesOf(category.value).find((s) => s.kind === 'open')?.name ?? 'Открыта'
  assigneeId.value = null
  if (categoryId.value) loadMembers(categoryId.value)
}

async function save() {
  const t = task.value
  if (!t) return
  const patch: Record<string, unknown> = {}
  const trimmed = title.value.trim()
  if (trimmed && trimmed !== t.title) patch.title = trimmed
  if (note.value !== t.note) patch.note = note.value
  if (status.value !== t.status || categoryId.value !== t.project_id) patch.status = status.value
  if (priority.value !== t.priority) patch.priority = priority.value
  if (categoryId.value !== t.project_id) patch.project_id = categoryId.value
  if ((dueDate.value || null) !== t.due_date) patch.due_date = dueDate.value || null
  if ((dueTime.value || null) !== t.due_time) patch.due_time = dueTime.value || null
  if (remind.value !== t.remind) patch.remind = remind.value
  if (remindBefore.value !== t.remind_before_min) patch.remind_before_min = remindBefore.value
  if ((repeatKind.value || null) !== t.repeat_kind) patch.repeat_kind = repeatKind.value || null
  if (repeatKind.value === 'interval') patch.repeat_param = repeatParam.value
  if (assigneeId.value !== t.assignee_id) patch.assignee_id = assigneeId.value
  if (Object.keys(patch).length === 0) {
    emit('close')
    return
  }
  patch.tz_offset_minutes = currentTzOffset()
  if (await props.tasks.patch(t.id, patch)) emit('close')
}

async function remove() {
  if (task.value && (await props.tasks.remove(task.value.id))) emit('close')
}

// --- чек-лист ---

async function addItem() {
  const name = newItem.value.trim()
  if (!name || !task.value) return
  try {
    const { item } = await tasksApi.addChecklistItem(task.value.id, name)
    checklist.value.push(item)
    newItem.value = ''
    bumpChecklist()
  } catch (e) {
    showToast(errorText(e))
  }
}

async function toggleItem(item: ChecklistItem) {
  try {
    const { item: updated } = await tasksApi.updateChecklistItem(item.id, { done: !item.done })
    const i = checklist.value.findIndex((x) => x.id === item.id)
    if (i >= 0) checklist.value[i] = updated
    bumpChecklist()
  } catch (e) {
    showToast(errorText(e))
  }
}

async function removeItem(item: ChecklistItem) {
  try {
    await tasksApi.deleteChecklistItem(item.id)
    checklist.value = checklist.value.filter((x) => x.id !== item.id)
    bumpChecklist()
  } catch (e) {
    showToast(errorText(e))
  }
}

/** Обновить счётчики «2/5» в списке без перезагрузки. */
function bumpChecklist() {
  const t = task.value
  if (!t) return
  const fresh = {
    ...t,
    checklist_total: checklist.value.length,
    checklist_done: checklist.value.filter((i) => i.done).length,
  }
  task.value = fresh
  props.tasks.replaceTask(fresh)
}

function memberLabel(u: ShareUser): string {
  return u.first_name || (u.username ? '@' + u.username : `#${u.id}`)
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content card">
      <div v-if="loading" class="loading">Загрузка…</div>
      <template v-else-if="task">
        <label class="field">
          <span>Задача</span>
          <input v-model="title" type="text" maxlength="300" />
        </label>

        <div class="two-cols">
          <label class="field">
            <span>Статус</span>
            <select v-model="status">
              <option v-for="s in statusList" :key="s.name" :value="s.name">{{ s.name }}</option>
              <option v-if="!statusList.some((s) => s.name === status)" :value="status">
                {{ status }}
              </option>
            </select>
          </label>
          <label class="field">
            <span>Приоритет</span>
            <select v-model.number="priority">
              <option v-for="(name, i) in PRIORITY_NAMES" :key="i" :value="i">{{ name }}</option>
            </select>
          </label>
        </div>

        <label class="field">
          <span>Категория</span>
          <select v-model="categoryId" @change="onCategoryChange">
            <option :value="null">📥 Входящие</option>
            <option v-for="p in tasks.categories.value" :key="p.id" :value="p.id">
              {{ p.name }}
            </option>
          </select>
        </label>

        <div class="two-cols">
          <label class="field">
            <span>Срок</span>
            <input v-model="dueDate" type="date" />
          </label>
          <label class="field">
            <span>Время</span>
            <input v-model="dueTime" type="time" :disabled="!dueDate" />
          </label>
        </div>

        <label v-if="dueDate" class="check-line">
          <input v-model="remind" type="checkbox" />
          <span>🔔 Напомнить ботом</span>
        </label>
        <label v-if="dueDate && remind" class="field">
          <span>За сколько минут до срока (0 — в момент срока)</span>
          <input v-model.number="remindBefore" type="number" min="0" max="10080" step="5" />
        </label>

        <label class="field">
          <span>Повторение (при выполнении создаётся следующая)</span>
          <select v-model="repeatKind">
            <option value="">Без повтора</option>
            <option v-for="(name, kind) in REPEAT_NAMES" :key="kind" :value="kind">
              {{ name }}
            </option>
          </select>
        </label>
        <label v-if="repeatKind === 'interval'" class="field">
          <span>Интервал, дней</span>
          <input v-model.number="repeatParam" type="number" min="1" max="365" />
        </label>

        <label v-if="isShared && members.length" class="field">
          <span>Исполнитель</span>
          <select v-model="assigneeId">
            <option :value="null">Не назначен</option>
            <option v-for="u in members" :key="u.id" :value="u.id">{{ memberLabel(u) }}</option>
          </select>
        </label>

        <div class="checklist">
          <div class="section-title">
            Подзадачи
            <span v-if="checklist.length">
              {{ checklist.filter((i) => i.done).length }}/{{ checklist.length }}
            </span>
          </div>
          <div v-for="item in checklist" :key="item.id" class="cl-item">
            <label class="cl-label">
              <input type="checkbox" :checked="item.done" @change="toggleItem(item)" />
              <span :class="{ struck: item.done }">{{ item.name }}</span>
            </label>
            <button class="cl-x" @click="removeItem(item)">✕</button>
          </div>
          <form class="cl-add" @submit.prevent="addItem">
            <input v-model="newItem" placeholder="Новая подзадача…" maxlength="300" />
            <button type="submit" :disabled="!newItem.trim()">＋</button>
          </form>
        </div>

        <div class="note-block">
          <div class="section-title">
            Описание (Markdown)
            <button class="link-btn" @click="editNote = !editNote">
              {{ editNote ? 'просмотр' : 'изменить' }}
            </button>
          </div>
          <textarea v-if="editNote || !note" v-model="note" rows="5" placeholder="Описание…"></textarea>
          <MarkdownView v-else :source="note" />
        </div>

        <button class="btn primary" @click="save">💾 Сохранить</button>
        <button v-if="!confirmDelete" class="btn danger" @click="confirmDelete = true">
          🗑 Удалить задачу
        </button>
        <button v-else class="btn danger" @click="remove">Точно удалить?</button>
        <button class="btn" @click="emit('close')">Отмена</button>
      </template>
    </div>
  </div>
</template>

<style scoped>
.card {
  max-height: 88vh;
  overflow-y: auto;
  text-align: left;
}

.loading {
  text-align: center;
  color: var(--text-secondary);
  padding: 24px 0;
}

.field {
  display: block;
  margin-bottom: 10px;
}

.field span {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
  margin-bottom: 3px;
}

.field input,
.field select {
  width: 100%;
}

.two-cols {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

.check-line {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  margin: 4px 0 10px;
  cursor: pointer;
}

.check-line input[type='checkbox'] {
  width: 18px !important;
  height: 18px;
  flex: none;
  margin: 0;
}

.section-title {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 12px 0 6px;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.checklist .cl-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 2px 0;
}

.cl-label {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  min-width: 0;
  cursor: pointer;
}

.cl-label input[type='checkbox'] {
  width: 17px !important;
  height: 17px;
  flex: none;
  margin: 0;
}

.cl-label span {
  overflow-wrap: anywhere;
}

.struck {
  text-decoration: line-through;
  color: var(--text-secondary);
}

.cl-x {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 12px;
  padding: 2px 6px;
}

.cl-add {
  display: flex;
  gap: 6px;
  margin-top: 6px;
}

.cl-add input {
  flex: 1;
  min-width: 0;
}

.cl-add button {
  flex: none;
  width: 38px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
  font-size: 16px;
}

.note-block textarea {
  width: 100%;
  resize: vertical;
}

.link-btn {
  background: none;
  border: none;
  color: var(--accent-color);
  font-size: 12px;
  padding: 0;
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
</style>
