<script setup lang="ts">
// «Профиль» — кто я, чем вошёл и с каких устройств.
//
// Связки IP+устройство собирает сервер при каждом заходе (раз в 5 минут на
// пользователя); раньше их видел только админ в чужой карточке — владельцу
// они нужнее.
import { computed, onMounted, ref } from 'vue'
import { api } from '../../../shared/api/client'
import { authMode } from '../../../shared/auth'
import { isAdmin, me } from '../../../shared/me'
import { confirmAction } from '../../../shared/telegram'
import { showToast } from '../../../shared/toast'

interface Profile {
  id: number
  username: string
  first_name: string
  created_at: string
  last_seen_at: string
  user_type: string
  last_ip: string
  last_device: string
}

interface Device {
  ip: string
  device: string
  created_at: string
}

const profile = ref<Profile | null>(null)
const devices = ref<Device[]>([])
const showDevices = ref(false)
const busy = ref(false)

onMounted(load)

async function load() {
  try {
    const res = await api.get<{ profile: Profile; devices: Device[] }>('/settings/profile')
    profile.value = res.profile
    devices.value = res.devices ?? []
  } catch {
    /* нет сети — блок останется на данных из /me */
  }
}

const name = computed(() =>
  profile.value?.first_name || me.value?.first_name || me.value?.username || 'без имени',
)

const login = computed(() =>
  authMode.value === 'token' ? 'по токену доступа (браузер или расширение)' : 'через Telegram',
)

/** Часовой пояс устройства: сервер хранит время в UTC, а человек живёт в своём. */
const timezone = computed(() => {
  try {
    const zone = Intl.DateTimeFormat().resolvedOptions().timeZone
    const off = -new Date().getTimezoneOffset() / 60
    return `${zone} (UTC${off >= 0 ? '+' : ''}${off})`
  } catch {
    return ''
  }
})

function when(value?: string): string {
  if (!value) return '—'
  const d = new Date(value)
  return d.toLocaleString('ru-RU', { dateStyle: 'medium', timeStyle: 'short' })
}

async function revokeAll() {
  if (!(await confirmAction(
    'Отозвать все токены доступа? Веб-версия и расширение попросят войти заново. Telegram это не затронет.',
  ))) return
  busy.value = true
  try {
    const { revoked } = await api.post<{ revoked: number }>('/settings/tokens/revoke-all', {})
    showToast(revoked ? `Отозвано токенов: ${revoked}` : 'Активных токенов не было')
  } catch {
    showToast('Не удалось отозвать токены')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <p class="line big">
    {{ name }}
    <span v-if="me?.username" class="dim">@{{ me.username }}</span>
    <span v-if="isAdmin" class="badge">админ</span>
  </p>
  <p class="line dim">id {{ profile?.id ?? me?.id ?? '—' }} · вход {{ login }}</p>
  <p v-if="profile" class="line dim">
    Первый заход: {{ when(profile.created_at) }} · последний: {{ when(profile.last_seen_at) }}
  </p>
  <p v-if="timezone" class="line dim">Часовой пояс устройства: {{ timezone }}</p>

  <button v-if="devices.length" class="btn-link" @click="showDevices = !showDevices">
    {{ showDevices ? 'Скрыть устройства' : `Мои устройства и сеансы (${devices.length})` }}
  </button>

  <div v-if="showDevices" class="devices">
    <p class="hint">
      Связка «адрес + устройство» запоминается при первом заходе. Список
      показывает, откуда в приложение заходили, а не открытые сеансы: выйти
      из Telegram приложение не может — его сеанс подписывает сам Telegram.
    </p>
    <div v-for="(d, i) in devices" :key="i" class="device">
      <span class="dev-name">{{ d.device || 'неизвестное устройство' }}</span>
      <span class="dim">{{ d.ip }} · с {{ when(d.created_at) }}</span>
    </div>
    <button class="btn" :disabled="busy" @click="revokeAll">
      Отозвать все токены доступа
    </button>
  </div>
</template>

<style scoped>
.line {
  margin: 0 0 6px;
  font-size: 14px;
}

.line.big {
  font-size: 16px;
}

.dim {
  color: var(--text-secondary);
  font-size: 13px;
}

.badge {
  background: var(--accent-color);
  border-radius: 6px;
  color: #fff;
  font-size: 11px;
  padding: 1px 6px;
  margin-left: 6px;
  vertical-align: middle;
}

.btn-link {
  margin-top: 4px;
  padding: 0;
  border: none;
  background: none;
  color: var(--accent-color);
  font-size: 13px;
  cursor: pointer;
}

.devices {
  margin-top: 8px;
}

.hint {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 8px;
}

.device {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 6px 0;
  border-top: 1px solid var(--border-color, rgba(128, 128, 128, 0.25));
}

.dev-name {
  font-size: 14px;
}

.btn {
  margin-top: 10px;
  background: var(--bg-color);
  border: none;
  border-radius: 8px;
  color: #ef4444;
  cursor: pointer;
  font-size: 14px;
  padding: 8px 12px;
}

.btn:disabled {
  opacity: 0.6;
}
</style>
