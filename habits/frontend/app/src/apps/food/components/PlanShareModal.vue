<script setup lang="ts">
import { onMounted, ref } from 'vue'
import RecipientPicker from '../../../components/RecipientPicker.vue'
import { showToast } from '../../../shared/toast'
import * as foodApi from '../api'
import { userLabel, type FoodPlan, type FoodPlanShareUser } from '../types'

// Два способа поделиться планом, как принято в проекте:
//   через контакт — ДОСТУП к этому же плану (можно выдать право правки);
//   по ссылке — независимая КОПИЯ у получателя.
const props = defineProps<{ plan: FoodPlan }>()
const emit = defineEmits<{ close: [] }>()

const users = ref<FoodPlanShareUser[]>([])
const loading = ref(true)
const shareTo = ref('')
const sharing = ref(false)
const confirmRevoke = ref<number | null>(null)

const link = ref('')
const linkBusy = ref(false)

async function load() {
  loading.value = true
  try {
    users.value = (await foodApi.fetchPlanShares(props.plan.id)).users
  } catch {
    showToast('Не удалось загрузить доступы')
  } finally {
    loading.value = false
  }
}

onMounted(load)

async function share() {
  if (!shareTo.value.trim()) return
  sharing.value = true
  try {
    const { queued } = await foodApi.sharePlan(props.plan.id, shareTo.value.trim())
    showToast(queued ? 'Приглашение отправлено — ждёт принятия 📨' : 'Доступ открыт ✅')
    shareTo.value = ''
    load()
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось поделиться')
  } finally {
    sharing.value = false
  }
}

async function toggleEdit(u: FoodPlanShareUser) {
  try {
    await foodApi.updatePlanShare(props.plan.id, u.id, !u.can_edit)
    u.can_edit = !u.can_edit
  } catch {
    showToast('Не удалось изменить')
  }
}

async function revoke(u: FoodPlanShareUser) {
  if (confirmRevoke.value !== u.id) {
    confirmRevoke.value = u.id
    setTimeout(() => (confirmRevoke.value = null), 3000)
    return
  }
  try {
    await foodApi.revokePlanShare(props.plan.id, u.id)
    showToast('Доступ отозван')
    load()
  } catch {
    showToast('Не удалось отозвать')
  }
}

async function makeLink() {
  linkBusy.value = true
  try {
    const res = await foodApi.planShareLink(props.plan.id)
    link.value = res.link || res.token
  } catch {
    showToast('Не удалось создать ссылку')
  } finally {
    linkBusy.value = false
  }
}

async function copyLink() {
  try {
    await navigator.clipboard.writeText(link.value)
    showToast('Ссылка скопирована')
  } catch {
    showToast('Скопируйте ссылку вручную')
  }
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content share">
      <h3>📤 Поделиться планом</h3>
      <p class="sub">{{ plan.name }}</p>

      <section class="sec">
        <p class="sec-title">👥 Совместный доступ</p>
        <p class="hint">Тот же самый план: изменения видят все. Правку выдаёте отдельно.</p>

        <p v-if="loading" class="empty">Загрузка…</p>
        <p v-else-if="users.length === 0" class="empty">Пока никому не открыт.</p>
        <div v-for="u in users" :key="u.id" class="urow">
          <span class="uname">👤 {{ userLabel(u) }}</span>
          <label class="edit-flag">
            <input type="checkbox" :checked="u.can_edit" @change="toggleEdit(u)" />
            может править
          </label>
          <button class="mini" @click="revoke(u)">
            {{ confirmRevoke === u.id ? 'точно?' : '✕' }}
          </button>
        </div>

        <RecipientPicker v-model="shareTo" />
        <button class="btn primary" :disabled="sharing || !shareTo.trim()" @click="share">
          {{ sharing ? '…' : '📤 Открыть доступ' }}
        </button>
      </section>

      <section class="sec">
        <p class="sec-title">🔗 Ссылка-приглашение</p>
        <p class="hint">
          По ссылке получатель заберёт себе <b>копию</b> плана — своя, независимая. Ссылки на ваши
          продукты и рецепты в копию не попадут, но КБЖУ и названия сохранятся.
        </p>
        <button v-if="!link" class="btn" :disabled="linkBusy" @click="makeLink">
          {{ linkBusy ? '…' : '🔗 Получить ссылку' }}
        </button>
        <template v-else>
          <p class="link">{{ link }}</p>
          <button class="btn" @click="copyLink">📋 Скопировать</button>
        </template>
      </section>

      <button class="btn" @click="emit('close')">Закрыть</button>
    </div>
  </div>
</template>

<style scoped>
.share {
  text-align: left;
  max-height: 88vh;
  overflow-y: auto;
}

.share h3 {
  text-align: center;
  margin-bottom: 2px;
}

.sub {
  text-align: center;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 10px;
  overflow-wrap: anywhere;
}

.sec {
  border-top: 1px solid var(--bg-secondary);
  padding-top: 10px;
  margin-top: 10px;
}

.sec-title {
  font-size: 14px;
  font-weight: 600;
  margin: 0 0 2px;
}

.hint {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 0 0 8px;
}

.empty {
  font-size: 13px;
  color: var(--text-secondary);
  text-align: center;
  padding: 6px 0;
}

.urow {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 0;
}

.uname {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  overflow-wrap: anywhere;
}

.edit-flag {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: var(--text-secondary);
  flex: none;
}

.edit-flag input {
  width: auto;
  margin: 0;
}

.mini {
  background: none;
  border: none;
  color: var(--text-secondary);
  padding: 4px 6px;
  flex: none;
}

.link {
  font-size: 12px;
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 8px;
  overflow-wrap: anywhere;
  margin: 0;
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

.btn:disabled {
  opacity: 0.5;
}
</style>
