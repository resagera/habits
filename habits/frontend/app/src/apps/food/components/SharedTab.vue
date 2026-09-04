<script setup lang="ts">
import { onMounted, ref } from 'vue'
import RecipientPicker from '../../../components/RecipientPicker.vue'
import { showToast } from '../../../shared/toast'
import * as foodApi from '../api'
import {
  addDays,
  assetUrl,
  fmtDay,
  MEAL_TYPE_LABELS,
  r0,
  r1,
  todayStr,
  userLabel,
  type FoodDiary,
  type FoodShareUser,
} from '../types'

// Вкладка «Общие»: кому доступен мой дневник (+флаги) и чьи дневники доступны мне.
const myShares = ref<FoodShareUser[]>([])
const sharedWithMe = ref<FoodShareUser[]>([])
const loading = ref(true)
const failed = ref(false)

async function load() {
  loading.value = true
  failed.value = false
  try {
    const [mine, withMe] = await Promise.all([foodApi.fetchShares(), foodApi.fetchSharedWithMe()])
    myShares.value = mine.users
    sharedWithMe.value = withMe.owners
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
}

onMounted(load)

// --- выдача доступа ---
const shareTo = ref('')
const sharing = ref(false)

async function share() {
  if (!shareTo.value.trim()) return
  sharing.value = true
  try {
    const { queued } = await foodApi.shareDiary(shareTo.value.trim())
    showToast(queued ? 'Приглашение отправлено — ждёт принятия 📨' : 'Доступ открыт ✅')
    shareTo.value = ''
    load()
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось поделиться')
  } finally {
    sharing.value = false
  }
}

const FLAGS: { key: keyof FoodShareUser; label: string }[] = [
  { key: 'show_goals', label: 'Цели' },
  { key: 'show_photos', label: 'Фото' },
  { key: 'show_notes', label: 'Описания' },
  { key: 'show_weight', label: 'Вес' },
]

async function toggleFlag(u: FoodShareUser, key: keyof FoodShareUser) {
  const updated = { ...u, [key]: !u[key] }
  try {
    await foodApi.updateShare(u.id, {
      show_weight: updated.show_weight as boolean,
      show_goals: updated.show_goals as boolean,
      show_photos: updated.show_photos as boolean,
      show_notes: updated.show_notes as boolean,
    })
    Object.assign(u, updated)
  } catch {
    showToast('Не удалось изменить')
  }
}

const confirmRevoke = ref<number | null>(null)

async function revoke(u: FoodShareUser) {
  if (confirmRevoke.value !== u.id) {
    confirmRevoke.value = u.id
    setTimeout(() => (confirmRevoke.value = null), 3000)
    return
  }
  try {
    await foodApi.revokeShare(u.id)
    showToast('Доступ отозван')
    load()
  } catch {
    showToast('Не удалось отозвать')
  }
}

const confirmLeave = ref<number | null>(null)

async function leave(u: FoodShareUser) {
  if (confirmLeave.value !== u.id) {
    confirmLeave.value = u.id
    setTimeout(() => (confirmLeave.value = null), 3000)
    return
  }
  try {
    await foodApi.leaveShared(u.id)
    if (viewing.value?.id === u.id) viewing.value = null
    showToast('Дневник убран')
    load()
  } catch {
    showToast('Не удалось убрать')
  }
}

// --- просмотр чужого дневника ---
const viewing = ref<FoodShareUser | null>(null)
const viewDay = ref(todayStr())
const viewDiary = ref<FoodDiary | null>(null)
const viewLoading = ref(false)

async function openDiary(u: FoodShareUser) {
  viewing.value = u
  viewDay.value = todayStr()
  await loadView()
}

async function loadView() {
  const u = viewing.value
  if (!u) return
  viewLoading.value = true
  try {
    viewDiary.value = await foodApi.fetchSharedDiary(u.id, viewDay.value)
  } catch {
    showToast('Не удалось загрузить дневник')
    viewDiary.value = null
  } finally {
    viewLoading.value = false
  }
}

function viewShift(n: number) {
  viewDay.value = addDays(viewDay.value, n)
  loadView()
}
</script>

<template>
  <div v-if="loading" class="hint">Загрузка…</div>
  <p v-else-if="failed" class="hint">
    Не удалось загрузить <button class="retry" @click="load">повторить</button>
  </p>

  <template v-else>
    <!-- доступны мне -->
    <section class="sec card-glass">
      <h3>📥 Доступны мне</h3>
      <p v-if="sharedWithMe.length === 0" class="empty">Никто пока не поделился с вами дневником.</p>
      <div v-for="u in sharedWithMe" :key="u.id" class="srow">
        <button class="srow-name" @click="openDiary(u)">👤 {{ userLabel(u) }}</button>
        <button class="mini" @click="leave(u)">{{ confirmLeave === u.id ? 'точно?' : '✕' }}</button>
      </div>
    </section>

    <!-- просмотр чужого дневника -->
    <section v-if="viewing" class="sec card-glass view">
      <div class="view-head">
        <b>Дневник: {{ userLabel(viewing) }}</b>
        <button class="mini" @click="viewing = null; viewDiary = null">✕</button>
      </div>
      <div class="date-nav">
        <button class="nav-btn" @click="viewShift(-1)">‹</button>
        <span class="date-lbl">{{ viewDay === todayStr() ? 'Сегодня' : fmtDay(viewDay) }}</span>
        <button class="nav-btn" @click="viewShift(1)">›</button>
      </div>
      <p v-if="viewLoading" class="empty">Загрузка…</p>
      <template v-else-if="viewDiary">
        <p class="vsum">
          <b>{{ r0(viewDiary.summary.calories) }}</b>
          <template v-if="viewDiary.goal"> / {{ r0(viewDiary.goal.calories) }}</template> ккал ·
          Б {{ r1(viewDiary.summary.protein) }} · Ж {{ r1(viewDiary.summary.fat) }} · У {{ r1(viewDiary.summary.carbs) }}
          <span v-if="viewDiary.weight_kg" class="vweight">· вес {{ viewDiary.weight_kg }} кг</span>
        </p>
        <p v-if="viewDiary.meals.length === 0" class="empty">Записей нет</p>
        <div v-for="m in viewDiary.meals" :key="m.id" class="vmeal">
          <img v-if="m.photo" :src="assetUrl(m.photo)" class="vphoto" loading="lazy" alt="" />
          <div class="vbody">
            <div class="vtitle">
              <span class="vtype">{{ MEAL_TYPE_LABELS[m.meal_type] }}</span>
              <span v-if="m.time" class="vtime">{{ m.time }}</span>
              {{ m.name || 'Без названия' }}
            </div>
            <div v-if="m.description" class="vdesc">{{ m.description }}</div>
            <div class="vkbju">{{ r0(m.calories) }} ккал · Б {{ r1(m.protein) }} · Ж {{ r1(m.fat) }} · У {{ r1(m.carbs) }}</div>
          </div>
        </div>
      </template>
    </section>

    <!-- мой дневник доступен -->
    <section class="sec card-glass">
      <h3>📤 Мой дневник доступен</h3>
      <p v-if="myShares.length === 0" class="empty">Вы ещё никому не открывали дневник.</p>
      <div v-for="u in myShares" :key="u.id" class="myshare">
        <div class="srow">
          <span class="srow-name plain">👤 {{ userLabel(u) }}</span>
          <button class="mini" @click="revoke(u)">{{ confirmRevoke === u.id ? 'точно?' : '✕' }}</button>
        </div>
        <div class="flags">
          <label v-for="f in FLAGS" :key="f.key" class="flag">
            <input type="checkbox" :checked="Boolean(u[f.key])" @change="toggleFlag(u, f.key)" />
            {{ f.label }}
          </label>
        </div>
      </div>

      <div class="grant">
        <RecipientPicker v-model="shareTo" />
        <button class="btn primary" :disabled="sharing || !shareTo.trim()" @click="share">
          {{ sharing ? '…' : '📤 Открыть доступ (только чтение)' }}
        </button>
      </div>
    </section>
  </template>
</template>

<style scoped>
.sec {
  background: var(--card-color);
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 12px;
}

.sec h3 {
  margin: 0 0 8px;
  font-size: 15px;
}

.srow {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 4px 0;
}

.srow-name {
  background: none;
  border: none;
  color: var(--accent-color);
  font-size: 14px;
  text-align: left;
  padding: 4px 0;
  flex: 1;
  min-width: 0;
  overflow-wrap: anywhere;
}

.srow-name.plain {
  color: var(--text-color);
}

.mini {
  background: none;
  border: none;
  color: var(--text-secondary);
  padding: 4px 6px;
  flex: none;
}

.myshare {
  border-top: 1px solid var(--bg-secondary);
  padding-top: 6px;
  margin-top: 6px;
}

.flags {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 2px 0 4px;
}

.flag {
  display: flex;
  align-items: center;
  gap: 4px;
}

.flag input {
  width: auto;
  margin: 0;
}

.grant {
  border-top: 1px solid var(--bg-secondary);
  margin-top: 10px;
  padding-top: 10px;
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

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn:disabled {
  opacity: 0.5;
}

.view-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.date-nav {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 8px 0;
}

.nav-btn {
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  padding: 4px 12px;
  color: var(--text-color);
}

.date-lbl {
  flex: 1;
  text-align: center;
  font-size: 14px;
}

.vsum {
  font-size: 13px;
  margin: 6px 0;
}

.vweight {
  color: var(--text-secondary);
}

.vmeal {
  display: flex;
  gap: 8px;
  border-top: 1px solid var(--bg-secondary);
  padding: 6px 0;
}

.vphoto {
  width: 44px;
  height: 44px;
  object-fit: cover;
  border-radius: 8px;
  flex: none;
}

.vbody {
  min-width: 0;
}

.vtitle {
  font-size: 13px;
  font-weight: 600;
  overflow-wrap: anywhere;
}

.vtype {
  font-weight: 400;
  font-size: 11px;
  color: var(--text-secondary);
  margin-right: 4px;
}

.vtime {
  font-weight: 400;
  font-size: 11px;
  color: var(--text-secondary);
  margin-right: 4px;
}

.vdesc {
  font-size: 12px;
  color: var(--text-secondary);
}

.vkbju {
  font-size: 12px;
}

.empty {
  font-size: 13px;
  color: var(--text-secondary);
  text-align: center;
  padding: 8px 0;
}

.hint {
  text-align: center;
  color: var(--text-secondary);
  padding: 20px 0;
}

.retry {
  background: none;
  border: none;
  color: var(--accent-color);
  text-decoration: underline;
}
</style>
