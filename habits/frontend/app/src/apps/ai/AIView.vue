<script setup lang="ts">
// Страница AI: задачи для coding-агентов (Claude Code; Codex — заглушка) на
// домашних машинах. Машина = habits-ai-agent с белым списком папок; задача =
// цепочка прогонов с общим контекстом (--resume). Прогон уходит агенту по WS,
// результат приходит асинхронно — страница поллит, пока есть активные прогоны.
import { computed, onUnmounted, ref, watch } from 'vue'
import { showToast } from '../../shared/toast'
import MarkdownView from '../articles/components/MarkdownView.vue'
import { createArticle } from '../articles/api'
import * as aiApi from './api'
import {
  RUN_STATUS_LABELS,
  type AIMachine,
  type AIRun,
  type AISchedule,
  type AITask,
  type CLISession,
  type CLISessionDetail,
  type UsageInfo,
} from './types'

type ToolKey = 'claude' | 'codex'

const machines = ref<AIMachine[]>([])
const loading = ref(true)
const selectedId = ref<number | null>(null)
const selected = computed(() => machines.value.find((m) => m.id === selectedId.value) ?? null)

async function loadMachines() {
  try {
    machines.value = (await aiApi.fetchMachines()).machines
    if (selectedId.value === null && machines.value.length) {
      selectedId.value = (machines.value.find((m) => m.online) ?? machines.value[0]).id
    }
  } catch {
    showToast('Не удалось загрузить машины')
  } finally {
    loading.value = false
  }
}
loadMachines()

// --- добавление / настройки машины (как на Servers/Terminal) ---
const machineModal = ref(false)
const editing = ref<AIMachine | null>(null)
const machineName = ref('')
const confirmDelete = ref(false)

const INSTALL_PREFIX =
  'curl -fsSL https://raw.githubusercontent.com/resagera/habits-agent/main/install-ai.sh | sudo bash -s -- '

function installCmd(m: AIMachine): string {
  return `${INSTALL_PREFIX}${m.token}`
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    showToast('Скопировано')
  } catch {
    showToast('Не удалось скопировать')
  }
}

function openCreateMachine() {
  editing.value = null
  machineName.value = ''
  confirmDelete.value = false
  machineModal.value = true
}
function openMachineSettings(m: AIMachine) {
  editing.value = m
  machineName.value = m.name
  confirmDelete.value = false
  machineModal.value = true
}

async function saveMachine() {
  const name = machineName.value.trim()
  if (!name) {
    showToast('Имя обязательно')
    return
  }
  try {
    if (editing.value) {
      const { machine } = await aiApi.renameMachine(editing.value.id, name)
      const i = machines.value.findIndex((x) => x.id === machine.id)
      if (i >= 0) machines.value[i] = machine
      machineModal.value = false
    } else {
      const { machine } = await aiApi.createMachine(name)
      machines.value.push(machine)
      selectedId.value = machine.id
      editing.value = machine // остаёмся в модалке: показать токен и команду установки
    }
  } catch {
    showToast('Не удалось сохранить')
  }
}

async function removeMachine() {
  if (!editing.value) return
  if (!confirmDelete.value) {
    confirmDelete.value = true
    setTimeout(() => (confirmDelete.value = false), 3500)
    return
  }
  try {
    await aiApi.deleteMachine(editing.value.id)
    machines.value = machines.value.filter((x) => x.id !== editing.value!.id)
    if (selectedId.value === editing.value.id) selectedId.value = machines.value[0]?.id ?? null
    machineModal.value = false
  } catch {
    showToast('Не удалось удалить')
  }
}

// --- проверка инструментов ---
const checking = ref<string | null>(null) // 'claude' | 'codex'

async function runCheck(tool: ToolKey) {
  const m = selected.value
  if (!m) return
  checking.value = tool
  try {
    const { tool: st } = await aiApi.checkTool(m.id, tool)
    m.tools = { ...m.tools, [tool]: st }
    if (st.authorized) showToast(`${TOOL_LABELS[tool]}: готов ✅`)
    else showToast(st.error || 'Инструмент не готов')
  } catch (e) {
    showToast(e instanceof Error && e.message ? e.message : 'Проверка не удалась')
  } finally {
    checking.value = null
  }
}

/** Инструмент проверен и авторизован — можно ставить задачи. */
function toolReady(tool: ToolKey): boolean {
  return selected.value?.tools?.[tool]?.authorized === true
}

const TOOL_LABELS: Record<string, string> = { claude: 'Claude Code', codex: 'Codex' }
const TOOL_ICONS: Record<string, string> = { claude: '🤖', codex: '🟢' }

// карточки инструментов: до успешной проверки видна только кнопка «Проверить»,
// после — «＋ Задача» и меню ⋮ (лимиты, повторная проверка, старые сессии)
const TOOLS: { key: ToolKey; hint: string; setup?: string }[] = [
  { key: 'claude', hint: 'Установите Claude Code на машине и авторизуйтесь (команда claude).' },
  {
    key: 'codex',
    hint: 'Установите Codex CLI на машине и авторизуйтесь (codex login).',
    setup: 'Установите и авторизуйтесь на машине самостоятельно (npm i -g @openai/codex, затем codex login).',
  },
]

const menuOpen = ref<ToolKey | null>(null)

// --- лимиты (/usage у Claude Code, /status у Codex) ---
const usage = ref<Partial<Record<'claude' | 'codex', UsageInfo>>>({})
const usageLoading = ref<string | null>(null)

async function loadUsage(tool: ToolKey) {
  const m = selected.value
  if (!m) return
  usageLoading.value = tool
  try {
    usage.value = { ...usage.value, [tool]: (await aiApi.fetchUsage(m.id, tool)).usage }
  } catch (e) {
    showToast(e instanceof Error && e.message ? e.message : 'Не удалось получить лимиты')
  } finally {
    usageLoading.value = null
  }
}

// смена машины — сбрасываем показанные лимиты, меню и кэш сессий
watch(selectedId, () => {
  usage.value = {}
  menuOpen.value = null
  sessions.value = []
  sessionDetails.value = {}
})

function fmtResets(iso?: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  const now = new Date()
  const sameDay = d.toDateString() === now.toDateString()
  const t = d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
  return sameDay ? t : `${d.toLocaleDateString('ru-RU', { day: '2-digit', month: '2-digit' })} ${t}`
}

function barClass(p: number): string {
  return p >= 90 ? 'crit' : p >= 70 ? 'warn' : ''
}

// --- старые сессии CLI на машине ---
// Список — из индекса агента (только заголовки), содержимое сессии грузится
// по клику. Любую сессию можно продолжить: новая задача с её папкой и
// session_id, первый прогон уходит агенту с --resume.
const sessionsModal = ref(false)
const sessionsTool = ref<ToolKey>('claude')
const sessions = ref<CLISession[]>([])
const sessionsLoading = ref(false)
const sessionsError = ref('')
const openSessionId = ref('')
const sessionDetails = ref<Record<string, CLISessionDetail>>({})
const sessionLoading = ref(false)
const sessionPrompt = ref('')
const sessionStarting = ref(false)

const ROLE_ICONS: Record<string, string> = { user: '🧑', assistant: '🤖', tool: '🔧' }

async function openSessions(tool: ToolKey) {
  sessionsTool.value = tool
  sessionsModal.value = true
  openSessionId.value = ''
  sessionPrompt.value = ''
  sessions.value = []
  sessionsError.value = ''
  sessionsLoading.value = true
  const m = selected.value
  if (!m) return
  try {
    const { sessions: res } = await aiApi.fetchSessions(m.id, tool)
    sessions.value = res.sessions ?? []
    sessionsError.value = res.error ?? ''
  } catch (e) {
    sessionsError.value = e instanceof Error && e.message ? e.message : 'Не удалось получить список сессий'
  } finally {
    sessionsLoading.value = false
  }
}

async function toggleSession(s: CLISession) {
  if (openSessionId.value === s.id) {
    openSessionId.value = ''
    return
  }
  openSessionId.value = s.id
  sessionPrompt.value = ''
  if (sessionDetails.value[s.id]) return // уже загружали — из кэша
  sessionLoading.value = true
  try {
    const { session } = await aiApi.fetchSession(selected.value!.id, sessionsTool.value, s.id)
    sessionDetails.value = { ...sessionDetails.value, [s.id]: session }
  } catch (e) {
    showToast(e instanceof Error && e.message ? e.message : 'Не удалось загрузить сессию')
    openSessionId.value = ''
  } finally {
    sessionLoading.value = false
  }
}

async function continueSession(s: CLISession) {
  const m = selected.value
  if (!m || !sessionPrompt.value.trim()) return
  sessionStarting.value = true
  try {
    const { task, queued_offline } = await aiApi.createTask({
      machine_id: m.id,
      tool: sessionsTool.value,
      workdir: s.workdir,
      model: '',
      params: '',
      prompt: sessionPrompt.value,
      session_id: s.id,
    })
    sessionsModal.value = false
    await loadTasks()
    await openTaskView(task.id)
    showToast(queued_offline ? 'Агент офлайн — задача в очереди ⏳' : 'Продолжаем сессию 🚀')
  } catch (e) {
    showToast(e instanceof Error && e.message ? e.message : 'Не удалось продолжить сессию')
  } finally {
    sessionStarting.value = false
  }
}

function fmtBytes(n: number): string {
  if (n >= 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} МБ`
  return `${Math.max(1, Math.round(n / 1024))} КБ`
}

// --- задачи ---
const tasks = ref<AITask[]>([])
const tasksLoading = ref(false)

async function loadTasks() {
  const m = selected.value
  if (!m) {
    tasks.value = []
    return
  }
  tasksLoading.value = true
  try {
    tasks.value = (await aiApi.fetchTasks(m.id)).tasks
  } catch {
    /* поллинг повторит */
  } finally {
    tasksLoading.value = false
  }
}
watch(selectedId, loadTasks, { immediate: true })

// поллинг, пока есть активные прогоны (или открыта задача с активным прогоном)
const hasActive = computed(() =>
  tasks.value.some((t) => t.last_status === 'queued' || t.last_status === 'running'),
)
let pollTimer: ReturnType<typeof setInterval> | undefined
watch(
  hasActive,
  (active) => {
    clearInterval(pollTimer)
    if (active) {
      pollTimer = setInterval(async () => {
        await loadTasks()
        if (openTask.value) await refreshOpenTask()
      }, 5000)
    }
  },
  { immediate: true },
)
onUnmounted(() => clearInterval(pollTimer))

// --- новая задача ---
const taskModal = ref(false)
const taskForm = ref({ tool: 'claude', workdir: '', subdir: '', model: '', params: '', prompt: '', planFirst: false })
const creating = ref(false)

function openCreateTask(tool: 'claude' | 'codex') {
  const m = selected.value
  if (!m) return
  taskForm.value = { tool, workdir: m.dirs[0] ?? '', subdir: '', model: '', params: '', prompt: '', planFirst: false }
  taskModal.value = true
}

function fullWorkdir(): string {
  const sub = taskForm.value.subdir.trim().replace(/^\/+|\/+$/g, '')
  return sub ? `${taskForm.value.workdir}/${sub}` : taskForm.value.workdir
}

async function createTask() {
  const m = selected.value
  if (!m) return
  if (!taskForm.value.prompt.trim()) {
    showToast('Опишите задачу')
    return
  }
  if (!taskForm.value.workdir) {
    showToast('Выберите папку')
    return
  }
  creating.value = true
  try {
    const { task, queued_offline } = await aiApi.createTask({
      machine_id: m.id,
      tool: taskForm.value.tool,
      workdir: fullWorkdir(),
      model: taskForm.value.model,
      params: taskForm.value.params.trim(),
      prompt: taskForm.value.prompt,
      mode: taskForm.value.planFirst ? 'plan' : '',
    })
    taskModal.value = false
    await loadTasks()
    await openTaskView(task.id)
    showToast(queued_offline ? 'Агент офлайн — задача в очереди, запустится при подключении ⏳' : 'Задача отправлена агенту 🚀')
  } catch (e) {
    showToast(e instanceof Error && e.message ? e.message : 'Не удалось создать задачу')
  } finally {
    creating.value = false
  }
}

// --- просмотр задачи (прогоны, продолжение) ---
const openTask = ref<AITask | null>(null)
const openRuns = ref<AIRun[]>([])
const collapsedRuns = ref<Set<number>>(new Set())
const continuePrompt = ref('')
const continuing = ref(false)
const confirmDeleteTask = ref(false)

async function openTaskView(id: number) {
  try {
    const { task, runs } = await aiApi.fetchTask(id)
    openTask.value = task
    openRuns.value = runs
    // все прогоны, кроме последнего, свёрнуты
    collapsedRuns.value = new Set(runs.slice(0, -1).map((r) => r.id))
    continuePrompt.value = ''
    confirmDeleteTask.value = false
  } catch {
    showToast('Не удалось открыть задачу')
  }
}

async function refreshOpenTask() {
  const t = openTask.value
  if (!t) return
  try {
    const { task, runs } = await aiApi.fetchTask(t.id)
    openTask.value = task
    openRuns.value = runs
  } catch {
    /* поллинг повторит */
  }
}

const lastRunFinished = computed(() => {
  const last = openRuns.value[openRuns.value.length - 1]
  return last ? last.status === 'done' || last.status === 'error' : false
})

// последний прогон — успешный план: предлагаем «Выполнить план»
const planReady = computed(() => {
  const last = openRuns.value[openRuns.value.length - 1]
  return last?.mode === 'plan' && last.status === 'done'
})
const executingPlan = ref(false)

async function executePlan() {
  const t = openTask.value
  if (!t) return
  executingPlan.value = true
  try {
    const { queued_offline } = await aiApi.continueTask(
      t.id,
      'Выполни составленный план (тот, что ты только что описал). Действуй по шагам плана.',
      '',
    )
    await refreshOpenTask()
    await loadTasks()
    showToast(queued_offline ? 'Агент офлайн — выполнение в очереди ⏳' : 'Выполнение плана запущено ▶️')
  } catch (e) {
    showToast(e instanceof Error && e.message ? e.message : 'Не удалось запустить')
  } finally {
    executingPlan.value = false
  }
}

function toggleRun(id: number) {
  const s = new Set(collapsedRuns.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  collapsedRuns.value = s
}

async function continueRun() {
  const t = openTask.value
  if (!t || !continuePrompt.value.trim()) return
  continuing.value = true
  try {
    const { queued_offline } = await aiApi.continueTask(t.id, continuePrompt.value)
    continuePrompt.value = ''
    await refreshOpenTask()
    await loadTasks()
    showToast(queued_offline ? 'Агент офлайн — прогон в очереди ⏳' : 'Прогон отправлен 🚀')
  } catch (e) {
    showToast(e instanceof Error && e.message ? e.message : 'Не удалось продолжить')
  } finally {
    continuing.value = false
  }
}

// --- остановка прогона ---
const cancelingId = ref<number | null>(null)

async function cancelRun(r: AIRun) {
  cancelingId.value = r.id
  try {
    await aiApi.cancelRun(r.id)
    showToast('Остановка запрошена ⏹')
    await refreshOpenTask()
    await loadTasks()
  } catch (e) {
    showToast(e instanceof Error && e.message ? e.message : 'Не удалось остановить')
  } finally {
    cancelingId.value = null
  }
}

// --- результат: копия и экспорт в статью ---
async function copyOutput(r: AIRun) {
  try {
    await navigator.clipboard.writeText(r.output || r.error)
    showToast('Скопировано')
  } catch {
    showToast('Не удалось скопировать')
  }
}

const savingArticleId = ref<number | null>(null)

async function saveToArticle(r: AIRun) {
  const t = openTask.value
  if (!t) return
  savingArticleId.value = r.id
  try {
    const title = `AI: ${t.title}`.slice(0, 120)
    const body = `> **Задача:** ${r.prompt}\n\n${r.output || r.error}`
    await createArticle(title, body, null)
    showToast('Сохранено в Articles 📄')
  } catch {
    showToast('Не удалось сохранить статью')
  } finally {
    savingArticleId.value = null
  }
}

// --- расписания ---
const schedules = ref<AISchedule[]>([])
const scheduleModal = ref(false)
const editingSchedule = ref<AISchedule | null>(null)
const scheduleSaving = ref(false)
const DOW_LABELS = ['Вс', 'Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб']
const scheduleForm = ref({
  tool: 'claude',
  workdir: '',
  subdir: '',
  model: '',
  params: '',
  prompt: '',
  period: 'daily' as 'daily' | 'weekly' | 'hours',
  time: '09:00',
  dow: 1,
  every_hours: 24,
})

async function loadSchedules() {
  try {
    schedules.value = (await aiApi.fetchSchedules()).schedules
  } catch {
    /* не критично */
  }
}
loadSchedules()

const machineSchedules = computed(() => schedules.value.filter((s) => s.machine_id === selectedId.value))

function openCreateSchedule() {
  const m = selected.value
  if (!m) return
  editingSchedule.value = null
  scheduleForm.value = {
    tool: 'claude', workdir: m.dirs[0] ?? '', subdir: '', model: '', params: '',
    prompt: '', period: 'daily', time: '09:00', dow: 1, every_hours: 24,
  }
  scheduleModal.value = true
}

function openEditSchedule(s: AISchedule) {
  editingSchedule.value = s
  const hh = String(Math.floor(s.at_minute / 60)).padStart(2, '0')
  const mm = String(s.at_minute % 60).padStart(2, '0')
  scheduleForm.value = {
    tool: s.tool, workdir: s.workdir, subdir: '', model: s.model, params: s.params,
    prompt: s.prompt, period: s.period, time: `${hh}:${mm}`, dow: s.dow, every_hours: s.every_hours,
  }
  scheduleModal.value = true
}

function scheduleBody(): aiApi.ScheduleBody | null {
  const m = selected.value
  if (!m || !scheduleForm.value.prompt.trim()) return null
  const [hh, mm] = scheduleForm.value.time.split(':').map(Number)
  const sub = scheduleForm.value.subdir.trim().replace(/^\/+|\/+$/g, '')
  return {
    machine_id: editingSchedule.value?.machine_id ?? m.id,
    tool: scheduleForm.value.tool,
    workdir: sub ? `${scheduleForm.value.workdir}/${sub}` : scheduleForm.value.workdir,
    model: scheduleForm.value.model,
    params: scheduleForm.value.params.trim(),
    prompt: scheduleForm.value.prompt,
    period: scheduleForm.value.period,
    at_minute: (hh || 0) * 60 + (mm || 0),
    dow: scheduleForm.value.dow,
    every_hours: scheduleForm.value.every_hours,
    tz_off: -new Date().getTimezoneOffset(),
    enabled: editingSchedule.value?.enabled ?? true,
  }
}

async function saveSchedule() {
  const body = scheduleBody()
  if (!body) {
    showToast('Опишите задачу')
    return
  }
  scheduleSaving.value = true
  try {
    if (editingSchedule.value) {
      const { schedule } = await aiApi.updateSchedule(editingSchedule.value.id, body)
      const i = schedules.value.findIndex((x) => x.id === schedule.id)
      if (i >= 0) schedules.value[i] = schedule
    } else {
      const { schedule } = await aiApi.createSchedule(body)
      schedules.value.push(schedule)
    }
    scheduleModal.value = false
    showToast('Расписание сохранено ✅')
  } catch (e) {
    showToast(e instanceof Error && e.message ? e.message : 'Не удалось сохранить')
  } finally {
    scheduleSaving.value = false
  }
}

async function toggleSchedule(s: AISchedule) {
  try {
    const { schedule } = await aiApi.updateSchedule(s.id, {
      machine_id: s.machine_id, tool: s.tool, workdir: s.workdir, model: s.model,
      params: s.params, prompt: s.prompt, period: s.period, at_minute: s.at_minute,
      dow: s.dow, every_hours: s.every_hours, tz_off: s.tz_off, enabled: !s.enabled,
    })
    const i = schedules.value.findIndex((x) => x.id === schedule.id)
    if (i >= 0) schedules.value[i] = schedule
  } catch {
    showToast('Не удалось переключить')
  }
}

async function removeSchedule(s: AISchedule) {
  try {
    await aiApi.deleteSchedule(s.id)
    schedules.value = schedules.value.filter((x) => x.id !== s.id)
    scheduleModal.value = false
  } catch {
    showToast('Не удалось удалить')
  }
}

function scheduleWhen(s: AISchedule): string {
  const hh = String(Math.floor(s.at_minute / 60)).padStart(2, '0')
  const mm = String(s.at_minute % 60).padStart(2, '0')
  if (s.period === 'daily') return `ежедневно в ${hh}:${mm}`
  if (s.period === 'weekly') return `по ${DOW_LABELS[s.dow % 7]} в ${hh}:${mm}`
  return `каждые ${s.every_hours} ч`
}

async function removeTask() {
  const t = openTask.value
  if (!t) return
  if (!confirmDeleteTask.value) {
    confirmDeleteTask.value = true
    setTimeout(() => (confirmDeleteTask.value = false), 3500)
    return
  }
  try {
    await aiApi.deleteTask(t.id)
    openTask.value = null
    await loadTasks()
  } catch {
    showToast('Не удалось удалить')
  }
}

function fmtTime(iso?: string): string {
  if (!iso) return ''
  return new Date(iso).toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })
}

function runMetaLine(r: AIRun): string {
  const parts: string[] = []
  if (r.meta.num_turns) parts.push(`${r.meta.num_turns} шагов`)
  if (r.meta.elapsed_sec) parts.push(`${r.meta.elapsed_sec}с`)
  if (r.meta.cost_usd) parts.push(`$${r.meta.cost_usd.toFixed(3)}`)
  if (r.meta.tokens_out) parts.push(`${r.meta.tokens_in ?? 0}→${r.meta.tokens_out} ткн`)
  if (r.meta.branch) parts.push(`⎇ ${r.meta.branch}`)
  return parts.join(' · ')
}

function shortDir(d: string): string {
  const parts = d.split('/')
  return parts.length > 2 ? '…/' + parts.slice(-2).join('/') : d
}
</script>

<template>
  <!-- машины -->
  <div class="head-row">
    <h3 class="sect-title">Машины</h3>
    <button class="btn slim" @click="openCreateMachine">＋ Машина</button>
  </div>
  <p v-if="loading" class="hint">Загрузка…</p>
  <p v-else-if="!machines.length" class="hint">
    Добавьте машину — компьютер, где установлен Claude Code. После добавления вы
    получите команду установки агента.
  </p>
  <div v-else class="machines">
    <button
      v-for="m in machines"
      :key="m.id"
      class="machine"
      :class="{ active: m.id === selectedId }"
      @click="selectedId = m.id"
    >
      <span class="dot" :class="{ online: m.online }"></span>
      <span class="m-name">{{ m.name }}</span>
      <span v-if="m.busy > 0" class="chip run">⏳ {{ m.busy }}</span>
      <span class="m-gear" @click.stop="openMachineSettings(m)">⚙️</span>
    </button>
  </div>

  <template v-if="selected">
    <!-- инструменты -->
    <h3 class="sect-title">Инструменты</h3>
    <div v-for="t in TOOLS" :key="t.key" class="tool-card">
      <div class="tool-head">
        <span class="tool-name">{{ TOOL_ICONS[t.key] }} {{ TOOL_LABELS[t.key] }}</span>
        <span v-if="selected.tools?.[t.key]?.authorized" class="chip ok">
          готов · {{ selected.tools[t.key]!.version.split(' ')[0] }}
        </span>
        <span v-else-if="selected.tools?.[t.key]" class="chip bad">не готов</span>
        <span v-else class="chip">не проверялся</span>
        <span v-if="checking === t.key" class="chip run">проверка…</span>
        <span v-else-if="usageLoading === t.key" class="chip run">лимиты…</span>
      </div>
      <p v-if="selected.tools?.[t.key] && !selected.tools[t.key]!.authorized" class="hint">
        {{ selected.tools[t.key]!.error || t.hint }}
      </p>
      <p v-if="selected.tools?.[t.key]?.path" class="hint path">{{ selected.tools[t.key]!.path }}</p>
      <p v-else-if="!selected.tools?.[t.key] && t.setup" class="hint">{{ t.setup }}</p>
      <!-- пока не проверено — только «Проверить»; после — задача и меню ⋮ -->
      <div class="row">
        <template v-if="toolReady(t.key)">
          <button class="btn primary" :disabled="!selected.online" @click="openCreateTask(t.key)">
            ＋ Задача
          </button>
          <div class="menu-wrap">
            <button class="btn slim" @click="menuOpen = menuOpen === t.key ? null : t.key">⋮</button>
            <div v-if="menuOpen === t.key" class="menu">
              <button :disabled="!selected.online" @click="menuOpen = null; openSessions(t.key)">
                🗂 Старые сессии
              </button>
              <button :disabled="usageLoading !== null || !selected.online" @click="menuOpen = null; loadUsage(t.key)">
                📊 Лимиты
              </button>
              <button :disabled="checking !== null || !selected.online" @click="menuOpen = null; runCheck(t.key)">
                🔄 Проверить
              </button>
            </div>
          </div>
        </template>
        <button v-else class="btn" :disabled="checking !== null || !selected.online" @click="runCheck(t.key)">
          {{ checking === t.key ? 'Проверка… (до 1 мин)' : 'Проверить' }}
        </button>
      </div>
      <div v-if="usage[t.key]" class="usage">
        <p v-if="usage[t.key]!.error" class="hint warn">{{ usage[t.key]!.error }}</p>
        <template v-else>
          <p v-if="usage[t.key]!.plan" class="hint">План: {{ usage[t.key]!.plan }}</p>
          <div v-for="w in usage[t.key]!.windows" :key="w.label" class="u-row">
            <span class="u-label">{{ w.label }}</span>
            <span class="u-bar"><span class="u-fill" :class="barClass(w.percent)" :style="{ width: Math.min(100, w.percent) + '%' }"></span></span>
            <span class="u-pct">{{ Math.round(w.percent) }}%</span>
            <span v-if="w.resets_at" class="u-reset">↻ {{ fmtResets(w.resets_at) }}</span>
          </div>
          <p v-if="usage[t.key]!.note" class="hint">{{ usage[t.key]!.note }}</p>
        </template>
      </div>
    </div>
    <p v-if="!selected.online" class="hint warn">Агент не в сети — проверьте, что он запущен на машине.</p>
    <div v-if="menuOpen" class="menu-backdrop" @click="menuOpen = null"></div>

    <!-- задачи -->
    <div class="head-row">
      <h3 class="sect-title">Задачи</h3>
    </div>
    <p v-if="tasksLoading && !tasks.length" class="hint">Загрузка…</p>
    <p v-else-if="!tasks.length" class="hint">Задач пока нет.</p>
    <button v-for="t in tasks" :key="t.id" class="task" @click="openTaskView(t.id)">
      <div class="t-top">
        <span class="t-title">{{ t.title }}</span>
        <span class="chip" :class="{ ok: t.last_status === 'done', bad: t.last_status === 'error', run: t.last_status === 'running' || t.last_status === 'queued' }">
          {{ RUN_STATUS_LABELS[t.last_status] ?? t.last_status }}
        </span>
      </div>
      <div class="t-sub">
        {{ TOOL_ICONS[t.tool] ?? '' }} {{ TOOL_LABELS[t.tool] ?? t.tool }} ·
        📁 {{ shortDir(t.workdir) }} · прогонов: {{ t.run_count }}
        <template v-if="t.model"> · {{ t.model }}</template>
        · {{ fmtTime(t.updated_at) }}
      </div>
    </button>

    <!-- расписания -->
    <div class="head-row">
      <h3 class="sect-title">Расписания</h3>
      <button class="btn slim" @click="openCreateSchedule">＋ Расписание</button>
    </div>
    <p v-if="!machineSchedules.length" class="hint">
      Периодические задачи для этой машины — например, «каждое утро прогони тесты и пришли отчёт».
    </p>
    <div v-for="s in machineSchedules" :key="s.id" class="task sched">
      <div class="t-top">
        <label class="sw" @click.stop>
          <input type="checkbox" :checked="s.enabled" @change="toggleSchedule(s)" />
        </label>
        <span class="t-title" :class="{ off: !s.enabled }" @click="openEditSchedule(s)">{{ s.prompt }}</span>
        <span class="chip">{{ scheduleWhen(s) }}</span>
      </div>
      <div class="t-sub" @click="openEditSchedule(s)">
        {{ TOOL_ICONS[s.tool] ?? '' }} {{ TOOL_LABELS[s.tool] ?? s.tool }} · 📁 {{ shortDir(s.workdir) }}
        <template v-if="s.enabled && s.next_run_at"> · след. запуск {{ fmtTime(s.next_run_at) }}</template>
      </div>
    </div>
  </template>

  <!-- модалка машины: создание / настройки + инструкция -->
  <div v-if="machineModal" class="modal" @click.self="machineModal = false">
    <div class="modal-content">
      <h3>{{ editing ? '⚙️ Машина' : '＋ Новая машина' }}</h3>
      <input v-model="machineName" placeholder="Название (например, Домашний ПК)" class="full-w" />
      <template v-if="editing">
        <p class="hint left">
          Установите агента на машину (Linux) — выполните команду от root. Агент получит
          доступ только к папкам, которые вы укажете при установке:
        </p>
        <pre class="code" @click="copyText(installCmd(editing))">{{ installCmd(editing) }}</pre>
        <p class="hint left">Токен машины (для ручной настройки):</p>
        <pre class="code" @click="copyText(editing.token)">{{ editing.token }}</pre>
        <p class="hint left">
          Разрешённые папки задаются на машине (AI_AGENT_DIRS) и появятся здесь после
          подключения агента.
          <template v-if="editing.dirs.length">Сейчас: {{ editing.dirs.join(', ') }}</template>
        </p>
      </template>
      <div class="row">
        <button class="btn" @click="machineModal = false">Закрыть</button>
        <button v-if="editing" class="btn danger" @click="removeMachine">
          {{ confirmDelete ? 'Точно удалить?' : 'Удалить' }}
        </button>
        <button class="btn primary" @click="saveMachine">{{ editing ? 'Сохранить' : 'Добавить' }}</button>
      </div>
    </div>
  </div>

  <!-- модалка новой задачи -->
  <div v-if="taskModal" class="modal" @click.self="taskModal = false">
    <div class="modal-content">
      <h3>＋ Задача для {{ TOOL_LABELS[taskForm.tool] }}</h3>
      <label class="lbl">Папка</label>
      <select v-model="taskForm.workdir" class="full-w">
        <option v-for="d in selected?.dirs ?? []" :key="d" :value="d">{{ d }}</option>
      </select>
      <input v-model="taskForm.subdir" placeholder="Подпапка внутри (необязательно)" class="full-w" />
      <label class="lbl">Модель</label>
      <select v-if="taskForm.tool === 'claude'" v-model="taskForm.model" class="full-w">
        <option value="">По умолчанию</option>
        <option value="fable">Fable</option>
        <option value="opus">Opus</option>
        <option value="sonnet">Sonnet</option>
        <option value="haiku">Haiku</option>
      </select>
      <template v-else>
        <input
          v-model="taskForm.model"
          list="codex-models"
          placeholder="Пусто — модель из config.toml на машине"
          class="full-w"
        />
        <datalist id="codex-models">
          <option value="gpt-5.5"></option>
          <option value="gpt-5.5-codex"></option>
          <option value="gpt-5.1-codex-mini"></option>
        </datalist>
      </template>
      <label class="lbl">Задача</label>
      <textarea
        v-model="taskForm.prompt"
        rows="5"
        class="full-w"
        placeholder="Опишите, что нужно сделать в этой папке…"
      ></textarea>
      <label class="radio">
        <input v-model="taskForm.planFirst" type="checkbox" />
        <span>📝 Сначала план (без правок файлов) — выполнение отдельной кнопкой после просмотра</span>
      </label>
      <label class="lbl">Доп. параметры CLI (необязательно)</label>
      <input v-model="taskForm.params" placeholder='например: --max-turns 30' class="full-w" />
      <details class="params-help">
        <summary>Справка по параметрам</summary>
        <ul v-if="taskForm.tool === 'claude'">
          <li><code>--max-turns N</code> — ограничить число шагов агента</li>
          <li><code>--permission-mode plan</code> — только план, без правок файлов</li>
          <li><code>--allowed-tools "Bash(git:*) Edit"</code> — белый список инструментов</li>
          <li><code>--disallowed-tools "WebSearch"</code> — запретить инструменты</li>
          <li><code>--append-system-prompt "…"</code> — дописать системный промпт</li>
          <li><code>--add-dir /path</code> — разрешить доп. папку (в пределах белого списка агента)</li>
          <li><code>--fallback-model sonnet</code> — запасная модель при перегрузке</li>
        </ul>
        <ul v-else>
          <li><code>--sandbox read-only</code> — только чтение (аналог «плана»)</li>
          <li><code>-c model_reasoning_effort="high"</code> — усердие размышлений</li>
          <li><code>-c 'key=value'</code> — любой override config.toml</li>
          <li><code>--add-dir /path</code> — доп. папка для записи</li>
          <li><code>--ephemeral</code> — не сохранять сессию на диск (без продолжения)</li>
          <li><code>--enable/--disable FEATURE</code> — фичи Codex</li>
        </ul>
        <p v-if="taskForm.tool === 'claude'" class="hint left">
          Флаги <code>-p</code>, <code>--output-format</code>, <code>--resume</code>,
          <code>--session-id</code> задаются автоматически — агент их игнорирует. По
          умолчанию прогон идёт с полным bypass прав (настройка агента).
        </p>
        <p v-else class="hint left">
          Флаги <code>--json</code>, <code>-o</code>, <code>-C</code>, <code>-m</code>
          задаются автоматически — агент их игнорирует. По умолчанию прогон идёт без
          песочницы (bypass, настройка агента); продолжение задачи —
          <code>codex exec resume</code>.
        </p>
      </details>
      <div class="row">
        <button class="btn" @click="taskModal = false">Отмена</button>
        <button class="btn primary" :disabled="creating" @click="createTask">
          {{ creating ? 'Отправка…' : 'Запустить' }}
        </button>
      </div>
    </div>
  </div>

  <!-- модалка старых сессий CLI: заголовки сразу, содержимое — по клику -->
  <div v-if="sessionsModal" class="modal" @click.self="sessionsModal = false">
    <div class="modal-content wide">
      <h3>🗂 Сессии {{ TOOL_LABELS[sessionsTool] }}</h3>
      <p class="hint left">
        Прошлые сессии инструмента на машине «{{ selected?.name }}» — только в разрешённых папках агента.
        Содержимое загружается по клику; любую сессию можно продолжить в её папке и с её контекстом.
      </p>
      <p v-if="sessionsLoading" class="hint">Загрузка списка…</p>
      <p v-else-if="sessionsError" class="hint warn">{{ sessionsError }}</p>
      <div v-for="s in sessions" :key="s.id" class="run">
        <button class="run-head" @click="toggleSession(s)">
          <span class="chev">{{ openSessionId === s.id ? '▾' : '▸' }}</span>
          <span class="run-prompt">{{ s.title }}</span>
          <span class="chip">{{ fmtTime(s.updated_at) }}</span>
        </button>
        <div v-if="openSessionId === s.id" class="run-body">
          <p class="hint left">
            📁 {{ s.workdir }}
            <template v-if="s.branch"> · ⎇ {{ s.branch }}</template>
            · реплик: {{ s.msgs }} · {{ fmtBytes(s.bytes) }}
          </p>
          <p v-if="sessionLoading" class="hint">Загрузка содержимого…</p>
          <template v-else-if="sessionDetails[s.id]">
            <p v-if="sessionDetails[s.id].error" class="hint warn">{{ sessionDetails[s.id].error }}</p>
            <p v-if="sessionDetails[s.id].truncated" class="hint">
              Сессия длинная — показан её хвост (начало обрезано).
            </p>
            <div v-if="sessionDetails[s.id].messages.length" class="sess-log">
              <div v-for="(msg, i) in sessionDetails[s.id].messages" :key="i" class="sess-msg" :class="msg.role">
                <span class="sess-role">{{ ROLE_ICONS[msg.role] }}</span>
                <span class="sess-text">{{ msg.text }}</span>
              </div>
            </div>
          </template>
          <label class="lbl">Продолжить эту сессию</label>
          <textarea
            v-model="sessionPrompt"
            rows="3"
            class="full-w"
            placeholder="Что сделать дальше — агент продолжит с контекстом этой сессии…"
          ></textarea>
          <button
            class="btn primary"
            :disabled="sessionStarting || !sessionPrompt.trim()"
            @click="continueSession(s)"
          >
            {{ sessionStarting ? 'Отправка…' : '▶️ Продолжить сессию' }}
          </button>
        </div>
      </div>
      <div class="row">
        <button class="btn" @click="sessionsModal = false">Закрыть</button>
      </div>
    </div>
  </div>

  <!-- модалка расписания -->
  <div v-if="scheduleModal" class="modal" @click.self="scheduleModal = false">
    <div class="modal-content">
      <h3>{{ editingSchedule ? '🕒 Расписание' : '＋ Новое расписание' }}</h3>
      <label class="lbl">Инструмент</label>
      <select v-model="scheduleForm.tool" class="full-w">
        <option value="claude">Claude Code</option>
        <option value="codex">Codex</option>
      </select>
      <label class="lbl">Папка</label>
      <select v-if="!editingSchedule" v-model="scheduleForm.workdir" class="full-w">
        <option v-for="d in selected?.dirs ?? []" :key="d" :value="d">{{ d }}</option>
      </select>
      <input v-else v-model="scheduleForm.workdir" class="full-w" />
      <input v-if="!editingSchedule" v-model="scheduleForm.subdir" placeholder="Подпапка внутри (необязательно)" class="full-w" />
      <label class="lbl">Периодичность</label>
      <select v-model="scheduleForm.period" class="full-w">
        <option value="daily">Ежедневно</option>
        <option value="weekly">Еженедельно</option>
        <option value="hours">Каждые N часов</option>
      </select>
      <div v-if="scheduleForm.period !== 'hours'" class="row">
        <select v-if="scheduleForm.period === 'weekly'" v-model.number="scheduleForm.dow" class="grow">
          <option v-for="(d, i) in DOW_LABELS" :key="i" :value="i">{{ d }}</option>
        </select>
        <input v-model="scheduleForm.time" type="time" class="grow" />
      </div>
      <input
        v-else
        v-model.number="scheduleForm.every_hours"
        type="number"
        min="1"
        max="720"
        class="full-w"
        placeholder="Интервал в часах"
      />
      <label class="lbl">Модель (пусто — по умолчанию)</label>
      <input v-model="scheduleForm.model" class="full-w" placeholder="fable / opus / haiku / gpt-5.5…" />
      <label class="lbl">Задача</label>
      <textarea v-model="scheduleForm.prompt" rows="4" class="full-w" placeholder="Что делать при каждом запуске…"></textarea>
      <input v-model="scheduleForm.params" class="full-w" placeholder="Доп. параметры CLI (необязательно)" />
      <p class="hint left">
        Каждый запуск создаёт новую задачу (🕒 в списке) со свежим контекстом; результат придёт
        уведомлением бота. Время — ваша текущая таймзона.
      </p>
      <div class="row">
        <button class="btn" @click="scheduleModal = false">Отмена</button>
        <button v-if="editingSchedule" class="btn danger" @click="removeSchedule(editingSchedule)">Удалить</button>
        <button class="btn primary" :disabled="scheduleSaving" @click="saveSchedule">
          {{ scheduleSaving ? 'Сохранение…' : 'Сохранить' }}
        </button>
      </div>
    </div>
  </div>

  <!-- модалка задачи: прогоны + продолжение -->
  <div v-if="openTask" class="modal" @click.self="openTask = null">
    <div class="modal-content wide">
      <h3>{{ openTask.title }}</h3>
      <p class="hint left">
        {{ TOOL_ICONS[openTask.tool] ?? '' }} {{ TOOL_LABELS[openTask.tool] ?? openTask.tool }} ·
        📁 {{ openTask.workdir }}
        <template v-if="openTask.model"> · модель: {{ openTask.model }}</template>
        <template v-if="openTask.params"> · {{ openTask.params }}</template>
      </p>

      <div v-for="(r, i) in openRuns" :key="r.id" class="run">
        <button class="run-head" @click="toggleRun(r.id)">
          <span class="chev">{{ collapsedRuns.has(r.id) ? '▸' : '▾' }}</span>
          <span class="run-n">#{{ i + 1 }}</span>
          <span v-if="r.mode === 'plan'" class="chip plan">📝 план</span>
          <span class="run-prompt">{{ r.prompt }}</span>
          <span class="chip" :class="{ ok: r.status === 'done', bad: r.status === 'error', run: r.status === 'running' || r.status === 'queued' }">
            {{ RUN_STATUS_LABELS[r.status] }}
          </span>
        </button>
        <div v-if="!collapsedRuns.has(r.id)" class="run-body">
          <p class="run-prompt-full">{{ r.prompt }}</p>
          <template v-if="r.status === 'running' || r.status === 'queued'">
            <p class="hint">
              {{ r.status === 'queued' ? '⏳ В очереди — ждёт агента…' : '⏳ Выполняется… (лог обновляется автоматически)' }}
            </p>
            <pre v-if="r.log" class="run-out live">{{ r.log }}</pre>
            <button class="btn danger slim" :disabled="cancelingId === r.id" @click="cancelRun(r)">
              {{ cancelingId === r.id ? 'Остановка…' : '⏹ Остановить' }}
            </button>
          </template>
          <div v-if="r.output" class="run-md">
            <MarkdownView :source="r.output" />
          </div>
          <details v-if="(r.status === 'done' || r.status === 'error') && r.log">
            <summary class="hint">Ход выполнения</summary>
            <pre class="run-out">{{ r.log }}</pre>
          </details>
          <pre v-if="r.error" class="run-out err">{{ r.error }}</pre>
          <p v-if="runMetaLine(r)" class="hint">{{ runMetaLine(r) }}</p>
          <details v-if="r.meta.diffstat">
            <summary class="hint">Изменения (git diff --stat)</summary>
            <pre class="run-out">{{ r.meta.diffstat }}</pre>
          </details>
          <div v-if="r.status === 'done' || r.status === 'error'" class="row">
            <button class="btn slim" @click="copyOutput(r)">📋 Копировать</button>
            <button class="btn slim" :disabled="savingArticleId === r.id" @click="saveToArticle(r)">
              {{ savingArticleId === r.id ? 'Сохранение…' : '📄 В статью' }}
            </button>
          </div>
        </div>
      </div>

      <button
        v-if="planReady"
        class="btn primary"
        style="margin-top: 8px"
        :disabled="executingPlan"
        @click="executePlan"
      >
        {{ executingPlan ? 'Запуск…' : '▶️ Выполнить план' }}
      </button>

      <template v-if="lastRunFinished">
        <label class="lbl">Продолжить с тем же контекстом</label>
        <textarea
          v-model="continuePrompt"
          rows="3"
          class="full-w"
          placeholder="Следующая задача — агент помнит контекст предыдущих прогонов…"
        ></textarea>
        <div class="row">
          <button class="btn primary" :disabled="continuing || !continuePrompt.trim()" @click="continueRun">
            {{ continuing ? 'Отправка…' : 'Продолжить' }}
          </button>
        </div>
      </template>

      <div class="row" style="margin-top: 10px">
        <button class="btn" @click="openTask = null">Закрыть</button>
        <button class="btn danger" @click="removeTask">
          {{ confirmDeleteTask ? 'Точно удалить задачу?' : 'Удалить задачу' }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.head-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin: 4px 0 8px;
}

.sect-title {
  margin: 10px 0 8px;
  font-size: 16px;
}

.hint {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 4px 0 10px;
}

.hint.left {
  text-align: left;
}

.hint.warn {
  color: #f59e0b;
}

/* путь до бинарника: длинный, ломаем по символам, чтобы не рвал карточку */
.hint.path {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px;
  opacity: 0.75;
  word-break: break-all;
  margin-top: -4px;
}

.machines {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 6px;
}

.machine {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border: 1px solid var(--hover-bg-color);
  border-radius: 10px;
  background: var(--card-color);
  color: var(--text-color);
}

.machine.active {
  border-color: var(--accent-color);
}

.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #6b7280;
}

.dot.online {
  background: #22c55e;
}

.m-gear {
  opacity: 0.7;
}

.tool-card {
  background: var(--card-color);
  border-radius: 10px;
  padding: 12px 14px;
  margin-bottom: 10px;
}

.tool-card.stub {
  opacity: 0.85;
}

.tool-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.tool-name {
  font-weight: 600;
}

.chip {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  flex: none;
}

.chip.ok {
  background: #16653420;
  color: #22c55e;
}

.chip.bad {
  background: #7f1d1d20;
  color: #ef4444;
}

.chip.run {
  background: #1e3a8a20;
  color: #60a5fa;
}

.row {
  display: flex;
  gap: 8px;
  margin-top: 8px;
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
  padding: 8px 14px;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn.danger {
  background: #b91c1c;
  color: #fff;
}

.btn:disabled {
  opacity: 0.5;
}

/* меню ⋮ у готового инструмента: лимиты, проверка, старые сессии */
.menu-wrap {
  position: relative;
  flex: none;
}

.menu {
  position: absolute;
  right: 0;
  top: calc(100% + 4px);
  z-index: 20;
  display: flex;
  flex-direction: column;
  min-width: 180px;
  padding: 4px;
  border-radius: 10px;
  background: var(--bg-secondary);
  box-shadow: 0 6px 24px rgba(0, 0, 0, 0.35);
}

.menu button {
  padding: 10px 12px;
  border: none;
  border-radius: 6px;
  background: none;
  color: var(--text-color);
  text-align: left;
  font: inherit;
}

.menu button:disabled {
  opacity: 0.5;
}

.menu-backdrop {
  position: fixed;
  inset: 0;
  z-index: 10;
}

/* содержимое старой сессии */
.sess-log {
  max-height: 50vh;
  overflow-y: auto;
  background: var(--card-color);
  border-radius: 6px;
  padding: 8px 10px;
  margin-bottom: 8px;
}

.sess-msg {
  display: flex;
  gap: 8px;
  font-size: 13px;
  line-height: 1.45;
  text-align: left;
  padding: 4px 0;
  border-bottom: 1px solid var(--hover-bg-color);
}

.sess-msg:last-child {
  border-bottom: none;
}

.sess-role {
  flex: none;
}

.sess-text {
  flex: 1;
  min-width: 0;
  white-space: pre-wrap;
  word-break: break-word;
}

.sess-msg.tool .sess-text {
  color: var(--text-secondary);
  font-family: ui-monospace, monospace;
  font-size: 11px;
}

.sess-msg.user .sess-text {
  font-weight: 600;
}

.usage {
  margin-top: 10px;
  border-top: 1px solid var(--hover-bg-color);
  padding-top: 8px;
}

.u-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
  font-size: 12px;
}

.u-label {
  flex: none;
  width: 132px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-secondary);
}

.u-bar {
  flex: 1;
  height: 8px;
  border-radius: 4px;
  background: var(--bg-secondary);
  overflow: hidden;
}

.u-fill {
  display: block;
  height: 100%;
  border-radius: 4px;
  background: #22c55e;
}

.u-fill.warn {
  background: #f59e0b;
}

.u-fill.crit {
  background: #ef4444;
}

.u-pct {
  flex: none;
  width: 36px;
  text-align: right;
}

.u-reset {
  flex: none;
  color: var(--text-secondary);
}

.task {
  display: block;
  width: 100%;
  text-align: left;
  background: var(--card-color);
  border: none;
  border-radius: 10px;
  padding: 10px 12px;
  margin-bottom: 8px;
  color: var(--text-color);
}

.t-top {
  display: flex;
  align-items: center;
  gap: 8px;
}

.t-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 14px;
}

.t-sub {
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 4px;
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
  resize: vertical;
}

.lbl {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 6px 0 4px;
  text-align: left;
}

.code {
  background: var(--bg-secondary);
  border-radius: 6px;
  padding: 8px 10px;
  font-family: ui-monospace, monospace;
  font-size: 11px;
  white-space: pre-wrap;
  word-break: break-all;
  cursor: pointer;
  text-align: left;
  margin: 4px 0 8px;
}

.params-help {
  text-align: left;
  font-size: 13px;
  margin-bottom: 8px;
}

.params-help summary {
  color: var(--accent-color);
  cursor: pointer;
}

.params-help ul {
  margin: 6px 0;
  padding-left: 18px;
}

.params-help code {
  background: var(--bg-secondary);
  padding: 1px 4px;
  border-radius: 4px;
  font-size: 12px;
}

.modal-content.wide {
  max-width: 720px;
  width: 100%;
}

.run {
  background: var(--bg-secondary);
  border-radius: 8px;
  margin-bottom: 8px;
  overflow: hidden;
}

.run-head {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 10px;
  border: none;
  background: none;
  color: var(--text-color);
  text-align: left;
}

.run-n {
  color: var(--text-secondary);
  font-size: 12px;
  flex: none;
}

.chev {
  color: var(--text-secondary);
  flex: none;
}

.run-prompt {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
}

.run-body {
  padding: 0 10px 10px;
}

.run-prompt-full {
  font-size: 13px;
  color: var(--text-secondary);
  white-space: pre-wrap;
  margin: 0 0 8px;
  text-align: left;
}

.run-out {
  background: var(--card-color);
  border-radius: 6px;
  padding: 8px 10px;
  font-size: 12px;
  line-height: 1.45;
  white-space: pre-wrap;
  word-break: break-word;
  text-align: left;
  max-height: 50vh;
  overflow-y: auto;
  margin: 0 0 6px;
}

.run-out.err {
  color: #ef4444;
}

.run-out.live {
  max-height: 40vh;
  font-size: 11px;
}

.chip.plan {
  background: #4c1d9520;
  color: #a78bfa;
}

.radio {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 6px 0;
  cursor: pointer;
  font-size: 13px;
  text-align: left;
}

.run-md {
  background: var(--card-color);
  border-radius: 6px;
  padding: 4px 10px;
  font-size: 13px;
  text-align: left;
  max-height: 60vh;
  overflow-y: auto;
  margin: 0 0 6px;
}

.sched .t-title {
  cursor: pointer;
}

.t-title.off {
  opacity: 0.5;
  text-decoration: line-through;
}

.sw {
  flex: none;
  display: flex;
}
</style>
