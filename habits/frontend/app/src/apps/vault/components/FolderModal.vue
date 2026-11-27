<script setup lang="ts">
/**
 * Создание папки и её настройки. Пароль задаётся ТОЛЬКО здесь; сменить его
 * можно, лишь зная текущий — ключ папки переворачивается на клиенте, сервер
 * получает новую обёртку и ничего о пароле не узнаёт.
 */
import { computed, ref } from 'vue'
import { showToast } from '../../../shared/toast'
import * as vaultApi from '../api'
import { createFolderKey, rewrapFolderKey } from '../crypto'
import { isUnlocked, lockFolder, passwordFor, unlock } from '../session'
import type { VaultFolder } from '../types'
import ShareBox from './ShareBox.vue'

const props = defineProps<{ folder: VaultFolder | null; parentId: number | null }>()
const emit = defineEmits<{ saved: [VaultFolder]; created: [VaultFolder]; deleted: [number]; close: [] }>()

const editing = computed(() => props.folder !== null)
const name = ref(props.folder?.name ?? '')
const hint = ref(props.folder?.hint ?? '')
const thumbs = ref(props.folder?.thumbs ?? true)
const hideChildren = ref(props.folder?.hide_children ?? false)
const autoDelete = ref(props.folder?.auto_delete_days ?? 0)
const password = ref('')
const password2 = ref('')
const oldPassword = ref('')
const confirmDelete = ref(false)
const busy = ref(false)

async function create() {
  if (busy.value) return
  if (!name.value.trim()) return showToast('Название нужно')
  if (password.value.length < 4) return showToast('Пароль слишком короткий')
  if (password.value !== password2.value) return showToast('Пароли не совпадают')
  busy.value = true
  try {
    const keys = await createFolderKey(password.value)
    const { folder } = await vaultApi.createFolder({
      parent_id: props.parentId,
      name: name.value.trim(),
      hint: hint.value.trim(),
      thumbs: thumbs.value,
      hide_children: hideChildren.value,
      auto_delete_days: autoDelete.value,
      kdf_salt: keys.kdf_salt,
      kdf_iter: keys.kdf_iter,
      wrapped_key: keys.wrapped_key,
      wrap_iv: keys.wrap_iv,
    })
    await unlock(folder, password.value) // сразу открыта: человек только что ввёл пароль
    emit('created', folder)
  } catch {
    showToast('Не удалось создать папку')
  } finally {
    busy.value = false
  }
}

async function save() {
  const f = props.folder
  if (!f || busy.value) return
  busy.value = true
  try {
    const patch: Record<string, unknown> = {}
    if (name.value.trim() && name.value.trim() !== f.name) patch.name = name.value.trim()
    if (hint.value.trim() !== f.hint) patch.hint = hint.value.trim()
    if (hideChildren.value !== f.hide_children) patch.hide_children = hideChildren.value
    if (autoDelete.value !== f.auto_delete_days) patch.auto_delete_days = autoDelete.value

    if (password.value) {
      if (password.value !== password2.value) {
        showToast('Пароли не совпадают')
        return
      }
      // текущий пароль либо уже в памяти (папка открыта), либо спрашиваем
      const current = passwordFor(f.id) ?? oldPassword.value
      if (!current) {
        showToast('Введите текущий пароль')
        return
      }
      const wrap = await rewrapFolderKey(current, f, password.value)
      if (!wrap) {
        showToast('Текущий пароль не подходит')
        return
      }
      Object.assign(patch, wrap)
    }
    if (Object.keys(patch).length === 0) {
      emit('close')
      return
    }
    const { folder } = await vaultApi.updateFolder(f.id, patch)
    if (password.value) {
      // ключ в памяти обёрнут старым паролем — запираем, чтобы открыли новым
      lockFolder(f.id)
      showToast('Пароль изменён — откройте папку заново')
    }
    emit('saved', folder)
  } catch {
    showToast('Не удалось сохранить')
  } finally {
    busy.value = false
  }
}

async function remove() {
  const f = props.folder
  if (!f) return
  try {
    await vaultApi.deleteFolder(f.id)
    emit('deleted', f.id)
  } catch {
    showToast('Не удалось удалить папку')
  }
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content">
      <h3>{{ editing ? 'Настройки папки' : 'Новая папка' }}</h3>

      <label class="field">
        <span>Название</span>
        <input v-model="name" type="text" maxlength="100" placeholder="Документы" />
      </label>

      <label class="field">
        <span>Подсказка к паролю (видна всем, кому открыт доступ)</span>
        <input v-model="hint" type="text" maxlength="200" placeholder="например: как обычно" />
      </label>

      <template v-if="!editing">
        <label class="field">
          <span>Пароль папки</span>
          <input v-model="password" type="password" autocomplete="new-password" />
        </label>
        <label class="field">
          <span>Ещё раз</span>
          <input v-model="password2" type="password" autocomplete="new-password" />
        </label>
        <label class="check">
          <input v-model="thumbs" type="checkbox" />
          <span>Делать превью для картинок</span>
        </label>
        <p class="hint">
          Превью включаются только сейчас: сервер их построить не может, а достроить задним числом
          нечем — файлы уже зашифрованы. Превью тоже шифруются и занимают место.
        </p>
        <label class="check">
          <input v-model="hideChildren" type="checkbox" />
          <span>Не показывать подпапки, пока не введён пароль</span>
        </label>
        <p class="hint">
          Это косметика, а не шифрование: имена папок сервер всё равно знает, они хранятся
          открытыми. Прячет от чужого взгляда через плечо, но не от того, у кого есть база.
        </p>

        <label class="field">
          <span>Удалять файлы этой папки через</span>
          <select v-model.number="autoDelete">
            <option :value="0">никогда</option>
            <option :value="1">день</option>
            <option :value="7">неделю</option>
            <option :value="30">месяц</option>
            <option :value="90">3 месяца</option>
            <option :value="365">год</option>
          </select>
        </label>
        <p class="hint">Срок ставится файлам при загрузке; у уже лежащих он не меняется.</p>
        <p class="warn">
          ⚠️ Пароль хранится только у вас. Забыли — файлы не восстановит никто, включая нас.
        </p>
        <button class="btn primary" :disabled="busy" @click="create">🔐 Создать</button>
      </template>

      <template v-else>
        <div class="pass-block">
          <div class="title">Сменить пароль</div>
          <input
            v-if="!isUnlocked(folder!.id)"
            v-model="oldPassword"
            type="password"
            placeholder="Текущий пароль"
            autocomplete="off"
          />
          <input v-model="password" type="password" placeholder="Новый пароль" autocomplete="new-password" />
          <input v-model="password2" type="password" placeholder="Ещё раз" autocomplete="new-password" />
          <p class="hint">
            Файлы не перешифровываются: меняется только обёртка ключа папки. После смены папку
            нужно открыть новым паролем.
          </p>
        </div>

        <label class="check">
          <input v-model="hideChildren" type="checkbox" />
          <span>Не показывать подпапки, пока не введён пароль</span>
        </label>
        <p class="hint">
          Это косметика, а не шифрование: имена папок сервер всё равно знает, они хранятся
          открытыми. Прячет от чужого взгляда через плечо, но не от того, у кого есть база.
        </p>

        <label class="field">
          <span>Удалять файлы этой папки через</span>
          <select v-model.number="autoDelete">
            <option :value="0">никогда</option>
            <option :value="1">день</option>
            <option :value="7">неделю</option>
            <option :value="30">месяц</option>
            <option :value="90">3 месяца</option>
            <option :value="365">год</option>
          </select>
        </label>
        <p class="hint">Срок ставится файлам при загрузке; у уже лежащих он не меняется.</p>
        <button class="btn primary" :disabled="busy" @click="save">💾 Сохранить</button>

        <ShareBox v-if="folder!.mine" kind="folder" :id="folder!.id" />

        <button v-if="!confirmDelete" class="btn danger" @click="confirmDelete = true">
          🗑 Удалить папку
        </button>
        <button v-else class="btn danger" @click="remove">
          Точно удалить? Файлы внутри пропадут навсегда
        </button>
      </template>

      <button class="btn" @click="emit('close')">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.field {
  display: block;
  text-align: left;
  margin-bottom: 12px;
}

.field span {
  display: block;
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 4px;
}

.field input,
.field select {
  width: 100%;
  padding: 9px 10px;
  border-radius: 8px;
  border: 1px solid var(--border-color, rgba(128, 128, 128, 0.3));
  background: var(--bg-secondary);
  color: var(--text-color);
  font: inherit;
}

.check {
  display: flex;
  align-items: center;
  gap: 8px;
  text-align: left;
  font-size: 14px;
  margin-top: 10px;
}

.check input {
  width: 18px !important;
  height: 18px;
  flex: none;
  margin: 0;
}

.hint {
  margin: 6px 0 0;
  font-size: 11px;
  color: var(--text-secondary);
  text-align: left;
}

.warn {
  margin: 8px 0 0;
  font-size: 12px;
  text-align: left;
  color: #f59e0b;
}

.pass-block {
  margin-top: 14px;
  padding-top: 10px;
  border-top: 1px solid var(--bg-secondary);
  text-align: left;
}

.pass-block .title {
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 6px;
}

.pass-block input {
  width: 100%;
  margin-bottom: 6px;
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
