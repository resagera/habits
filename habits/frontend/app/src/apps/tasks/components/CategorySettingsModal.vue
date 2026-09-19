<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import RecipientPicker from '../../../components/RecipientPicker.vue'
import { showToast } from '../../../shared/toast'
import * as tasksApi from '../api'
import { errorText } from '../composables/useTasks'
import type { CategoryStatus, ShareUser, TaskCategory } from '../types'
import { DEFAULT_STATUSES, PRIORITY_NAMES, REPEAT_NAMES, type RepeatKind, type StatusKind } from '../types'

const props = defineProps<{ category: TaskCategory }>()

const emit = defineEmits<{
  saved: [category: TaskCategory]
  removed: []
  left: []
  close: []
}>()

const name = ref(props.category.name)
const color = ref(props.category.color)
const confirmDelete = ref(false)
const confirmLeave = ref(false)
const saving = ref(false)

// --- статусы ---
const customStatuses = ref(!!props.category.statuses?.length)
const statuses = ref<CategoryStatus[]>(
  (props.category.statuses?.length ? props.category.statuses : DEFAULT_STATUSES).map((s) => ({ ...s })),
)

const KIND_LABELS: Record<StatusKind, string> = {
  open: '⏳ в работе',
  done: '✅ выполнена',
  archived: '🗄 архив',
}

function addStatus() {
  statuses.value.push({ name: '', kind: 'open' })
}

function removeStatus(i: number) {
  statuses.value.splice(i, 1)
}

const statusesError = computed(() => {
  if (!customStatuses.value) return ''
  const list = statuses.value.filter((s) => s.name.trim())
  if (list.length === 0) return 'Добавьте хотя бы один статус'
  if (!list.some((s) => s.kind === 'open') || !list.some((s) => s.kind === 'done'))
    return 'Нужны минимум один статус «в работе» и один «выполнена»'
  const names = list.map((s) => s.name.trim())
  if (new Set(names).size !== names.length) return 'Имена статусов не должны повторяться'
  return ''
})

// --- дефолты новых задач ---
const defPriority = ref<number>(props.category.defaults?.priority ?? -1)
const defRemind = ref<boolean>(props.category.defaults?.remind ?? false)
const defRemindBefore = ref<number>(props.category.defaults?.remind_before_min ?? 0)
const defRepeat = ref<'' | RepeatKind>(props.category.defaults?.repeat_kind ?? '')
const defRepeatParam = ref<number>(props.category.defaults?.repeat_param ?? 3)

// --- шаринг ---
const shareTo = ref('')
const members = ref<ShareUser[]>([])
const sharing = ref(false)

onMounted(async () => {
  if (!props.category.mine) return
  try {
    members.value = (await tasksApi.fetchCategoryShares(props.category.id)).users
  } catch {
    /* список просто не покажем */
  }
})

async function share() {
  const to = shareTo.value.trim()
  if (!to || sharing.value) return
  sharing.value = true
  try {
    const { shared_with } = await tasksApi.shareCategory(props.category.id, to)
    if (!members.value.some((u) => u.id === shared_with.id)) members.value.push(shared_with)
    shareTo.value = ''
    showToast(`Доступ открыт: ${memberLabel(shared_with)}`)
  } catch (e) {
    showToast(errorText(e))
  } finally {
    sharing.value = false
  }
}

async function revoke(u: ShareUser) {
  try {
    await tasksApi.revokeCategoryShare(props.category.id, u.id)
    members.value = members.value.filter((m) => m.id !== u.id)
  } catch (e) {
    showToast(errorText(e))
  }
}

function memberLabel(u: ShareUser): string {
  return u.first_name || (u.username ? '@' + u.username : `#${u.id}`)
}

async function save() {
  if (statusesError.value) {
    showToast(statusesError.value)
    return
  }
  saving.value = true
  const patch: Parameters<typeof tasksApi.updateCategory>[1] = {}
  const trimmed = name.value.trim()
  if (trimmed && trimmed !== props.category.name) patch.name = trimmed
  if (color.value !== props.category.color) patch.color = color.value
  patch.statuses = customStatuses.value
    ? statuses.value.filter((s) => s.name.trim()).map((s) => ({ name: s.name.trim(), kind: s.kind }))
    : null
  const defaults: Record<string, unknown> = {}
  if (defPriority.value >= 0) defaults.priority = defPriority.value
  if (defRemind.value) {
    defaults.remind = true
    defaults.remind_before_min = defRemindBefore.value
  }
  if (defRepeat.value) {
    defaults.repeat_kind = defRepeat.value
    if (defRepeat.value === 'interval') defaults.repeat_param = defRepeatParam.value
  }
  patch.defaults = Object.keys(defaults).length ? defaults : null
  try {
    const { project: saved } = await tasksApi.updateCategory(props.category.id, patch)
    emit('saved', saved)
  } catch (e) {
    showToast(errorText(e))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content card">
      <h3>Настройки категории</h3>

      <template v-if="category.mine">
        <label class="field">
          <span>Название</span>
          <input v-model="name" type="text" maxlength="100" />
        </label>

        <label class="field">
          <span>Цвет</span>
          <input v-model="color" type="color" class="color-input" />
        </label>

        <label class="check-line">
          <input v-model="customStatuses" type="checkbox" />
          <span>Свои статусы задач</span>
        </label>
        <div v-if="customStatuses" class="statuses">
          <div v-for="(s, i) in statuses" :key="i" class="status-row">
            <input v-model="s.name" placeholder="Название статуса" maxlength="60" />
            <select v-model="s.kind">
              <option v-for="(label, kind) in KIND_LABELS" :key="kind" :value="kind">
                {{ label }}
              </option>
            </select>
            <button class="row-x" @click="removeStatus(i)">✕</button>
          </div>
          <button class="btn small" @click="addStatus">＋ Статус</button>
          <p v-if="statusesError" class="err">{{ statusesError }}</p>
          <p class="hint">
            Статусы можно переключать в карточке задачи в любом порядке. «Выполнена» попадает в лог,
            «архив» скрывается из работы.
          </p>
        </div>

        <div class="section-title">Параметры новых задач категории</div>
        <label class="field">
          <span>Приоритет по умолчанию</span>
          <select v-model.number="defPriority">
            <option :value="-1">Не задан</option>
            <option v-for="(n, i) in PRIORITY_NAMES" :key="i" :value="i">{{ n }}</option>
          </select>
        </label>
        <label class="check-line">
          <input v-model="defRemind" type="checkbox" />
          <span>🔔 Напоминать (если задан срок)</span>
        </label>
        <label v-if="defRemind" class="field">
          <span>За сколько минут</span>
          <input v-model.number="defRemindBefore" type="number" min="0" max="10080" step="5" />
        </label>
        <label class="field">
          <span>Повторение по умолчанию</span>
          <select v-model="defRepeat">
            <option value="">Без повтора</option>
            <option v-for="(n, kind) in REPEAT_NAMES" :key="kind" :value="kind">{{ n }}</option>
          </select>
        </label>
        <label v-if="defRepeat === 'interval'" class="field">
          <span>Интервал, дней</span>
          <input v-model.number="defRepeatParam" type="number" min="1" max="365" />
        </label>

        <div class="share-block">
          <div class="section-title">Совместный доступ</div>
          <div v-if="members.length" class="members">
            <span v-for="u in members" :key="u.id" class="member-chip">
              {{ memberLabel(u) }}
              <button class="row-x" title="Отозвать доступ" @click="revoke(u)">✕</button>
            </span>
          </div>
          <RecipientPicker v-model="shareTo" />
          <button class="btn small" :disabled="!shareTo.trim() || sharing" @click="share">
            👥 Поделиться категорией
          </button>
          <p class="hint">Участники видят категорию и все её задачи, могут добавлять и выполнять их.</p>
        </div>

        <button class="btn primary" :disabled="saving" @click="save">💾 Сохранить</button>
        <button v-if="!confirmDelete" class="btn danger" @click="confirmDelete = true">
          🗑 Удалить категорию
        </button>
        <button v-else class="btn danger" @click="emit('removed')">
          Точно удалить? Задачи переедут во «Входящие»
        </button>
      </template>

      <template v-else>
        <p class="hint">👥 Совместная категория — настройки меняет владелец.</p>
        <button v-if="!confirmLeave" class="btn danger" @click="confirmLeave = true">
          🚪 Покинуть категорию
        </button>
        <button v-else class="btn danger" @click="emit('left')">
          Точно покинуть? Категория исчезнет из вашего списка
        </button>
      </template>

      <button class="btn" @click="emit('close')">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.card {
  max-height: 88vh;
  overflow-y: auto;
  text-align: left;
}

.card h3 {
  text-align: center;
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

.color-input {
  height: 42px;
  padding: 2px;
}

.check-line {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  margin: 6px 0 10px;
  cursor: pointer;
}

.check-line input[type='checkbox'] {
  width: 18px !important;
  height: 18px;
  flex: none;
  margin: 0;
}

.statuses {
  margin-bottom: 8px;
}

.status-row {
  display: flex;
  gap: 6px;
  margin-bottom: 6px;
  align-items: center;
}

.status-row input {
  flex: 1;
  min-width: 0;
}

.status-row select {
  flex: none;
  width: 128px;
}

.row-x {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 12px;
  padding: 2px 5px;
  flex: none;
}

.section-title {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 14px 0 6px;
  padding-top: 8px;
  border-top: 1px solid var(--bg-secondary);
}

.members {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.member-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: var(--bg-secondary);
  border-radius: 12px;
  padding: 4px 6px 4px 11px;
  font-size: 12px;
}

.err {
  color: #f44336;
  font-size: 12px;
  margin: 4px 0 0;
}

.hint {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 4px 0 0;
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

.btn.small {
  font-size: 13px;
  padding: 8px;
}

.btn:disabled {
  opacity: 0.5;
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
