<script setup lang="ts">
import { onMounted, ref } from 'vue'
import RecipientPicker from '../../../components/RecipientPicker.vue'
import { showToast } from '../../../shared/toast'
import * as checkerApi from '../api'
import type { CheckTemplate } from '../api'
import type { CheckGroup } from '../types'

const emit = defineEmits<{ started: [group: CheckGroup] }>()

const templates = ref<CheckTemplate[]>([])
const open = ref(false)
const loaded = ref(false)

// модалка создания/редактирования
const modal = ref(false)
const editing = ref<CheckTemplate | null>(null)
const name = ref('')
const itemsText = ref('')
const confirmDelete = ref(false)

// прямое удаление из строки (двойное нажатие — подтверждение)
const confirmRowId = ref<number | null>(null)

// модалка шаринга
const shareModal = ref<CheckTemplate | null>(null)
const sendTo = ref('')
const inviteLink = ref('')
const sending = ref(false)

onMounted(async () => {
  try {
    templates.value = (await checkerApi.fetchTemplates()).templates
    loaded.value = true
  } catch {
    /* секция просто останется пустой */
  }
})

// пришли по ссылке-приглашению? redeem уже добавил шаблон — раскрываем секцию
defineExpose({
  reload: async () => {
    templates.value = (await checkerApi.fetchTemplates()).templates
  },
})

function openCreate() {
  editing.value = null
  name.value = ''
  itemsText.value = ''
  confirmDelete.value = false
  modal.value = true
}

function openEdit(t: CheckTemplate) {
  editing.value = t
  name.value = t.name
  itemsText.value = t.items.join('\n')
  confirmDelete.value = false
  modal.value = true
}

async function save() {
  const n = name.value.trim()
  if (!n) {
    showToast('Введите название')
    return
  }
  const items = itemsText.value
    .split('\n')
    .map((s) => s.trim())
    .filter(Boolean)
  try {
    if (editing.value) {
      const { template } = await checkerApi.updateTemplate(editing.value.id, n, items)
      const i = templates.value.findIndex((x) => x.id === template.id)
      if (i >= 0) templates.value[i] = template
    } else {
      const { template } = await checkerApi.createTemplate(n, items)
      templates.value.push(template)
    }
    modal.value = false
  } catch {
    showToast('Не удалось сохранить шаблон')
  }
}

async function remove() {
  if (!editing.value) return
  if (!confirmDelete.value) {
    confirmDelete.value = true
    setTimeout(() => (confirmDelete.value = false), 3500)
    return
  }
  try {
    await checkerApi.deleteTemplate(editing.value.id)
    templates.value = templates.value.filter((x) => x.id !== editing.value!.id)
    modal.value = false
  } catch {
    showToast('Не удалось удалить')
  }
}

async function removeRow(t: CheckTemplate) {
  if (confirmRowId.value !== t.id) {
    confirmRowId.value = t.id
    setTimeout(() => {
      if (confirmRowId.value === t.id) confirmRowId.value = null
    }, 3000)
    return
  }
  confirmRowId.value = null
  try {
    await checkerApi.deleteTemplate(t.id)
    templates.value = templates.value.filter((x) => x.id !== t.id)
  } catch {
    showToast('Не удалось удалить')
  }
}

async function start(t: CheckTemplate) {
  try {
    const { group } = await checkerApi.startTemplate(t.id)
    emit('started', group)
    showToast(`Чек-лист «${group.name}» создан ✅`)
  } catch {
    showToast('Не удалось запустить шаблон')
  }
}

async function openShare(t: CheckTemplate) {
  shareModal.value = t
  sendTo.value = ''
  inviteLink.value = ''
  try {
    const { link, token } = await checkerApi.shareToken(t.id)
    inviteLink.value = link || `chk_${token}`
  } catch {
    showToast('Не удалось получить ссылку')
  }
}

async function copyInvite() {
  try {
    await navigator.clipboard.writeText(inviteLink.value)
    showToast('Ссылка-приглашение скопирована 🔗')
  } catch {
    showToast('Не удалось скопировать')
  }
}

async function send() {
  const t = shareModal.value
  const to = sendTo.value.trim()
  if (!t || !to) return
  sending.value = true
  try {
    const { sent_to } = await checkerApi.sendTemplate(t.id, to)
    showToast(`Отправлено ${sent_to.first_name || '@' + sent_to.username || '#' + sent_to.id} 📤`)
    shareModal.value = null
  } catch (e) {
    showToast(e instanceof Error && e.message.includes('not') ? 'Пользователь не найден' : 'Не удалось отправить')
  } finally {
    sending.value = false
  }
}
</script>

<template>
  <div class="tpl-section">
    <button class="tpl-head" @click="open = !open">
      <span class="chevron" :class="{ open }">▸</span>
      📋 Шаблоны
      <span v-if="loaded" class="count">{{ templates.length }}</span>
    </button>

    <template v-if="open">
      <p class="tpl-hint">
        Многоразовые списки: «запустить» создаёт свежий чек-лист из шаблона.
        Шаблоном можно поделиться с другом.
      </p>

      <div v-for="t in templates" :key="t.id" class="tpl-row">
        <div class="tpl-info">
          <div class="tpl-name">{{ t.name }}</div>
          <div class="tpl-items">{{ t.items.length }} пунктов</div>
        </div>
        <span class="tpl-actions">
          <button class="icon-btn" title="Запустить — создать чек-лист" @click="start(t)">▶️</button>
          <button class="icon-btn" title="Поделиться" @click="openShare(t)">📤</button>
          <button class="icon-btn" title="Редактировать" @click="openEdit(t)">✏️</button>
          <button
            class="icon-btn del"
            :class="{ confirming: confirmRowId === t.id }"
            :title="confirmRowId === t.id ? 'Нажмите ещё раз' : 'Удалить шаблон'"
            @click="removeRow(t)"
          >
            {{ confirmRowId === t.id ? 'точно?' : '🗑' }}
          </button>
        </span>
      </div>

      <button class="tpl-add" @click="openCreate">＋ Новый шаблон</button>
    </template>
  </div>

  <!-- создание/редактирование -->
  <div v-if="modal" class="modal" @click.self="modal = false">
    <div class="modal-content tpl-modal">
      <h3>{{ editing ? 'Шаблон' : 'Новый шаблон' }}</h3>
      <input v-model="name" placeholder="Название (например, Сборы в поездку)" maxlength="200" />
      <label class="field-label">Пункты — по одному на строку</label>
      <textarea v-model="itemsText" rows="8" placeholder="Паспорт&#10;Зарядка&#10;Наушники"></textarea>
      <button class="btn primary" @click="save">💾 Сохранить</button>
      <button v-if="editing" class="btn danger" @click="remove">
        {{ confirmDelete ? 'Точно удалить шаблон?' : '🗑 Удалить' }}
      </button>
      <button class="btn" @click="modal = false">Отмена</button>
    </div>
  </div>

  <!-- шаринг -->
  <div v-if="shareModal" class="modal" @click.self="shareModal = null">
    <div class="modal-content tpl-modal">
      <h3>Поделиться «{{ shareModal.name }}»</h3>

      <label class="field-label">Пользователю приложения</label>
      <RecipientPicker v-model="sendTo" />
      <button class="btn primary" :disabled="sending || !sendTo.trim()" @click="send">
        {{ sending ? 'Отправка…' : '📤 Отправить' }}
      </button>

      <label class="field-label">Или ссылка-приглашение (для любого друга в Telegram)</label>
      <div class="invite-box">{{ inviteLink || 'получаем ссылку…' }}</div>
      <button class="btn" :disabled="!inviteLink" @click="copyInvite">🔗 Копировать ссылку</button>
      <p class="tpl-hint">
        Друг откроет ссылку, запустит приложение — и шаблон добавится ему автоматически.
      </p>

      <button class="btn" @click="shareModal = null">Закрыть</button>
    </div>
  </div>
</template>

<style scoped>
.tpl-section {
  background: var(--card-color);
  border-radius: 10px;
  padding: 6px 12px 10px;
  margin-bottom: 12px;
}

.tpl-head {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  background: none;
  border: none;
  color: var(--text-color);
  font-weight: 700;
  font-size: 14px;
  text-align: left;
  padding: 6px 0;
}

.count {
  font-size: 11px;
  color: var(--text-secondary);
}

.chevron {
  display: inline-block;
  transition: transform 0.15s;
  color: var(--text-secondary);
}

.chevron.open {
  transform: rotate(90deg);
}

.tpl-hint {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 2px 0 8px;
}

.tpl-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  padding: 6px 0;
  border-top: 1px solid var(--bg-secondary);
}

.tpl-info {
  min-width: 0;
}

.tpl-name {
  font-weight: 600;
  font-size: 14px;
  overflow-wrap: anywhere;
}

.tpl-items {
  font-size: 11px;
  color: var(--text-secondary);
}

.tpl-actions {
  flex: none;
}

.icon-btn {
  background: none;
  border: none;
  padding: 4px 5px;
}

.icon-btn.del.confirming {
  color: #ef4444;
  font-size: 11px;
  font-weight: 600;
}

.tpl-add {
  display: block;
  width: 100%;
  margin-top: 8px;
  padding: 8px;
  border: 1px dashed var(--text-secondary);
  border-radius: 8px;
  background: none;
  color: var(--text-secondary);
  font-size: 13px;
}

.tpl-modal {
  text-align: left;
}

.tpl-modal h3 {
  text-align: center;
}

.tpl-modal input,
.tpl-modal textarea {
  width: 100%;
  margin-top: 6px;
  resize: vertical;
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
