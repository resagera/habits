<script setup lang="ts">
// «Мои данные»: выгрузить всё одним файлом и удалить аккаунт.
//
// Выгрузка — это дамп базы по вашему id: сервер сам находит таблицы с вашими
// данными. Файлы (картинки, вложения, сейф) в неё не входят.
import { ref } from 'vue'
import { api } from '../../../shared/api/client'
import { clearToken } from '../../../shared/auth'
import { showToast } from '../../../shared/toast'

const busy = ref(false)
const confirmOpen = ref(false)
const word = ref('')

async function exportData() {
  busy.value = true
  try {
    const data = await api.get<Record<string, unknown>>('/settings/export')
    const url = URL.createObjectURL(
      new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }),
    )
    const a = document.createElement('a')
    a.href = url
    a.download = `habits-export-${new Date().toISOString().slice(0, 10)}.json`
    a.click()
    setTimeout(() => URL.revokeObjectURL(url), 5000)
    showToast('Файл с данными сохранён')
  } catch {
    showToast('Не удалось выгрузить данные')
  } finally {
    busy.value = false
  }
}

/** Слово вводится руками: подтверждение кнопкой для такого действия мало. */
async function deleteAccount() {
  if (word.value.trim() !== 'УДАЛИТЬ') {
    showToast('Впишите слово УДАЛИТЬ, чтобы подтвердить')
    return
  }
  busy.value = true
  try {
    await api.post('/settings/account/delete', { confirm: word.value.trim() })
    clearToken()
    localStorage.clear()
    showToast('Аккаунт удалён')
    setTimeout(() => location.reload(), 1200)
  } catch {
    showToast('Не удалось удалить аккаунт')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <p class="hint">
    Выгрузка — все ваши записи из базы одним JSON: трекер, чек-листы, дневник,
    задачи, ссылки, финансы и остальное. Файлы (картинки фонов, вложения,
    содержимое сейфа) в неё не входят.
  </p>
  <div class="row">
    <button class="btn" :disabled="busy" @click="exportData">⬇️ Выгрузить мои данные</button>
  </div>

  <p class="sub-title">Удаление аккаунта</p>
  <p class="hint">
    Удаляются все записи во всех разделах — без возможности вернуть.
    Сначала имеет смысл выгрузить данные.
  </p>
  <div v-if="!confirmOpen" class="row">
    <button class="btn danger" :disabled="busy" @click="confirmOpen = true">
      Удалить аккаунт
    </button>
  </div>
  <div v-else class="row">
    <input v-model="word" placeholder="Впишите УДАЛИТЬ" />
    <button class="btn danger" :disabled="busy || word.trim() !== 'УДАЛИТЬ'" @click="deleteAccount">
      Удалить навсегда
    </button>
    <button class="btn" @click="confirmOpen = false; word = ''">Отмена</button>
  </div>
</template>

<style scoped>
.hint {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 10px;
}

.sub-title {
  margin: 14px 0 4px;
  font-size: 14px;
}

.row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin: 8px 0;
  align-items: center;
}

.btn {
  background: var(--bg-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  cursor: pointer;
  font-size: 14px;
  padding: 8px 12px;
}

.btn.danger {
  color: #ef4444;
}

.btn:disabled {
  opacity: 0.6;
}

input {
  font-size: 14px;
}
</style>
