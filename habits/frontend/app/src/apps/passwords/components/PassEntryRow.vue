<script setup lang="ts">
import type { VaultEntry } from '../crypto'
import TotpCode from './TotpCode.vue'

defineProps<{
  entry: VaultEntry
  revealed: boolean
}>()

const emit = defineEmits<{
  copyLogin: []
  copyPassword: []
  toggleReveal: []
  edit: []
  openUrl: []
}>()
</script>

<template>
  <div class="pass-item">
    <div class="pass-info">
      <div class="pass-name">{{ entry.name }}</div>
      <div v-if="entry.login" class="pass-login">{{ entry.login }}</div>
      <div v-if="entry.url" class="pass-url">{{ entry.url }}</div>
      <div v-if="revealed && entry.password" class="pass-value">{{ entry.password }}</div>
      <div v-if="entry.description" class="pass-desc">{{ entry.description }}</div>
      <TotpCode v-if="entry.totp" :secret="entry.totp" />
    </div>
    <div class="pass-actions">
      <button v-if="entry.url" class="icon-btn" title="Перейти по ссылке" @click="emit('openUrl')">🌐</button>
      <button v-if="entry.login" class="icon-btn" title="Копировать логин" @click="emit('copyLogin')">👤</button>
      <button v-if="entry.password" class="icon-btn" title="Копировать пароль" @click="emit('copyPassword')">📋</button>
      <button v-if="entry.password" class="icon-btn" title="Показать" @click="emit('toggleReveal')">
        {{ revealed ? '🙈' : '👁' }}
      </button>
      <button class="icon-btn" title="Редактировать" @click="emit('edit')">✏️</button>
    </div>
  </div>
</template>

<style scoped>
.pass-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  background: var(--card-color);
  border-radius: 8px;
  padding: 9px 12px;
  margin-bottom: 8px;
}

.pass-info {
  min-width: 0;
}

.pass-name {
  font-weight: 600;
}

.pass-login,
.pass-desc {
  font-size: 12px;
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}

.pass-url {
  font-size: 11px;
  color: var(--accent-color);
  overflow-wrap: anywhere;
}

.pass-value {
  font-family: monospace;
  font-size: 13px;
  overflow-wrap: anywhere;
}

.pass-actions {
  display: flex;
  gap: 0;
  flex: none;
  flex-wrap: wrap;
  justify-content: flex-end;
  max-width: 132px;
}

.icon-btn {
  background: none;
  border: none;
  padding: 4px 5px;
}

/* карточки-«стекло»: размытие под .pass-item (класс неоднозначный — scoped) */
:root[data-card-glass] .pass-item {
  backdrop-filter: blur(var(--card-blur, 0px));
  -webkit-backdrop-filter: blur(var(--card-blur, 0px));
}
</style>
