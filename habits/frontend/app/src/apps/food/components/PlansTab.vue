<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { showToast } from '../../../shared/toast'
import * as foodApi from '../api'
import PlanEditor from './PlanEditor.vue'
import { fmtDay, r0, todayStr, type FoodPlanCard } from '../types'

// Вкладка «План»: список планов (свои и открытые мне) → редактор одного плана.
const plans = ref<FoodPlanCard[]>([])
const loading = ref(true)
const failed = ref(false)
const archived = ref(false)
const openId = ref<number | null>(null)

async function load() {
  loading.value = true
  failed.value = false
  try {
    plans.value = (await foodApi.fetchPlans(archived.value)).plans
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
}

onMounted(load)

const mine = computed(() => plans.value.filter((p) => p.is_owner))
const shared = computed(() => plans.value.filter((p) => !p.is_owner))

// --- создание ---
const creating = ref(false)
const draft = ref({ name: '', days: 14, start_date: todayStr() })
const saving = ref(false)

async function create() {
  if (!draft.value.name.trim()) {
    showToast('Укажите название плана')
    return
  }
  saving.value = true
  try {
    const { plan } = await foodApi.createPlan({ ...draft.value })
    creating.value = false
    draft.value = { name: '', days: 14, start_date: todayStr() }
    await load()
    openId.value = plan.id
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось создать план')
  } finally {
    saving.value = false
  }
}

const confirmLeave = ref<number | null>(null)

async function leave(p: FoodPlanCard) {
  if (confirmLeave.value !== p.id) {
    confirmLeave.value = p.id
    setTimeout(() => (confirmLeave.value = null), 3000)
    return
  }
  try {
    await foodApi.leavePlan(p.id)
    showToast('План убран')
    load()
  } catch {
    showToast('Не удалось убрать')
  }
}

function period(p: FoodPlanCard): string {
  const d = `${p.days} дн.`
  return p.start_date ? `${d} · с ${fmtDay(p.start_date)}` : `${d} · без даты`
}
</script>

<template>
  <PlanEditor
    v-if="openId !== null"
    :key="openId"
    :plan-id="openId"
    @back="openId = null"
    @changed="load"
  />

  <template v-else>
    <div v-if="loading" class="hint">Загрузка…</div>
    <p v-else-if="failed" class="hint">
      Не удалось загрузить <button class="retry" @click="load">повторить</button>
    </p>

    <template v-else>
      <section class="sec card-glass">
        <div class="sec-head">
          <h3>📅 Мои планы</h3>
          <label class="arch">
            <input v-model="archived" type="checkbox" @change="load" />
            архив
          </label>
        </div>

        <p v-if="mine.length === 0" class="empty">
          {{ archived ? 'В архиве пусто.' : 'Планов пока нет. Составьте меню на неделю или две — приблизительно тоже можно.' }}
        </p>

        <button v-for="p in mine" :key="p.id" class="plan" @click="openId = p.id">
          <span class="p-name">{{ p.name }}</span>
          <span class="p-meta">
            {{ period(p) }}
            <template v-if="p.participants"> · 👥 {{ p.participants }}</template>
            <!-- у плана с участниками осмысленны итоги каждого, а не общая
                 средняя — её показываем только для плана на одного -->
            <template v-if="!p.participants && p.avg_calories > 0">
              · ≈{{ r0(p.avg_calories) }} ккал/день
            </template>
          </span>
          <span v-if="p.description" class="p-desc">{{ p.description }}</span>
        </button>

        <button v-if="!creating" class="add-btn" @click="creating = true">＋ Новый план</button>
        <div v-else class="create">
          <input v-model="draft.name" maxlength="200" placeholder="Название: например, «Меню на 2 недели»" />
          <div class="row2">
            <label>
              <span>Дней</span>
              <input v-model.number="draft.days" type="number" min="1" max="90" />
            </label>
            <label>
              <span>С какой даты</span>
              <input v-model="draft.start_date" type="date" />
            </label>
          </div>
          <p class="hint-s">Дату можно очистить — тогда план будет шаблонным, на любые даты.</p>
          <button class="btn primary" :disabled="saving" @click="create">
            {{ saving ? '…' : '💾 Создать' }}
          </button>
          <button class="btn" @click="creating = false">Отмена</button>
        </div>
      </section>

      <section v-if="shared.length" class="sec card-glass">
        <h3>📥 Открыты мне</h3>
        <div v-for="p in shared" :key="p.id" class="srow">
          <button class="plan flat" @click="openId = p.id">
            <span class="p-name">{{ p.name }}</span>
            <span class="p-meta">
              от {{ p.owner_name || 'другого пользователя' }} · {{ period(p) }}
              <template v-if="p.can_edit"> · можно править</template>
            </span>
          </button>
          <button class="mini" @click="leave(p)">{{ confirmLeave === p.id ? 'точно?' : '✕' }}</button>
        </div>
      </section>
    </template>
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

.sec-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.arch {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: var(--text-secondary);
}

.arch input {
  width: auto;
  margin: 0;
}

.plan {
  display: block;
  width: 100%;
  text-align: left;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  padding: 9px 11px;
  margin-bottom: 6px;
  color: var(--text-color);
}

.plan.flat {
  flex: 1;
  min-width: 0;
  margin-bottom: 0;
}

.p-name {
  display: block;
  font-size: 14px;
  font-weight: 600;
  overflow-wrap: anywhere;
}

.p-meta {
  display: block;
  font-size: 11px;
  color: var(--text-secondary);
  margin-top: 1px;
}

.p-desc {
  display: block;
  font-size: 11px;
  color: var(--text-secondary);
  margin-top: 3px;
  overflow-wrap: anywhere;
}

.srow {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 6px;
}

.mini {
  background: none;
  border: none;
  color: var(--text-secondary);
  padding: 4px 6px;
  flex: none;
}

.add-btn {
  display: block;
  width: 100%;
  margin-top: 4px;
  padding: 9px;
  border: 1px dashed var(--hover-bg-color);
  border-radius: 8px;
  background: none;
  color: var(--accent-color);
  font-size: 13px;
}

.create {
  border-top: 1px solid var(--bg-secondary);
  margin-top: 8px;
  padding-top: 8px;
}

.create input {
  width: 100%;
}

.row2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  margin-top: 8px;
}

.row2 span {
  display: block;
  font-size: 11px;
  color: var(--text-secondary);
  margin-bottom: 2px;
}

.hint-s {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 6px 0 0;
}

.empty {
  font-size: 13px;
  color: var(--text-secondary);
  text-align: center;
  padding: 8px 0;
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
