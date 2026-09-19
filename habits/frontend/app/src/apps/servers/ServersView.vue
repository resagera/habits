<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { showToast } from '../../shared/toast'
import * as srvApi from './api'
import ComboChart from './components/ComboChart.vue'
import Sparkline from './components/Sparkline.vue'
import {
  defaultAlerts,
  fmtBytes,
  fmtUptime,
  isOnline,
  minutesSinceOk,
  type ServerAlerts,
  type ServerEntry,
  type ServerSample,
} from './types'

const servers = ref<ServerEntry[]>([])
const history = ref(new Map<number, ServerSample[]>())
const loading = ref(true)

// компактный режим — клиентская настройка (в localStorage)
const compact = ref(localStorage.getItem('servers_compact') === '1')
function toggleCompact() {
  compact.value = !compact.value
  localStorage.setItem('servers_compact', compact.value ? '1' : '0')
}

const modal = ref(false)
const editing = ref<ServerEntry | null>(null)
const form = ref({
  kind: 'pull' as 'pull' | 'push',
  name: '',
  url: '',
  token: '',
  alerts: defaultAlerts() as ServerAlerts,
})
const confirmDelete = ref(false)
const refreshing = ref(new Set<number>())

const INSTALL_CMD_PREFIX =
  'curl -fsSL https://raw.githubusercontent.com/resagera/habits-agent/main/install-home.sh | sudo bash -s -- '

function installCmd(s: ServerEntry): string {
  return INSTALL_CMD_PREFIX + (s.push_token ?? '')
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    showToast('Скопировано')
  } catch {
    showToast('Не удалось скопировать')
  }
}

let timer: ReturnType<typeof setInterval> | undefined

async function loadHistory(id: number) {
  try {
    const { samples } = await srvApi.fetchHistory(id, 24)
    history.value.set(id, samples)
    history.value = new Map(history.value)
  } catch {
    /* график просто не покажем */
  }
}

async function loadAll() {
  try {
    servers.value = (await srvApi.fetchServers()).servers
    await Promise.all(servers.value.map((s) => loadHistory(s.id)))
  } catch {
    showToast('Не удалось загрузить серверы')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  loadAll()
  // поллер на бэке пишет раз в минуту — обновляем карточки в том же темпе
  timer = setInterval(async () => {
    try {
      servers.value = (await srvApi.fetchServers()).servers
      await Promise.all(servers.value.map((s) => loadHistory(s.id)))
    } catch {
      /* тихо, обновимся в следующий раз */
    }
  }, 60_000)
})
onUnmounted(() => clearInterval(timer))

function openCreate() {
  editing.value = null
  form.value = { kind: 'pull', name: '', url: '', token: '', alerts: defaultAlerts() }
  confirmDelete.value = false
  modal.value = true
}

function openEdit(s: ServerEntry) {
  editing.value = s
  form.value = {
    kind: s.kind,
    name: s.name,
    url: s.url,
    token: s.token ?? '',
    alerts: s.alerts ? { ...s.alerts } : defaultAlerts(),
  }
  confirmDelete.value = false
  modal.value = true
}

async function save() {
  const { kind, name, url, token, alerts } = form.value
  if (!name.trim() || (kind === 'pull' && !url.trim())) {
    showToast(kind === 'pull' ? 'Имя и адрес обязательны' : 'Имя обязательно')
    return
  }
  try {
    if (editing.value) {
      const { server } = await srvApi.updateServer(editing.value.id, kind, name.trim(), url.trim(), token.trim(), alerts)
      const i = servers.value.findIndex((x) => x.id === server.id)
      if (i >= 0) servers.value[i] = server
      modal.value = false
    } else {
      const { server } = await srvApi.createServer(kind, name.trim(), url.trim(), token.trim())
      servers.value.push(server)
      loadHistory(server.id)
      if (server.kind === 'push') {
        // сразу показываем токен и команду установки
        editing.value = server
        form.value = {
          kind: 'push',
          name: server.name,
          url: '',
          token: '',
          alerts: server.alerts ? { ...server.alerts } : defaultAlerts(),
        }
      } else {
        modal.value = false
      }
    }
  } catch {
    showToast('Не удалось сохранить (проверьте адрес)')
  }
}

async function remove() {
  if (!editing.value) return
  if (!confirmDelete.value) {
    confirmDelete.value = true
    setTimeout(() => (confirmDelete.value = false), 3500)
    return
  }
  try {
    await srvApi.deleteServer(editing.value.id)
    servers.value = servers.value.filter((x) => x.id !== editing.value!.id)
    modal.value = false
  } catch {
    showToast('Не удалось удалить')
  }
}

async function refresh(s: ServerEntry) {
  refreshing.value.add(s.id)
  refreshing.value = new Set(refreshing.value)
  try {
    const { server } = await srvApi.refreshServer(s.id)
    const i = servers.value.findIndex((x) => x.id === server.id)
    if (i >= 0) servers.value[i] = server
    loadHistory(s.id)
  } catch {
    showToast('Не удалось обновить')
  } finally {
    refreshing.value.delete(s.id)
    refreshing.value = new Set(refreshing.value)
  }
}

function cpuSeries(id: number): number[] {
  return (history.value.get(id) ?? []).map((s) => s.cpu_pct)
}

function ramSeries(id: number): number[] {
  return (history.value.get(id) ?? []).map((s) => s.ram_used)
}

function ramMax(id: number): number {
  const h = history.value.get(id) ?? []
  return h.length ? h[h.length - 1].ram_total : 0
}

function diskPct(total: number, free: number): number {
  return total > 0 ? Math.round(((total - free) / total) * 100) : 0
}

// --- пороги по конкретным дискам ---
// Список дисков в модалке: те, что сейчас в отчёте агента + те, для которых уже
// есть правило (даже если диск временно пропал).
function serverDisks(): { mount: string; free: number | null }[] {
  const list: { mount: string; free: number | null }[] = []
  const seen = new Set<string>()
  for (const d of editing.value?.last_data?.disks ?? []) {
    list.push({ mount: d.mount, free: d.free })
    seen.add(d.mount)
  }
  for (const r of form.value.alerts.disk_rules) {
    if (!seen.has(r.mount)) {
      list.push({ mount: r.mount, free: null })
      seen.add(r.mount)
    }
  }
  return list
}

function ruleIndex(mount: string): number {
  return form.value.alerts.disk_rules.findIndex((r) => r.mount === mount)
}

function isMonitored(mount: string): boolean {
  return ruleIndex(mount) >= 0
}

function toggleMount(mount: string) {
  const i = ruleIndex(mount)
  if (i >= 0) form.value.alerts.disk_rules.splice(i, 1)
  else form.value.alerts.disk_rules.push({ mount, min_free_mb: form.value.alerts.disk_min_free_mb || 1024 })
}

function ruleGb(mount: string): number {
  const r = form.value.alerts.disk_rules[ruleIndex(mount)]
  return r ? Math.round((r.min_free_mb / 1024) * 100) / 100 : 1
}

function setRuleGb(mount: string, v: number | string) {
  const r = form.value.alerts.disk_rules[ruleIndex(mount)]
  if (r) r.min_free_mb = Math.max(1, Math.round((Number(v) || 0) * 1024))
}
</script>

<template>
  <div v-if="loading" class="hint">Загрузка…</div>

  <template v-else>
    <div v-if="servers.length" class="toolbar">
      <button class="compact-toggle" :class="{ on: compact }" @click="toggleCompact">
        {{ compact ? '◱ Компактно' : '▭ Подробно' }}
      </button>
    </div>

    <p v-if="servers.length === 0" class="hint">
      Добавьте сервер (по адресу агента habits-agent) или домашнюю машину без
      внешнего IP (push-агент) — и здесь появятся ОС, диски, память и CPU с
      графиками 👇
    </p>

    <div v-for="s in servers" :key="s.id" class="srv-card">
      <div class="srv-head">
        <div class="srv-title">
          <span class="dot" :class="{ ok: isOnline(s), bad: s.last_error || (s.last_ok_at && !isOnline(s)) }" />
          <span v-if="s.kind === 'push'" title="Домашняя машина (push-агент)">🏠</span>
          {{ s.name }}
        </div>
        <span>
          <button
            v-if="s.kind === 'pull'"
            class="icon-btn"
            :disabled="refreshing.has(s.id)"
            title="Обновить"
            @click="refresh(s)"
          >
            {{ refreshing.has(s.id) ? '⏳' : '🔄' }}
          </button>
          <button class="icon-btn" title="Настройки" @click="openEdit(s)">⚙️</button>
        </span>
      </div>

      <p v-if="s.last_error" class="srv-error">⚠️ {{ s.last_error }}</p>
      <p v-if="s.kind === 'push' && !s.last_ok_at" class="srv-wait">
        Ожидаем первый отчёт агента — установите его командой из ⚙️ настроек
      </p>
      <p v-else-if="s.kind === 'push' && minutesSinceOk(s) !== null && !isOnline(s)" class="srv-error">
        ⚠️ Нет отчётов {{ minutesSinceOk(s) }} мин
      </p>

      <template v-if="s.last_data">
        <div v-if="!compact" class="srv-meta">
          <span>💿 {{ s.last_data.os }}</span>
          <span>🌐 {{ s.last_data.external_ip || '—' }}</span>
          <span>⏳ uptime {{ fmtUptime(s.last_data.uptime_sec) }}</span>
          <span>🧠 {{ s.last_data.cpu_cores }} ядер, load {{ s.last_data.load1.toFixed(2) }}</span>
        </div>

        <!-- компактно: один совмещённый график CPU+RAM без подписей -->
        <ComboChart
          v-if="compact"
          :cpu="cpuSeries(s.id)"
          :ram="ramSeries(s.id)"
          :ram-max="ramMax(s.id) || s.last_data.ram.total"
          :cpu-current="`${s.last_data.cpu_pct.toFixed(0)}%`"
          :ram-current="`${fmtBytes(s.last_data.ram.used)}`"
        />
        <template v-else>
          <Sparkline
            :values="cpuSeries(s.id)"
            :max="100"
            color="#f59e0b"
            label="CPU, % (24 ч)"
            :current="`${s.last_data.cpu_pct.toFixed(1)}%`"
          />
          <Sparkline
            :values="ramSeries(s.id)"
            :max="ramMax(s.id) || s.last_data.ram.total"
            color="#60a5fa"
            label="RAM (24 ч)"
            :current="`${fmtBytes(s.last_data.ram.used)} / ${fmtBytes(s.last_data.ram.total)}`"
          />
        </template>

        <div class="disks" :class="{ compact }">
          <div v-for="d in s.last_data.disks" :key="d.mount" class="disk">
            <div class="disk-head">
              <span class="disk-mount">💽 {{ d.mount }}</span>
              <span class="disk-free">
                <template v-if="compact">{{ fmtBytes(d.free) }} своб.</template>
                <template v-else>свободно {{ fmtBytes(d.free) }} из {{ fmtBytes(d.total) }}</template>
              </span>
            </div>
            <div class="disk-bar">
              <div
                class="disk-fill"
                :class="{ warn: diskPct(d.total, d.free) >= 85 }"
                :style="{ width: diskPct(d.total, d.free) + '%' }"
              />
            </div>
          </div>
        </div>
      </template>
    </div>

    <button class="add-btn" @click="openCreate">＋ Добавить сервер</button>
  </template>

  <!-- Модалка сервера -->
  <div v-if="modal" class="modal" @click.self="modal = false">
    <div class="modal-content srv-modal">
      <h3>
        {{ editing ? (form.kind === 'push' ? 'Домашняя машина' : 'Настройки сервера') : 'Добавить' }}
      </h3>

      <div v-if="!editing" class="kind-tabs">
        <button class="kind-tab" :class="{ on: form.kind === 'pull' }" @click="form.kind = 'pull'">
          🖥 Сервер
        </button>
        <button class="kind-tab" :class="{ on: form.kind === 'push' }" @click="form.kind = 'push'">
          🏠 Домашняя машина
        </button>
      </div>
      <p v-if="!editing && form.kind === 'push'" class="kind-hint">
        Для машин без внешнего IP: агент сам шлёт метрики раз в минуту, открытые
        порты не нужны. После создания вы получите токен и команду установки.
      </p>

      <input v-model="form.name" placeholder="Имя (например, домашний ПК)" maxlength="100" />
      <template v-if="form.kind === 'pull'">
        <input v-model="form.url" placeholder="Адрес агента: http://host:9101/metrics" maxlength="500" spellcheck="false" />
        <input v-model="form.token" placeholder="Токен (если задан у агента)" maxlength="200" spellcheck="false" autocomplete="off" />
      </template>

      <template v-if="editing && editing.kind === 'push' && editing.push_token">
        <div class="push-block">
          <div class="push-label">Токен агента</div>
          <code class="push-code" @click="copyText(editing.push_token)">{{ editing.push_token }}</code>
          <div class="push-label">Установка на машине (Linux)</div>
          <code class="push-code" @click="copyText(installCmd(editing))">{{ installCmd(editing) }}</code>
          <p class="kind-hint">Нажмите на токен или команду, чтобы скопировать.</p>
        </div>
      </template>

      <div v-if="editing" class="alerts-block">
        <label class="alerts-toggle">
          <input v-model="form.alerts.enabled" type="checkbox" />
          🔔 Уведомлять о лимитах
        </label>
        <template v-if="form.alerts.enabled">
          <div class="disk-rules">
            <div class="disk-rules-head">💽 Диски — отметьте те, что важны:</div>
            <p v-if="serverDisks().length === 0" class="kind-hint">
              Список появится после первого отчёта агента.
            </p>
            <div v-for="d in serverDisks()" :key="d.mount" class="disk-rule">
              <label class="disk-rule-main">
                <input type="checkbox" :checked="isMonitored(d.mount)" @change="toggleMount(d.mount)" />
                <span class="disk-rule-mount">{{ d.mount }}</span>
                <span class="disk-rule-free">{{ d.free !== null ? fmtBytes(d.free) + ' своб.' : 'нет данных' }}</span>
              </label>
              <div v-if="isMonitored(d.mount)" class="disk-rule-thr">
                меньше
                <input
                  type="number"
                  min="0.1"
                  step="0.1"
                  class="num"
                  :value="ruleGb(d.mount)"
                  @input="setRuleGb(d.mount, ($event.target as HTMLInputElement).value)"
                />
                ГБ
              </div>
            </div>
          </div>
          <div class="alert-row">
            <span>🧠 RAM выше</span>
            <input v-model.number="form.alerts.ram_pct" type="number" min="50" max="100" class="num" />
            <span>% дольше</span>
            <input v-model.number="form.alerts.ram_minutes" type="number" min="1" max="180" class="num" />
            <span>мин</span>
          </div>
          <div class="alert-row">
            <span>🔥 CPU выше</span>
            <input v-model.number="form.alerts.cpu_pct" type="number" min="50" max="100" class="num" />
            <span>% дольше</span>
            <input v-model.number="form.alerts.cpu_minutes" type="number" min="1" max="180" class="num" />
            <span>мин</span>
          </div>
          <p class="kind-hint">
            Проверка раз в минуту, одно сообщение на событие (и одно — когда вернётся
            в норму). Уведомления шлёт бот.
          </p>
        </template>
      </div>

      <button class="btn primary" @click="save">💾 Сохранить</button>
      <button v-if="editing" class="btn danger" @click="remove">
        {{ confirmDelete ? 'Точно удалить с историей?' : '🗑 Удалить' }}
      </button>
      <button class="btn" @click="modal = false">{{ editing?.kind === 'push' ? 'Закрыть' : 'Отмена' }}</button>
    </div>
  </div>
</template>

<style scoped>
.hint {
  text-align: center;
  color: var(--text-secondary);
  padding: 24px 0;
}

.toolbar {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 10px;
}

.compact-toggle {
  border: none;
  border-radius: 8px;
  padding: 6px 12px;
  font-size: 13px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
}

.compact-toggle.on {
  background: var(--accent-color);
  color: #fff;
}

.srv-card {
  background: var(--card-color);
  border-radius: 12px;
  padding: 12px 14px;
  margin-bottom: 12px;
}

.srv-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.srv-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 700;
  font-size: 15px;
}

.dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: var(--text-secondary);
}

.dot.ok {
  background: #22c55e;
}

.dot.bad {
  background: #ef4444;
}

.icon-btn {
  background: none;
  border: none;
  padding: 4px 6px;
}

.srv-error {
  font-size: 12px;
  color: #ef4444;
  margin: 6px 0 0;
  overflow-wrap: anywhere;
}

.srv-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 14px;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 6px 0 2px;
}

.disks {
  margin-top: 10px;
}

.disks.compact {
  margin-top: 8px;
}

.disks.compact .disk {
  margin-bottom: 4px;
}

.disks.compact .disk-head {
  font-size: 11px;
  margin-bottom: 2px;
}

.disks.compact .disk-bar {
  height: 4px;
}

.disk {
  margin-bottom: 8px;
}

.disk-head {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  font-size: 12px;
  margin-bottom: 3px;
}

.disk-mount {
  font-weight: 600;
}

.disk-free {
  color: var(--text-secondary);
}

.disk-bar {
  height: 7px;
  background: var(--bg-secondary);
  border-radius: 4px;
  overflow: hidden;
}

.disk-fill {
  height: 100%;
  background: #22c55e;
  border-radius: 4px;
}

.disk-fill.warn {
  background: #ef4444;
}

.add-btn {
  display: block;
  width: 100%;
  margin-top: 4px;
  padding: 11px;
  border: none;
  border-radius: 8px;
  background: var(--accent-color);
  color: #fff;
}

.srv-wait {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 6px 0 0;
}

.kind-tabs {
  display: flex;
  gap: 6px;
  margin-top: 8px;
}

.kind-tab {
  flex: 1;
  padding: 8px 6px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: 13px;
}

.kind-tab.on {
  background: var(--accent-color);
  color: #fff;
}

.kind-hint {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 8px 0 0;
}

.push-block {
  margin-top: 10px;
}

.push-label {
  font-size: 12px;
  font-weight: 700;
  color: var(--text-secondary);
  margin: 8px 0 4px;
}

.push-code {
  display: block;
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 8px 10px;
  font-size: 11px;
  overflow-wrap: anywhere;
  cursor: pointer;
}

.srv-modal {
  text-align: left;
}

.srv-modal h3 {
  text-align: center;
}

.srv-modal input {
  width: 100%;
  margin-top: 8px;
}

.srv-modal input[type='checkbox'] {
  width: auto;
  margin-top: 0;
  flex: 0 0 auto;
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

.btn.danger {
  background: #b91c1c;
  color: #fff;
}

.alerts-block {
  margin-top: 14px;
  padding-top: 10px;
  border-top: 1px solid var(--bg-secondary);
}

.alerts-toggle {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 600;
}

.alert-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  font-size: 13px;
  color: var(--text-secondary);
  margin-top: 8px;
}

.alert-row .num,
.disk-rule-thr .num {
  width: 62px;
  margin-top: 0;
  padding: 4px 6px;
  text-align: center;
}

.disk-rules {
  margin-top: 10px;
}

.disk-rules-head {
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 4px;
}

.disk-rule {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 6px;
  padding: 4px 0;
}

.disk-rule-main {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  flex: 1;
}

.disk-rule-mount {
  font-weight: 600;
  overflow-wrap: anywhere;
}

.disk-rule-free {
  font-size: 11px;
  color: var(--text-secondary);
}

.disk-rule-thr {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--text-secondary);
}
</style>
