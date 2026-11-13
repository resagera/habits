<script setup lang="ts">
import { ref } from 'vue'
import { showToast } from '../../../shared/toast'
import * as foodApi from '../api'
import { r0, type FoodPlan, type FoodPlanParticipant } from '../types'

// Участники плана. Это именованные лица (жена, ребёнок) — быть пользователем
// Habits не обязательно. Коэффициент порции масштабирует ОБЩИЕ приёмы пищи.
const props = defineProps<{ plan: FoodPlan }>()
const emit = defineEmits<{ changed: []; close: [] }>()

const list = ref<FoodPlanParticipant[]>(props.plan.participants.map((p) => ({ ...p })))
const busy = ref(false)
const confirmDel = ref<number | null>(null)
// открытая строка «привязать к пользователю Habits»
const linkFor = ref<number | null>(null)
const linkTo = ref('')

const draft = ref({ name: '', emoji: '', portion_coef: 1, calories_target: 0 })

/** Перечитываем план: is_me и подпись пользователя считает сервер. */
async function reload() {
  try {
    list.value = (await foodApi.fetchPlan(props.plan.id)).plan.participants
  } catch {
    /* список останется прежним — не критично */
  }
  emit('changed')
}

async function add() {
  if (!draft.value.name.trim()) {
    showToast('Укажите имя участника')
    return
  }
  busy.value = true
  try {
    await foodApi.createParticipant(props.plan.id, { ...draft.value })
    draft.value = { name: '', emoji: '', portion_coef: 1, calories_target: 0 }
    await reload()
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось добавить')
  } finally {
    busy.value = false
  }
}

async function save(p: FoodPlanParticipant) {
  try {
    await foodApi.updateParticipant(props.plan.id, p.id, {
      name: p.name,
      emoji: p.emoji,
      portion_coef: p.portion_coef,
      calories_target: p.calories_target,
    })
    emit('changed')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось сохранить')
  }
}

function openLink(p: FoodPlanParticipant) {
  linkFor.value = linkFor.value === p.id ? null : p.id
  linkTo.value = ''
}

/** Привязка участника к аккаунту Habits: `user` — id или @username. */
async function link(p: FoodPlanParticipant, value: string) {
  busy.value = true
  try {
    await foodApi.updateParticipant(props.plan.id, p.id, { user: value })
    linkFor.value = null
    await reload()
    showToast(value ? 'Участник привязан' : 'Привязка снята')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось привязать')
  } finally {
    busy.value = false
  }
}

async function remove(p: FoodPlanParticipant) {
  if (confirmDel.value !== p.id) {
    confirmDel.value = p.id
    setTimeout(() => (confirmDel.value = null), 3000)
    return
  }
  try {
    await foodApi.deleteParticipant(props.plan.id, p.id)
    list.value = list.value.filter((x) => x.id !== p.id)
    showToast('Участник и его личные приёмы удалены')
    emit('changed')
  } catch {
    showToast('Не удалось удалить')
  }
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content parts">
      <h3>👥 Участники плана</h3>
      <p class="hint">
        Общий приём пищи входит в план каждого участника с его коэффициентом порции: 1 — как
        в рецепте, 0.5 — половина.
      </p>

      <p v-if="list.length === 0" class="empty">
        Участников нет — план считается на одного человека.
      </p>

      <div v-for="p in list" :key="p.id" class="part">
        <div class="prow">
          <input v-model="p.emoji" class="emoji" maxlength="4" placeholder="🙂" @change="save(p)" />
          <input v-model="p.name" class="pname" maxlength="100" @change="save(p)" />
          <button class="mini" @click="remove(p)">{{ confirmDel === p.id ? 'точно?' : '✕' }}</button>
        </div>
        <div class="prow2">
          <label>
            <span>Порция ×</span>
            <input
              v-model.number="p.portion_coef"
              type="number"
              min="0.1"
              max="10"
              step="0.1"
              @change="save(p)"
            />
          </label>
          <label>
            <span>Цель, ккал/день</span>
            <input
              v-model.number="p.calories_target"
              type="number"
              min="0"
              step="50"
              @change="save(p)"
            />
          </label>
          <span class="ptarget">
            {{ p.calories_target > 0 ? `цель ${r0(p.calories_target)}` : 'без цели' }}
          </span>
        </div>

        <div class="link-row">
          <span v-if="p.user_id" class="linked">
            🔗 {{ p.user_label || 'пользователь Habits' }}
            <span v-if="p.is_me" class="me">— это вы</span>
          </span>
          <span v-else class="unlinked">Без аккаунта Habits</span>
          <button class="link-btn" @click="openLink(p)">
            {{ linkFor === p.id ? 'отмена' : p.user_id ? 'изменить' : 'привязать' }}
          </button>
        </div>
        <div v-if="linkFor === p.id" class="link-form">
          <input v-model="linkTo" placeholder="@username или id" />
          <button class="act primary" :disabled="busy || !linkTo.trim()" @click="link(p, linkTo.trim())">
            ОК
          </button>
          <button v-if="p.user_id" class="act" :disabled="busy" @click="link(p, '')">Отвязать</button>
        </div>
        <p v-if="linkFor === p.id" class="link-hint">
          Привязка нужна, чтобы этот человек открыл план у себя и перенёс свои порции в свой
          дневник. Доступ к плану выдаётся отдельно — кнопкой «Поделиться».
        </p>
      </div>

      <div class="add">
        <p class="sec-title">Добавить участника</p>
        <div class="prow">
          <input v-model="draft.emoji" class="emoji" maxlength="4" placeholder="🧒" />
          <input v-model="draft.name" class="pname" maxlength="100" placeholder="Имя" />
        </div>
        <div class="prow2">
          <label>
            <span>Порция ×</span>
            <input v-model.number="draft.portion_coef" type="number" min="0.1" max="10" step="0.1" />
          </label>
          <label>
            <span>Цель, ккал/день</span>
            <input v-model.number="draft.calories_target" type="number" min="0" step="50" />
          </label>
        </div>
        <button class="btn primary" :disabled="busy" @click="add">＋ Добавить</button>
      </div>

      <button class="btn" @click="emit('close')">Закрыть</button>
    </div>
  </div>
</template>

<style scoped>
.parts {
  text-align: left;
  max-height: 88vh;
  overflow-y: auto;
}

.parts h3 {
  text-align: center;
}

.hint {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 4px 0 10px;
}

.empty {
  font-size: 13px;
  color: var(--text-secondary);
  text-align: center;
  padding: 6px 0;
}

.part {
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 8px;
  margin-bottom: 8px;
}

.prow {
  display: flex;
  align-items: center;
  gap: 6px;
}

.emoji {
  width: 48px;
  text-align: center;
  flex: none;
  padding: 6px 2px;
}

.pname {
  flex: 1;
  min-width: 0;
  padding: 6px 8px;
}

.mini {
  background: none;
  border: none;
  color: var(--text-secondary);
  padding: 4px 6px;
  flex: none;
}

.prow2 {
  display: flex;
  align-items: flex-end;
  gap: 8px;
  margin-top: 6px;
}

.prow2 label {
  flex: 1;
  min-width: 0;
}

.prow2 span {
  display: block;
  font-size: 10px;
  color: var(--text-secondary);
}

.prow2 input {
  width: 100%;
  padding: 6px;
}

.ptarget {
  font-size: 11px;
  color: var(--text-secondary);
  padding-bottom: 8px;
  flex: none;
}

.link-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 6px;
  font-size: 11px;
}

.linked {
  flex: 1;
  min-width: 0;
  overflow-wrap: anywhere;
}

.me {
  color: var(--accent-color);
}

.unlinked {
  flex: 1;
  color: var(--text-secondary);
}

.link-btn {
  flex: none;
  background: none;
  border: none;
  color: var(--accent-color);
  font-size: 11px;
  text-decoration: underline;
  padding: 2px 4px;
}

.link-form {
  display: flex;
  gap: 6px;
  margin-top: 6px;
}

.link-form input {
  flex: 1;
  min-width: 0;
  padding: 6px 8px;
}

.act {
  flex: none;
  background: var(--bg-secondary);
  border: 1px solid var(--hover-bg-color);
  border-radius: 8px;
  padding: 6px 10px;
  font-size: 12px;
  color: var(--text-color);
}

.act.primary {
  background: var(--accent-color);
  border-color: var(--accent-color);
  color: #fff;
}

.act:disabled {
  opacity: 0.5;
}

.link-hint {
  font-size: 10px;
  color: var(--text-secondary);
  margin: 6px 0 0;
}

.add {
  border-top: 1px solid var(--bg-secondary);
  margin-top: 10px;
  padding-top: 8px;
}

.sec-title {
  font-size: 13px;
  font-weight: 600;
  margin: 0 0 6px;
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
