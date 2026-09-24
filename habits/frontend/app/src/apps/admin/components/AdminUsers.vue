<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { showToast } from '../../../shared/toast'
import {
  fetchUser,
  fetchUsers,
  setUserBanned,
  setUserType,
  USER_TYPE_LABELS,
  type AdminUser,
  type AdminUserDetail,
  type UserType,
} from '../adminApi'

const users = ref<AdminUser[]>([])
const total = ref(0)
const loading = ref(true)
const open = ref(false)
const fullscreen = ref(false)

const detail = ref<AdminUserDetail | null>(null)
const detailLoading = ref(false)
const confirmBan = ref(false)
const banBusy = ref(false)

onMounted(load)

async function load() {
  loading.value = true
  try {
    const res = await fetchUsers()
    users.value = res.users
    total.value = res.total
  } catch {
    showToast('Не удалось загрузить пользователей')
  } finally {
    loading.value = false
  }
}

function fmt(iso: string): string {
  return new Date(iso).toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    year: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function displayName(u: AdminUser): string {
  const name = u.first_name || u.username || `id ${u.id}`
  return u.username && u.first_name ? `${name} (@${u.username})` : name
}

async function openDetail(u: AdminUser) {
  detailLoading.value = true
  confirmBan.value = false
  try {
    detail.value = await fetchUser(u.id)
  } catch {
    showToast('Не удалось загрузить пользователя')
  } finally {
    detailLoading.value = false
  }
}

/** данные пользователя по страницам приложения (только счётчики) */
const dataPages = computed(() => {
  const d = detail.value?.data ?? {}
  const pages: { name: string; parts: string[] }[] = []
  const add = (name: string, parts: [string, number][]) => {
    const filled = parts.filter(([, n]) => n > 0).map(([label, n]) => `${label}: ${n}`)
    if (filled.length) pages.push({ name, parts: filled })
  }
  add('📊 Tracker', [['категорий', d.tracker_categories ?? 0], ['отметок', d.tracker_marks ?? 0]])
  add('✅ Checker', [['групп', d.checker_groups ?? 0], ['пунктов', d.checker_items ?? 0]])
  add('📔 Diary', [['записей', d.diary_entries ?? 0]])
  add('🔗 Links', [['ссылок', d.links ?? 0], ['папок', d.links_folders ?? 0]])
  add('⚙️ Фон', [['изображений', d.backgrounds ?? 0]])
  return pages
})

const typeBusy = ref(false)

async function changeType(ev: Event) {
  const d = detail.value
  if (!d) return
  const t = (ev.target as HTMLSelectElement).value as UserType
  typeBusy.value = true
  try {
    await setUserType(d.user.id, t)
    d.user.user_type = t
    const row = users.value.find((u) => u.id === d.user.id)
    if (row) row.user_type = t
    showToast(`Тип: ${USER_TYPE_LABELS[t]} ✅`)
  } catch {
    showToast('Не удалось изменить тип')
  } finally {
    typeBusy.value = false
  }
}

async function toggleBan() {
  const d = detail.value
  if (!d) return
  if (!d.user.banned && !confirmBan.value) {
    confirmBan.value = true
    setTimeout(() => (confirmBan.value = false), 4000)
    return
  }
  confirmBan.value = false
  banBusy.value = true
  try {
    const { banned } = await setUserBanned(d.user.id, !d.user.banned)
    d.user.banned = banned
    const row = users.value.find((u) => u.id === d.user.id)
    if (row) row.banned = banned
    showToast(banned ? 'Пользователь забанен 🚫' : 'Пользователь разбанен ✅')
  } catch {
    showToast('Не удалось изменить статус')
  } finally {
    banBusy.value = false
  }
}
</script>

<template>
  <section class="section">
    <div class="head">
      <button class="head-btn" @click="open = !open">
        <h3>👥 Пользователи <span class="total">({{ total }})</span> {{ open ? '▴' : '▾' }}</h3>
      </button>
      <button v-if="open" class="expand-btn" title="На весь экран" @click="fullscreen = true">⛶</button>
    </div>

    <div v-if="open && loading" class="hint">Загрузка…</div>
    <div v-else-if="open" class="user-list">
      <button v-for="u in users" :key="u.id" class="user-row" @click="openDetail(u)">
        <span class="user-name">
          <span v-if="u.banned" title="Забанен">🚫</span>
          {{ displayName(u) }}
        </span>
        <span class="user-seen">{{ fmt(u.last_seen_at) }}</span>
      </button>
    </div>
  </section>

  <!-- Полноэкранный список -->
  <div v-if="fullscreen" class="fullscreen">
    <div class="fullscreen-head">
      <h3>👥 Пользователи ({{ total }})</h3>
      <button class="expand-btn" @click="fullscreen = false">✕</button>
    </div>
    <div class="fullscreen-list">
      <button v-for="u in users" :key="u.id" class="user-row" @click="openDetail(u)">
        <span class="user-name">
          <span v-if="u.banned" title="Забанен">🚫</span>
          {{ displayName(u) }}
          <span class="user-sub">{{ u.last_device }} · {{ u.last_ip }}</span>
        </span>
        <span class="user-seen">{{ fmt(u.last_seen_at) }}</span>
      </button>
    </div>
  </div>

  <!-- Карточка пользователя -->
  <div v-if="detail || detailLoading" class="modal" @click.self="detail = null">
    <div class="modal-content detail">
      <template v-if="detail">
        <h3>
          {{ displayName(detail.user) }}
          <span v-if="detail.is_admin" class="badge admin">админ</span>
          <span v-if="detail.user.banned" class="badge banned">забанен</span>
        </h3>
        <p class="detail-line">id: {{ detail.user.id }}</p>
        <p class="detail-line">Первый заход: {{ fmt(detail.user.created_at) }}</p>
        <p class="detail-line">Последний заход: {{ fmt(detail.user.last_seen_at) }}</p>
        <p v-if="detail.user.last_device" class="detail-line">
          Устройство: {{ detail.user.last_device }} · {{ detail.user.last_ip }}
        </p>

        <label class="type-line">
          <span>Тип пользователя (лимиты Projects):</span>
          <select :value="detail.user.user_type" :disabled="typeBusy" @change="changeType">
            <option v-for="(label, t) in USER_TYPE_LABELS" :key="t" :value="t">{{ label }}</option>
          </select>
        </label>

        <h4>Устройства и IP</h4>
        <div class="devices">
          <p v-if="detail.devices.length === 0" class="hint">нет данных</p>
          <div v-for="(d, i) in detail.devices" :key="i" class="device-row">
            <span>{{ d.device }} · {{ d.ip }}</span>
            <span class="user-seen">{{ fmt(d.created_at) }}</span>
          </div>
        </div>

        <h4>Данные на страницах</h4>
        <p v-if="dataPages.length === 0" class="hint">данных нет</p>
        <div v-for="page in dataPages" :key="page.name" class="data-row">
          <b>{{ page.name }}</b> — {{ page.parts.join(', ') }}
        </div>

        <button
          v-if="!detail.is_admin"
          class="btn"
          :class="detail.user.banned ? 'primary' : 'danger'"
          :disabled="banBusy"
          @click="toggleBan"
        >
          {{
            banBusy
              ? '…'
              : detail.user.banned
                ? '✅ Разбанить'
                : confirmBan
                  ? 'Точно забанить этого пользователя?'
                  : '🚫 Забанить'
          }}
        </button>
        <button class="btn" @click="detail = null">Закрыть</button>
      </template>
      <p v-else class="hint">Загрузка…</p>
    </div>
  </div>
</template>

<style scoped>
.type-line {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  margin: 8px 0;
}

.type-line span {
  color: var(--text-secondary);
  font-size: 12px;
}

.type-line select {
  flex: 1;
}

.section {
  background: var(--card-color);
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 14px;
}

.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.head-btn {
  flex: 1;
  min-width: 0;
  background: none;
  border: none;
  color: var(--text-color);
  text-align: left;
  padding: 0;
}

.head h3 {
  margin: 0;
  font-size: 16px;
}

.total {
  color: var(--text-secondary);
  font-weight: 400;
}

.expand-btn {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 18px;
  padding: 4px 8px;
}

/* фиксированная высота, прокрутка внутри */
.user-list {
  max-height: 260px;
  overflow-y: auto;
  margin-top: 8px;
}

.user-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 6px;
  background: none;
  border: none;
  border-bottom: 1px solid var(--hover-bg-color);
  color: var(--text-color);
  text-align: left;
  font-size: 14px;
}

.user-name {
  min-width: 0;
  overflow-wrap: anywhere;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
}

.user-sub {
  font-size: 11px;
  color: var(--text-secondary);
}

.user-seen {
  flex: none;
  font-size: 12px;
  color: var(--text-secondary);
}

.hint {
  color: var(--text-secondary);
  font-size: 13px;
}

/* полноэкранный режим */
.fullscreen {
  position: fixed;
  inset: 0;
  background: var(--bg-color);
  z-index: 2500;
  display: flex;
  flex-direction: column;
}

.fullscreen-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  border-bottom: 1px solid var(--hover-bg-color);
}

.fullscreen-head h3 {
  margin: 0;
}

.fullscreen-list {
  flex: 1;
  overflow-y: auto;
  padding: 0 16px 16px;
}

/* карточка */
.detail {
  text-align: left;
  max-height: 85vh;
  overflow-y: auto;
}

.detail h3 {
  text-align: center;
}

.detail h4 {
  margin: 14px 0 6px;
  font-size: 14px;
}

.detail-line {
  margin: 3px 0;
  font-size: 13px;
  color: var(--text-secondary);
}

.badge {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 10px;
  vertical-align: middle;
}

.badge.admin {
  background: var(--accent-color);
  color: #fff;
}

.badge.banned {
  background: #b91c1c;
  color: #fff;
}

.devices {
  max-height: 160px;
  overflow-y: auto;
}

.device-row {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  font-size: 13px;
  padding: 4px 0;
  border-bottom: 1px solid var(--hover-bg-color);
}

.data-row {
  font-size: 13px;
  margin: 4px 0;
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
