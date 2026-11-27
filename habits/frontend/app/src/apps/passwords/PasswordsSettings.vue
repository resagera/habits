<script setup lang="ts">
// Индивидуальные настройки страницы «Пароли»: экспорт/импорт хранилища и его
// сброс. Открывается из шестерёнки в шапке (см. PageSettingsModal).
import { onMounted, ref } from 'vue'
import { api } from '../../shared/api/client'
import { showToast } from '../../shared/toast'
import {
  decryptImport,
  exportEncryptedFile,
  exportPlainFile,
  parseImportFile,
  type ParsedImport,
} from './crypto'
import {
  deleteVaultEverywhere,
  entries as vaultEntries,
  folders as vaultFolders,
  initVault,
  lock,
  persistSession,
  sessionData,
  unlockWithPassword,
  unlocked as vaultUnlocked,
} from './session'

const hasVault = ref(true) // уточняется асинхронно в onMounted
const confirmVaultReset = ref(false)

const passMasterInput = ref('')
const passBusy = ref(false)
const exportEncrypt = ref(true)
const exportFilePassword = ref('')
const passImportInput = ref<HTMLInputElement>()
const pendingImport = ref<ParsedImport | null>(null)
const importFilePassword = ref('')
const confirmApplyImport = ref(false)

onMounted(() => {
  initVault().then((s) => (hasVault.value = s === 'exists'))
})

async function unlockForSettings() {
  if (!passMasterInput.value) return
  passBusy.value = true
  try {
    const res = await unlockWithPassword(passMasterInput.value)
    if (res === 'wrong') {
      showToast('Неверный мастер-пароль')
      return
    }
    if (res === 'empty') {
      hasVault.value = false
      return
    }
    passMasterInput.value = ''
  } finally {
    passBusy.value = false
  }
}

async function exportPasswords() {
  passBusy.value = true
  try {
    let content: string
    if (exportEncrypt.value) {
      if (exportFilePassword.value.length < 6) {
        showToast('Пароль файла — минимум 6 символов')
        return
      }
      content = await exportEncryptedFile(sessionData(), exportFilePassword.value)
    } else {
      content = exportPlainFile(sessionData())
    }
    const url = URL.createObjectURL(new Blob([content], { type: 'application/json' }))
    const a = document.createElement('a')
    a.href = url
    a.download = exportEncrypt.value ? 'passwords_backup_encrypted.json' : 'passwords_backup_PLAIN.json'
    a.click()
    URL.revokeObjectURL(url)
    exportFilePassword.value = ''
    showToast(exportEncrypt.value ? 'Экспортировано (зашифровано) 🔐' : 'Экспортировано БЕЗ шифрования ⚠️')
  } catch {
    showToast('Не удалось экспортировать')
  } finally {
    passBusy.value = false
  }
}

function onPassImportFile(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  const reader = new FileReader()
  reader.onload = () => {
    const parsed = parseImportFile(String(reader.result).trim())
    if (!parsed) {
      showToast('Не похоже на файл экспорта паролей')
      return
    }
    pendingImport.value = parsed
    importFilePassword.value = ''
    confirmApplyImport.value = false
  }
  reader.readAsText(file)
  ;(e.target as HTMLInputElement).value = ''
}

async function applyImport() {
  const parsed = pendingImport.value
  if (!parsed) return
  if (!confirmApplyImport.value) {
    confirmApplyImport.value = true
    setTimeout(() => (confirmApplyImport.value = false), 4000)
    return
  }
  confirmApplyImport.value = false
  passBusy.value = true
  try {
    let data
    if (parsed.kind === 'encrypted') {
      if (!importFilePassword.value) {
        showToast('Введите пароль файла')
        return
      }
      data = await decryptImport(parsed.container, importFilePassword.value)
      if (data === null) {
        showToast('Неверный пароль файла')
        return
      }
    } else {
      data = parsed.data
    }
    vaultFolders.value = data.folders
    vaultEntries.value = data.entries
    await persistSession()
    pendingImport.value = null
    importFilePassword.value = ''
    showToast(`Импортировано: ${data.entries.length} паролей ✅`)
  } catch {
    showToast('Не удалось импортировать')
  } finally {
    passBusy.value = false
  }
}

function resetVault() {
  if (!confirmVaultReset.value) {
    confirmVaultReset.value = true
    setTimeout(() => (confirmVaultReset.value = false), 4000)
    return
  }
  api.delete('/passwords/vault').catch(() => {})
  deleteVaultEverywhere()
  lock()
  hasVault.value = false
  confirmVaultReset.value = false
  showToast('Хранилище паролей удалено')
}
</script>

<template>
  <section class="section">
    <h3>Экспорт и импорт</h3>

    <template v-if="!hasVault">
      <p class="hint-text">Хранилище ещё не создано — откройте вкладку Passwords.</p>
    </template>

    <template v-else-if="!vaultUnlocked">
      <p class="hint-text">Для экспорта/импорта разблокируйте хранилище:</p>
      <div class="row">
        <input
          v-model="passMasterInput"
          type="password"
          placeholder="Мастер-пароль"
          autocomplete="off"
          class="grow"
          @keyup.enter="unlockForSettings"
        />
        <button class="btn slim" :disabled="passBusy" @click="unlockForSettings">🔓</button>
      </div>
    </template>

    <template v-else>
      <label class="radio">
        <input v-model="exportEncrypt" type="checkbox" />
        <span>🔐 Зашифровать файл (AES-256-GCM)</span>
      </label>
      <input
        v-if="exportEncrypt"
        v-model="exportFilePassword"
        type="password"
        placeholder="Пароль файла (можно отличный от мастер-пароля)"
        autocomplete="off"
        class="full-w"
      />
      <p v-if="!exportEncrypt" class="hint-text warn">
        ⚠️ Файл будет содержать пароли открытым текстом.
      </p>
      <button class="btn" :disabled="passBusy" @click="exportPasswords">📤 Экспортировать</button>

      <div class="row" style="margin-top: 12px">
        <button class="btn" @click="passImportInput?.click()">📥 Импорт из файла</button>
      </div>
      <input
        ref="passImportInput"
        type="file"
        accept=".json,application/json"
        class="hidden-input"
        @change="onPassImportFile"
      />
      <template v-if="pendingImport">
        <input
          v-if="pendingImport.kind === 'encrypted'"
          v-model="importFilePassword"
          type="password"
          placeholder="Пароль файла"
          autocomplete="off"
          class="full-w"
          style="margin-top: 8px"
        />
        <button class="btn danger" :disabled="passBusy" style="margin-top: 8px" @click="applyImport">
          {{
            confirmApplyImport
              ? 'Точно ЗАМЕНИТЬ все текущие пароли данными из файла?'
              : pendingImport.kind === 'encrypted'
                ? 'Расшифровать и заменить хранилище'
                : 'Заменить хранилище данными из файла'
          }}
        </button>
      </template>
    </template>
  </section>

  <section class="section">
    <h3>Сброс хранилища</h3>
    <p class="hint-text">
      Хранилище живёт только на этом устройстве. Сброс удалит все сохранённые пароли безвозвратно.
    </p>
    <button class="btn danger" :disabled="!hasVault" @click="resetVault">
      {{ !hasVault ? 'Хранилище не создано' : confirmVaultReset ? 'Точно удалить все пароли?' : 'Сбросить хранилище паролей' }}
    </button>
  </section>
</template>

<style scoped>
.section {
  background: var(--card-color);
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 14px;
}

.section h3 {
  margin: 0 0 10px;
  font-size: 16px;
}

.radio {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 0;
  cursor: pointer;
}

.row {
  display: flex;
  gap: 8px;
}

.btn {
  flex: 1;
  padding: 10px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
}

.btn.slim {
  flex: none;
}

.btn.danger {
  width: 100%;
  background: #b91c1c;
  color: #fff;
}

.btn:disabled {
  opacity: 0.5;
}

.hint-text {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 10px;
}

.hint-text.warn {
  color: #f59e0b;
}

.grow {
  flex: 1;
  min-width: 0;
}

.hidden-input {
  display: none;
}

.full-w {
  width: 100%;
  margin-bottom: 8px;
  font: inherit;
  background: var(--bg-secondary);
  color: var(--text-color);
  border: 1px solid var(--hover-bg-color);
  border-radius: 6px;
  padding: 8px;
}
</style>
