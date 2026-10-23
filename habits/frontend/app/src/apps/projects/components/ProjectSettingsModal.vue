<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '../../../shared/api/client'
import RecipientPicker from '../../../components/RecipientPicker.vue'
import { showToast } from '../../../shared/toast'
import * as projApi from '../api'
import type { HistoryEntry, Project, ProjectCategory, ProjectStatus, ShareUser } from '../types'
import { assetUrl, fmtDateTime, PRESET_TYPES, STATUS_LABELS, userLabel } from '../types'

// project = null — режим создания
const props = defineProps<{
  project: Project | null
  categories: ProjectCategory[]
  types: string[]
}>()

const emit = defineEmits<{
  saved: [project: Project]
  removed: [id: number]
  close: []
}>()

const p = props.project

const name = ref(p?.name ?? '')
const description = ref(p?.description ?? '')
const icon = ref(p?.icon ?? '')
const color = ref(p?.color ?? '#607d8b')
const ptype = ref(p?.ptype ?? '')
const status = ref<ProjectStatus>(p?.status ?? 'draft')
const tagsStr = ref(p?.tags.join(', ') ?? '')
const startDate = ref(p?.start_date ?? '')
const dueDate = ref(p?.due_date ?? '')
const tz = ref(p?.tz ?? '')
const categoryId = ref<number | null>(p?.category_id ?? null)

const saving = ref(false)
const confirmDelete = ref(false)
const confirmLeave = ref(false)

const typeSuggestions = computed(() => {
  const all = [...PRESET_TYPES, ...props.types]
  return [...new Set(all)]
})

const TZ_SUGGESTIONS = [
  'UTC',
  'Europe/Kaliningrad',
  'Europe/Moscow',
  'Europe/Samara',
  'Asia/Yekaterinburg',
  'Asia/Omsk',
  'Asia/Krasnoyarsk',
  'Asia/Irkutsk',
  'Asia/Yakutsk',
  'Asia/Vladivostok',
  'Europe/Berlin',
  'America/New_York',
]

function fields(): Record<string, unknown> {
  return {
    name: name.value.trim(),
    description: description.value.trim(),
    icon: icon.value.trim(),
    color: color.value,
    ptype: ptype.value.trim(),
    status: status.value,
    tags: tagsStr.value
      .split(',')
      .map((t) => t.trim())
      .filter(Boolean),
    start_date: startDate.value || '',
    due_date: dueDate.value || '',
    tz: tz.value.trim(),
    category_id: categoryId.value,
  }
}

async function save() {
  if (!name.value.trim() || saving.value) return
  saving.value = true
  try {
    const { project } = p
      ? await projApi.updateProject(p.id, fields())
      : await projApi.createProject(fields())
    emit('saved', project)
  } catch {
    showToast('Не удалось сохранить проект')
  } finally {
    saving.value = false
  }
}

async function remove() {
  if (!p) return
  try {
    await projApi.deleteProject(p.id)
    emit('removed', p.id)
  } catch {
    showToast('Не удалось удалить')
  }
}

// --- обложка ---

const coverInput = ref<HTMLInputElement | null>(null)
const cover = ref(p?.cover ?? '')

async function onCoverPicked(ev: Event) {
  const f = (ev.target as HTMLInputElement).files?.[0]
  ;(ev.target as HTMLInputElement).value = ''
  if (!f || !p) return
  try {
    const up = await projApi.uploadFile(p.id, f)
    if (!up.image) {
      showToast('Обложка должна быть картинкой')
      return
    }
    const { project } = await projApi.updateProject(p.id, { cover: up.url })
    cover.value = project.cover
    emit('saved', project)
  } catch {
    showToast('Не удалось загрузить обложку')
  }
}

async function removeCover() {
  if (!p) return
  try {
    const { project } = await projApi.updateProject(p.id, { cover: '' })
    cover.value = ''
    emit('saved', project)
  } catch {
    showToast('Не удалось убрать обложку')
  }
}

// --- шаринг ---

const shareTo = ref('')
const sharing = ref(false)
const members = ref<ShareUser[]>([])
const membersLoaded = ref(false)

if (p) {
  projApi
    .fetchShares(p.id)
    .then((d) => {
      members.value = d.users
      membersLoaded.value = true
    })
    .catch(() => {})
}

async function share() {
  const to = shareTo.value.trim()
  if (!to || !p || sharing.value) return
  sharing.value = true
  try {
    const { shared_with, queued } = await projApi.shareProject(p.id, to)
    shareTo.value = ''
    if (queued) {
      showToast(`Отправлено на подтверждение: ${userLabel(shared_with)} ⏳`)
    } else {
      if (!members.value.some((u) => u.id === shared_with.id)) members.value.push(shared_with)
      showToast(`Доступ открыт: ${userLabel(shared_with)}`)
    }
  } catch {
    showToast('Пользователь не найден')
  } finally {
    sharing.value = false
  }
}

async function revoke(u: ShareUser) {
  if (!p) return
  try {
    await projApi.revokeShare(p.id, u.id)
    members.value = members.value.filter((m) => m.id !== u.id)
  } catch {
    showToast('Не удалось отозвать доступ')
  }
}

async function leave() {
  if (!p) return
  try {
    // участник убирает себя
    const me = await api.get<{ id: number }>('/me')
    await projApi.revokeShare(p.id, me.id)
    emit('removed', p.id)
  } catch {
    showToast('Не удалось покинуть проект')
  }
}

// --- история ---

const historyOpen = ref(false)
const history = ref<HistoryEntry[]>([])
const historyLoaded = ref(false)

async function toggleHistory() {
  historyOpen.value = !historyOpen.value
  if (historyOpen.value && !historyLoaded.value && p) {
    try {
      history.value = (await projApi.fetchHistory(p.id)).history
      historyLoaded.value = true
    } catch {
      showToast('Не удалось загрузить историю')
    }
  }
}
</script>

<template>
  <Teleport to="body">
    <div class="modal" @click.self="emit('close')">
      <div class="modal-content card pcard">
        <h3>{{ p ? 'Настройки проекта' : 'Новый проект' }}</h3>

        <template v-if="!p || p.mine">
          <label class="field">
            <span>Название проекта</span>
            <input v-model="name" maxlength="200" placeholder="Мой проект" />
          </label>

          <label class="field">
            <span>Краткое описание</span>
            <textarea v-model="description" rows="2" maxlength="2000"></textarea>
          </label>

          <div class="two-cols">
            <label class="field">
              <span>Иконка (эмодзи)</span>
              <input v-model="icon" maxlength="8" placeholder="📦" />
            </label>
            <label class="field">
              <span>Цвет</span>
              <input v-model="color" type="color" class="color-input" />
            </label>
          </div>

          <label v-if="p" class="field">
            <span>Обложка</span>
            <img v-if="cover" class="cover-preview" :src="assetUrl(cover)" alt="" />
            <div class="cover-btns">
              <button class="btn small" type="button" @click="coverInput?.click()">
                🖼 {{ cover ? 'Заменить' : 'Загрузить' }}
              </button>
              <button v-if="cover" class="btn small" type="button" @click="removeCover">✕ Убрать</button>
            </div>
            <input ref="coverInput" type="file" accept="image/*" hidden @change="onCoverPicked" />
          </label>
          <p v-else class="hint">Обложку можно загрузить после создания проекта.</p>

          <label class="field">
            <span>Тип проекта</span>
            <input v-model="ptype" maxlength="100" list="ptype-suggestions" placeholder="рабочий, личный…" />
            <datalist id="ptype-suggestions">
              <option v-for="t in typeSuggestions" :key="t" :value="t" />
            </datalist>
          </label>

          <label class="field">
            <span>Статус проекта</span>
            <select v-model="status">
              <option v-for="(label, s) in STATUS_LABELS" :key="s" :value="s">{{ label }}</option>
            </select>
          </label>

          <label class="field">
            <span>Категория</span>
            <select v-model="categoryId">
              <option :value="null">— общий список —</option>
              <option v-for="c in categories" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
          </label>

          <div class="two-cols">
            <label class="field">
              <span>Дата начала</span>
              <input v-model="startDate" type="date" />
            </label>
            <label class="field">
              <span>План завершения</span>
              <input v-model="dueDate" type="date" />
            </label>
          </div>

          <label class="field">
            <span>Часовой пояс проекта</span>
            <input v-model="tz" maxlength="64" list="tz-suggestions" placeholder="Europe/Moscow" />
            <datalist id="tz-suggestions">
              <option v-for="z in TZ_SUGGESTIONS" :key="z" :value="z" />
            </datalist>
          </label>

          <label class="field">
            <span>Теги (через запятую)</span>
            <input v-model="tagsStr" placeholder="дом, ремонт, 2026" />
          </label>

          <div v-if="p" class="info">
            <div>Владелец: <b>{{ p.owner_name }}</b></div>
            <div>Создан: {{ fmtDateTime(p.created_at) }}</div>
            <div>Изменён: {{ fmtDateTime(p.updated_at) }}</div>
          </div>

          <!-- совместный доступ -->
          <div v-if="p" class="share-block">
            <div class="section-title">Совместный доступ</div>
            <div v-if="members.length" class="members">
              <span v-for="u in members" :key="u.id" class="member-chip">
                {{ userLabel(u) }}
                <button class="row-x" title="Отозвать доступ" @click="revoke(u)">✕</button>
              </span>
            </div>
            <RecipientPicker v-model="shareTo" />
            <button class="btn small" :disabled="!shareTo.trim() || sharing" @click="share">
              👥 Поделиться проектом
            </button>
            <p class="hint">
              Участники видят проект, добавляют и редактируют блоки. Если проект изменили без вас —
              в списке появится ★.
            </p>
          </div>

          <button class="btn primary" :disabled="!name.trim() || saving" @click="save">
            💾 {{ p ? 'Сохранить' : 'Создать проект' }}
          </button>
          <template v-if="p">
            <button v-if="!confirmDelete" class="btn danger" @click="confirmDelete = true">
              🗑 Удалить проект
            </button>
            <button v-else class="btn danger" @click="remove">
              Точно удалить проект со всеми блоками?
            </button>
          </template>
        </template>

        <!-- участник (не владелец): только просмотр и «покинуть» -->
        <template v-else>
          <div class="info">
            <div>Владелец: <b>{{ p.owner_name }}</b></div>
            <div>Создан: {{ fmtDateTime(p.created_at) }}</div>
            <div>Изменён: {{ fmtDateTime(p.updated_at) }}</div>
          </div>
          <p class="hint">👥 Совместный проект — параметры меняет владелец.</p>
          <button v-if="!confirmLeave" class="btn danger" @click="confirmLeave = true">
            🚪 Покинуть проект
          </button>
          <button v-else class="btn danger" @click="leave">
            Точно покинуть? Проект исчезнет из вашего списка
          </button>
        </template>

        <!-- история изменений -->
        <div v-if="p" class="history">
          <button class="btn small" @click="toggleHistory">
            🕓 История изменений {{ historyOpen ? '▴' : '▾' }}
          </button>
          <div v-if="historyOpen" class="history-list">
            <div v-for="e in history" :key="e.id" class="history-row">
              <span class="h-at">{{ fmtDateTime(e.at) }}</span>
              <span class="h-text"><b>{{ e.user_name }}</b> {{ e.action }}</span>
            </div>
            <p v-if="historyLoaded && !history.length" class="hint">Пока пусто</p>
          </div>
        </div>

        <button class="btn" @click="emit('close')">Закрыть</button>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.pcard {
  max-height: 88vh;
  overflow-y: auto;
  text-align: left;
}

.pcard h3 {
  text-align: center;
  margin-top: 0;
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
.field select,
.field textarea {
  width: 100%;
}

.field textarea {
  resize: vertical;
}

.color-input {
  height: 42px;
  padding: 2px;
}

.two-cols {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

.cover-preview {
  width: 100%;
  max-height: 120px;
  object-fit: cover;
  border-radius: 8px;
  margin-bottom: 6px;
}

.cover-btns {
  display: flex;
  gap: 6px;
}

.info {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 10px 0;
  line-height: 1.6;
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
  margin-bottom: 6px;
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

.row-x {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 12px;
  padding: 2px 5px;
}

.hint {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 4px 0 0;
}

.history {
  margin-top: 10px;
}

.history-list {
  margin-top: 6px;
  max-height: 30vh;
  overflow-y: auto;
}

.history-row {
  display: flex;
  gap: 8px;
  font-size: 12px;
  padding: 3px 0;
}

.h-at {
  flex: none;
  color: var(--text-secondary);
}

.h-text {
  overflow-wrap: anywhere;
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
  width: auto;
  display: inline-block;
  margin-top: 6px;
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
