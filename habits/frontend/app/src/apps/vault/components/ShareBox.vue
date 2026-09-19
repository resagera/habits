<script setup lang="ts">
/**
 * Выдача доступа к папке или файлу. Получателю уезжают те же шифроданные —
 * открыть он сможет, только зная пароль папки, поэтому текст об этом прямо
 * говорит: сервер пароля не знает и передать его не может.
 */
import { onMounted, ref } from 'vue'
import RecipientPicker from '../../../components/RecipientPicker.vue'
import { showToast } from '../../../shared/toast'
import * as vaultApi from '../api'
import type { ShareUser } from '../types'

const props = defineProps<{ kind: 'folder' | 'file'; id: number }>()

const to = ref('')
const members = ref<ShareUser[]>([])
const busy = ref(false)

onMounted(async () => {
  try {
    members.value = (await vaultApi.fetchShares(props.kind, props.id)).users
  } catch {
    /* список просто не покажем */
  }
})

async function share() {
  const value = to.value.trim()
  if (!value || busy.value) return
  busy.value = true
  try {
    const { shared_with } = await vaultApi.shareTarget(props.kind, props.id, value)
    if (!members.value.some((u) => u.id === shared_with.id)) members.value.push(shared_with)
    to.value = ''
    showToast('Доступ открыт. Пароль передайте сами — мы его не знаем')
  } catch {
    showToast('Не удалось поделиться')
  } finally {
    busy.value = false
  }
}

async function revoke(u: ShareUser) {
  try {
    await vaultApi.revokeShare(props.kind, props.id, u.id)
    members.value = members.value.filter((m) => m.id !== u.id)
  } catch {
    showToast('Не удалось отозвать доступ')
  }
}

function label(u: ShareUser): string {
  return u.first_name || (u.username ? '@' + u.username : `#${u.id}`)
}
</script>

<template>
  <div class="share">
    <div class="title">Доступ</div>
    <div v-if="members.length" class="members">
      <span v-for="u in members" :key="u.id" class="chip">
        {{ label(u) }}
        <button class="x" title="Отозвать доступ" @click="revoke(u)">✕</button>
      </span>
    </div>
    <RecipientPicker v-model="to" />
    <button class="btn small" :disabled="!to.trim() || busy" @click="share">👥 Поделиться</button>
    <p class="hint">
      Получатель увидит те же зашифрованные данные. Открыть сможет, только зная пароль папки —
      передайте его сами, сервер пароля не знает.
    </p>
  </div>
</template>

<style scoped>
.share {
  margin-top: 14px;
  padding-top: 10px;
  border-top: 1px solid var(--bg-secondary);
  text-align: left;
}

.title {
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 6px;
}

.members {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 6px;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: var(--bg-secondary);
  border-radius: 12px;
  padding: 4px 6px 4px 11px;
  font-size: 12px;
}

.x {
  background: none;
  border: none;
  padding: 0 4px;
  font-size: 11px;
  color: var(--text-secondary);
}

.btn {
  display: block;
  width: 100%;
  margin-top: 8px;
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

.hint {
  margin: 6px 0 0;
  font-size: 11px;
  color: var(--text-secondary);
}
</style>
