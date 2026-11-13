<script setup lang="ts">
// Экран входа вне Telegram: приложение открыто в браузере или расширении,
// а токена ещё нет (или он был отозван/истёк). Токен выдаётся в мини-
// приложении: Telegram → Habits → Settings → «Токены доступа».
import { ref } from 'vue'
import { api } from '../shared/api/client'
import { clearToken, setToken } from '../shared/auth'

const TG_APP_URL = 'https://t.me/resagerHelperBot/res_vault_flow'

const value = ref('')
const busy = ref(false)
const error = ref('')

async function submit() {
  const token = value.value.trim()
  if (!token) return
  busy.value = true
  error.value = ''
  setToken(token)
  try {
    // сразу проверяем токен, чтобы не пускать в приложение с нерабочим
    await api.get('/me')
    location.reload() // перезапуск с чистым состоянием: данные подтянутся заново
  } catch {
    clearToken()
    error.value = 'Токен не подошёл — проверьте, что он скопирован целиком и не отозван.'
    value.value = ''
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="gate">
    <div class="card">
      <h1>Habits</h1>
      <p class="hint">
        Вход вне Telegram. Откройте мини-приложение →
        <b>Settings</b> → <b>Токены доступа</b>, создайте токен и вставьте его сюда.
      </p>
      <a class="tg-link" :href="TG_APP_URL" target="_blank" rel="noopener">
        Открыть приложение в Telegram ↗
      </a>
      <input
        v-model="value"
        type="password"
        class="field"
        placeholder="hbt_…"
        autocomplete="off"
        spellcheck="false"
        data-no-copy
        @keyup.enter="submit"
      />
      <p v-if="error" class="err">{{ error }}</p>
      <button class="btn" :disabled="busy || !value.trim()" @click="submit">
        {{ busy ? 'Проверка…' : 'Войти' }}
      </button>
      <p class="hint small">
        Токен хранится только в этом браузере. Отозвать его можно там же,
        в Настройках мини-приложения.
      </p>
    </div>
  </div>
</template>

<style scoped>
.gate {
  min-height: 100dvh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
}

.card {
  background: var(--card-color);
  border-radius: 12px;
  padding: 20px;
  width: 100%;
  max-width: 380px;
}

h1 {
  margin: 0 0 12px;
  font-size: 22px;
  text-align: center;
}

.hint {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 0 0 14px;
  line-height: 1.45;
}

.hint.small {
  font-size: 11px;
  margin: 12px 0 0;
}

.tg-link {
  display: block;
  text-align: center;
  color: var(--accent-color);
  text-decoration: none;
  font-size: 13px;
  padding: 9px;
  border: 1px solid var(--hover-bg-color);
  border-radius: 8px;
  margin-bottom: 14px;
}

.field {
  width: 100%;
  font: inherit;
  background: var(--bg-secondary);
  color: var(--text-color);
  border: 1px solid var(--hover-bg-color);
  border-radius: 8px;
  padding: 10px;
  margin-bottom: 10px;
}

.err {
  color: #ef4444;
  font-size: 12px;
  margin: 0 0 10px;
}

.btn {
  width: 100%;
  padding: 11px;
  border: none;
  border-radius: 8px;
  background: var(--accent-color);
  color: #fff;
  font: inherit;
}

.btn:disabled {
  opacity: 0.5;
}
</style>
