<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { showToast } from '../../shared/toast'
import * as autoApi from './api'
import AutomationModal from './components/AutomationModal.vue'
import {
  fmtDateTime,
  PAYMENT_LABELS,
  STATUS_LABELS,
  type AgentInfo,
  type Automation,
  type AutomationRun,
  type RunStep,
} from './types'

const items = ref<Automation[]>([])
const loading = ref(true)
const failed = ref(false)
const agent = ref<AgentInfo | null>(null)

async function load() {
  loading.value = true
  failed.value = false
  try {
    const [list, info] = await Promise.all([autoApi.fetchAutomations(), autoApi.fetchAgentInfo()])
    items.value = list.automations
    agent.value = info
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
}

onMounted(load)

// --- агент сетевого выхода ---
const agentPanel = ref(false)
const showToken = ref(false)

function copy(text: string, label: string) {
  navigator.clipboard?.writeText(text).then(
    () => showToast(label + ' скопирован ✅'),
    () => showToast('Не удалось скопировать'),
  )
}

async function regenToken() {
  if (!confirm('Выдать новый токен? Текущий агент перестанет работать, пока не обновите токен на машине.')) return
  try {
    const { token } = await autoApi.regenAgentToken()
    if (agent.value) agent.value.token = token
    showToast('Новый токен выдан')
  } catch {
    showToast('Не удалось обновить токен')
  }
}

const modal = ref(false)
const editing = ref<Automation | null>(null)

function openCreate() {
  editing.value = null
  modal.value = true
}
function openEdit(a: Automation) {
  editing.value = a
  modal.value = true
}
function onSaved() {
  modal.value = false
  load()
}

// --- запуск / пробный прогон ---
const running = ref<number | null>(null)
const runResult = ref<{ title: string; steps: RunStep[]; ok: boolean; error?: string } | null>(null)

async function run(a: Automation, dry: boolean) {
  if (running.value) return
  if (!dry && !confirm(`Оформить реальный заказ воды (${a.config.quantity} бутылей)?`)) return
  running.value = a.id
  try {
    const res = await autoApi.runAutomation(a.id, dry)
    runResult.value = {
      title: (dry ? '🧪 Пробный прогон' : '🚀 Запуск') + ' — ' + a.title,
      steps: res.steps ?? [],
      ok: res.ok,
      error: res.error,
    }
    load()
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось запустить')
  } finally {
    running.value = null
  }
}

// --- история ---
const historyFor = ref<Automation | null>(null)
const runs = ref<AutomationRun[]>([])
const historyLoading = ref(false)

async function openHistory(a: Automation) {
  historyFor.value = a
  historyLoading.value = true
  try {
    runs.value = (await autoApi.fetchRuns(a.id)).runs
  } catch {
    showToast('Не удалось загрузить историю')
  } finally {
    historyLoading.value = false
  }
}

async function toggleEnabled(a: Automation) {
  try {
    await autoApi.updateAutomation(a.id, { enabled: !a.enabled })
    load()
  } catch {
    showToast('Не удалось изменить')
  }
}
</script>

<template>
  <p class="intro">
    🤖 Автоматизированные действия. Первый тип — заказ воды на jur.am по расписанию или вручную.
  </p>

  <!-- статус домашнего агента -->
  <div v-if="agent" class="agent card-glass" :class="{ off: !agent.online }">
    <button class="agent-head" @click="agentPanel = !agentPanel">
      <span class="dot" :class="{ on: agent.online }"></span>
      Домашний агент: <b>{{ agent.online ? 'в сети' : 'не в сети' }}</b>
      <span class="agent-toggle">{{ agentPanel ? '▴' : '▾' }}</span>
    </button>
    <div v-if="agentPanel" class="agent-body">
      <p class="agent-why">
        jur.am за Cloudflare блокирует IP сервера, поэтому заказ выполняется через агент на вашей
        домашней машине (её IP проходит). Пока агент не в сети, запуск невозможен.
      </p>
      <div class="agent-field">
        <span class="af-label">Команда установки на домашней машине (Linux):</span>
        <code class="af-code" @click="copy(agent.install, 'Команда')">{{ agent.install }}</code>
      </div>
      <div class="agent-field">
        <span class="af-label">Токен агента:</span>
        <code class="af-code" @click="copy(agent.token, 'Токен')">
          {{ showToken ? agent.token : '•'.repeat(16) }}
        </code>
        <div class="af-actions">
          <button class="mini-btn" @click="showToken = !showToken">{{ showToken ? 'скрыть' : 'показать' }}</button>
          <button class="mini-btn" @click="regenToken">новый токен</button>
        </div>
      </div>
      <p class="agent-hint">Нажмите на команду или токен, чтобы скопировать. Токен — секрет.</p>
    </div>
  </div>

  <div v-if="loading" class="hint">Загрузка…</div>
  <p v-else-if="failed" class="hint">
    Не удалось загрузить <button class="retry" @click="load">повторить</button>
  </p>

  <template v-else>
    <p v-if="items.length === 0" class="hint">
      Пока нет автоматизаций.<br />Создайте первую 👇
    </p>

    <div v-for="a in items" :key="a.id" class="card card-glass" :class="{ off: !a.enabled }">
      <div class="c-head">
        <button class="c-title" @click="openEdit(a)">
          {{ a.title }}
          <span v-if="!a.enabled" class="badge">выкл</span>
        </button>
        <label class="switch" title="Включена">
          <input type="checkbox" :checked="a.enabled" @change="toggleEnabled(a)" />
        </label>
      </div>

      <div class="c-params">
        <span>💧 {{ a.config.quantity }} бутылей</span>
        <span>♻️ тара: {{ a.config.tare_mode === 'auto' ? 'авто' : a.config.tare_qty }}</span>
        <span>💳 {{ PAYMENT_LABELS[a.config.payment] ?? a.config.payment }}</span>
      </div>
      <div class="c-sched">
        <template v-if="a.next_run_at">
          ⏰ следующий: <b>{{ fmtDateTime(a.next_run_at) }}</b> · каждые {{ a.interval_days }} дн.
        </template>
        <template v-else>⏸ только вручную</template>
      </div>
      <div class="c-last" :class="a.last_status">
        {{ STATUS_LABELS[a.last_status] ?? a.last_status }}
        <span v-if="a.last_run_at" class="c-when">· {{ fmtDateTime(a.last_run_at) }}</span>
      </div>
      <p v-if="!a.has_creds" class="warn">⚠️ Не заданы логин/пароль — откройте и заполните</p>

      <div class="c-actions">
        <button class="act" :disabled="running === a.id" @click="run(a, true)">
          {{ running === a.id ? '…' : '🧪 Пробный прогон' }}
        </button>
        <button class="act primary" :disabled="running === a.id" @click="run(a, false)">
          🚀 Запустить
        </button>
        <button class="act" @click="openHistory(a)">📜 История</button>
      </div>
    </div>

    <button class="add" @click="openCreate">＋ Новая автоматизация</button>
  </template>

  <!-- результат запуска -->
  <div v-if="runResult" class="modal" @click.self="runResult = null">
    <div class="modal-content res-box">
      <h3>{{ runResult.title }}</h3>
      <p class="res-status" :class="{ bad: !runResult.ok }">
        {{ runResult.ok ? '✅ Готово' : '❌ ' + (runResult.error ?? 'Ошибка') }}
      </p>
      <div v-for="(s, i) in runResult.steps" :key="i" class="step" :class="{ bad: !s.ok }">
        <span class="step-mark">{{ s.ok ? '✓' : '✗' }}</span>
        <span class="step-name">{{ s.name }}</span>
        <span v-if="s.detail" class="step-detail">{{ s.detail }}</span>
      </div>
      <button class="btn" @click="runResult = null">Закрыть</button>
    </div>
  </div>

  <!-- история запусков -->
  <div v-if="historyFor" class="modal" @click.self="historyFor = null">
    <div class="modal-content res-box">
      <h3>📜 История — {{ historyFor.title }}</h3>
      <p v-if="historyLoading" class="hint">Загрузка…</p>
      <p v-else-if="runs.length === 0" class="hint">Запусков ещё не было</p>
      <div v-for="r in runs" v-else :key="r.id" class="run">
        <div class="run-head">
          <span :class="'st-' + r.status">
            {{ r.status === 'success' ? '✅' : r.status === 'failed' ? '❌' : '⏳' }}
            {{ r.dry_run ? 'пробный' : r.trigger === 'manual' ? 'вручную' : 'по расписанию' }}
          </span>
          <span class="run-when">{{ fmtDateTime(r.started_at) }}</span>
        </div>
        <div v-for="(s, i) in r.steps" :key="i" class="step small" :class="{ bad: !s.ok }">
          <span class="step-mark">{{ s.ok ? '✓' : '✗' }}</span>
          <span class="step-name">{{ s.name }}</span>
          <span v-if="s.detail" class="step-detail">{{ s.detail }}</span>
        </div>
        <p v-if="r.error" class="run-err">{{ r.error }}</p>
      </div>
      <button class="btn" @click="historyFor = null">Закрыть</button>
    </div>
  </div>

  <AutomationModal v-if="modal" :automation="editing" @saved="onSaved" @close="modal = false" />
</template>

<style scoped>
.intro {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 0 0 12px;
}

.agent {
  background: var(--card-color);
  border-radius: 8px;
  padding: 10px 12px;
  margin-bottom: 12px;
}

.agent.off {
  border: 1px solid #f59e0b55;
}

.agent-head {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  background: none;
  border: none;
  color: var(--text-color);
  font-size: 14px;
  text-align: left;
  padding: 0;
}

.dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: #ef4444;
  flex: none;
}

.dot.on {
  background: #22c55e;
}

.agent-toggle {
  margin-left: auto;
  color: var(--text-secondary);
}

.agent-body {
  margin-top: 8px;
}

.agent-why {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 8px;
}

.agent-field {
  margin-bottom: 8px;
}

.af-label {
  display: block;
  font-size: 11px;
  color: var(--text-secondary);
  margin-bottom: 2px;
}

.af-code {
  display: block;
  background: var(--bg-secondary);
  border-radius: 6px;
  padding: 7px 9px;
  font-size: 11px;
  overflow-wrap: anywhere;
  cursor: pointer;
}

.af-actions {
  display: flex;
  gap: 6px;
  margin-top: 4px;
}

.mini-btn {
  background: none;
  border: 1px solid var(--hover-bg-color);
  border-radius: 6px;
  padding: 3px 8px;
  font-size: 11px;
  color: var(--accent-color);
}

.agent-hint {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 4px 0 0;
}

.card {
  background: var(--card-color);
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 12px;
}

.card.off {
  opacity: 0.65;
}

.c-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
}

.c-title {
  flex: 1;
  min-width: 0;
  text-align: left;
  background: none;
  border: none;
  color: var(--text-color);
  font-size: 16px;
  font-weight: 700;
  overflow-wrap: anywhere;
}

.badge {
  font-size: 11px;
  font-weight: 400;
  color: var(--text-secondary);
  border: 1px solid var(--hover-bg-color);
  border-radius: 8px;
  padding: 1px 6px;
  margin-left: 6px;
}

.switch input {
  width: 40px;
  height: 22px;
}

.c-params {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 6px;
}

.c-sched {
  font-size: 13px;
  margin-top: 6px;
}

.c-last {
  font-size: 12px;
  margin-top: 4px;
  color: var(--text-secondary);
}

.c-last.failed {
  color: #ef4444;
}

.c-when {
  color: var(--text-secondary);
}

.warn {
  font-size: 12px;
  color: #f59e0b;
  margin: 6px 0 0;
}

.c-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 10px;
}

.act {
  flex: 1;
  min-width: 100px;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  padding: 8px;
  font-size: 13px;
  color: var(--text-color);
}

.act.primary {
  background: var(--accent-color);
  color: #fff;
}

.act:disabled {
  opacity: 0.5;
}

.add {
  display: block;
  width: 100%;
  padding: 12px;
  border: 1px dashed var(--hover-bg-color);
  border-radius: 10px;
  background: none;
  color: var(--accent-color);
  font-size: 14px;
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

.res-box {
  text-align: left;
  max-height: 85vh;
  overflow-y: auto;
}

.res-box h3 {
  text-align: center;
}

.res-status {
  text-align: center;
  font-size: 14px;
  margin: 4px 0 10px;
}

.res-status.bad {
  color: #ef4444;
}

.step {
  display: flex;
  gap: 8px;
  align-items: baseline;
  padding: 4px 0;
  font-size: 13px;
  border-top: 1px solid var(--bg-secondary);
}

.step.small {
  font-size: 12px;
}

.step.bad {
  color: #ef4444;
}

.step-mark {
  flex: none;
  font-weight: 700;
}

.step-name {
  flex: none;
  font-weight: 600;
}

.step-detail {
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}

.run {
  border-top: 1px solid var(--bg-secondary);
  padding-top: 8px;
  margin-top: 8px;
}

.run-head {
  display: flex;
  justify-content: space-between;
  font-size: 13px;
  font-weight: 600;
}

.st-failed {
  color: #ef4444;
}

.run-when {
  color: var(--text-secondary);
  font-weight: 400;
}

.run-err {
  font-size: 12px;
  color: #ef4444;
  margin: 4px 0 0;
}

.btn {
  display: block;
  width: 100%;
  margin-top: 12px;
  padding: 10px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
}
</style>
