<script setup lang="ts">
import { nextTick, onUnmounted, ref } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { showToast } from '../../shared/toast'
import * as termApi from './api'
import type { TerminalMachine } from './types'

const machines = ref<TerminalMachine[]>([])
const loading = ref(true)

async function loadAll() {
  try {
    machines.value = (await termApi.fetchMachines()).machines
  } catch {
    showToast('Не удалось загрузить машины')
  } finally {
    loading.value = false
  }
}
loadAll()

// --- добавление / настройки машины ---
const modal = ref(false)
const editing = ref<TerminalMachine | null>(null)
const form = ref({ name: '' })
const confirmDelete = ref(false)

const INSTALL_PREFIX =
  'curl -fsSL https://raw.githubusercontent.com/resagera/habits-agent/main/install-term.sh | sudo bash -s -- '

function installCmd(m: TerminalMachine): string {
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

function openCreate() {
  editing.value = null
  form.value = { name: '' }
  confirmDelete.value = false
  modal.value = true
}
function openSettings(m: TerminalMachine) {
  editing.value = m
  form.value = { name: m.name }
  confirmDelete.value = false
  modal.value = true
}

async function save() {
  const name = form.value.name.trim()
  if (!name) {
    showToast('Имя обязательно')
    return
  }
  try {
    if (editing.value) {
      const { machine } = await termApi.renameMachine(editing.value.id, name)
      const i = machines.value.findIndex((x) => x.id === machine.id)
      if (i >= 0) machines.value[i] = machine
      modal.value = false
    } else {
      const { machine } = await termApi.createMachine(name)
      machines.value.push(machine)
      editing.value = machine // показать токен и команду установки
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
    await termApi.deleteMachine(editing.value.id)
    machines.value = machines.value.filter((x) => x.id !== editing.value!.id)
    modal.value = false
  } catch {
    showToast('Не удалось удалить')
  }
}

// --- консоль ---
const termOpen = ref(false)
const termName = ref('')
const termStatus = ref<'connecting' | 'online' | 'closed'>('connecting')
const ctrlSticky = ref(false)
const termHost = ref<HTMLDivElement | null>(null)

let term: Terminal | null = null
let fit: FitAddon | null = null
let ws: WebSocket | null = null
let activeMachine: TerminalMachine | null = null

function isDark(): boolean {
  return document.documentElement.dataset.theme !== 'light'
}

function termTheme() {
  return isDark()
    ? { background: '#0c0c0c', foreground: '#e0e0e0', cursor: '#e0e0e0' }
    : { background: '#ffffff', foreground: '#1a1a1a', cursor: '#1a1a1a' }
}

function send(data: string) {
  if (ws && ws.readyState === WebSocket.OPEN) ws.send(new TextEncoder().encode(data))
}

function sendResize() {
  if (term && ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ t: 'resize', cols: term.cols, rows: term.rows }))
  }
}

async function openConsole(m: TerminalMachine) {
  activeMachine = m
  termName.value = m.name
  termOpen.value = true
  termStatus.value = 'connecting'
  await nextTick()

  term = new Terminal({
    fontSize: 13,
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
    cursorBlink: true,
    theme: termTheme(),
    scrollback: 5000,
  })
  fit = new FitAddon()
  term.loadAddon(fit)
  term.open(termHost.value!)
  fit.fit()

  // ввод: с учётом «липкого» Ctrl (Ctrl+буква на мобилке)
  term.onData((d) => {
    if (ctrlSticky.value && d.length === 1) {
      const code = d.toLowerCase().charCodeAt(0)
      if (code >= 97 && code <= 122) d = String.fromCharCode(code - 96)
      ctrlSticky.value = false
    }
    send(d)
  })
  term.onResize(() => sendResize())

  await connect()
}

async function connect() {
  if (!activeMachine) return
  termStatus.value = 'connecting'
  try {
    const { ticket } = await termApi.openSession(activeMachine.id)
    ws = new WebSocket(termApi.streamWsUrl(ticket))
    ws.binaryType = 'arraybuffer'
    ws.onopen = () => {
      termStatus.value = 'online'
      fit?.fit()
      sendResize()
      term?.focus()
    }
    ws.onmessage = (e) => {
      if (e.data instanceof ArrayBuffer) term?.write(new Uint8Array(e.data))
    }
    ws.onclose = () => {
      termStatus.value = 'closed'
    }
    ws.onerror = () => {
      termStatus.value = 'closed'
    }
  } catch (e) {
    termStatus.value = 'closed'
    const msg = e instanceof Error ? e.message : ''
    term?.write(`\r\n\x1b[31m${msg.includes('offline') ? 'Машина офлайн — агент не на связи.' : 'Не удалось открыть сессию.'}\x1b[0m\r\n`)
  }
}

function reconnect() {
  term?.clear()
  connect()
}

function closeConsole() {
  ws?.close()
  ws = null
  term?.dispose()
  term = null
  fit = null
  activeMachine = null
  ctrlSticky.value = false
  termOpen.value = false
}

// спецклавиши для мобильной клавиатуры
const KEYS: { label: string; seq: string }[] = [
  { label: 'Esc', seq: '\x1b' },
  { label: 'Tab', seq: '\t' },
  { label: '↑', seq: '\x1b[A' },
  { label: '↓', seq: '\x1b[B' },
  { label: '←', seq: '\x1b[D' },
  { label: '→', seq: '\x1b[C' },
  { label: '^C', seq: '\x03' },
  { label: '^D', seq: '\x04' },
  { label: '^Z', seq: '\x1a' },
  { label: '^L', seq: '\x0c' },
  { label: '|', seq: '|' },
  { label: '~', seq: '~' },
  { label: '/', seq: '/' },
]

function pressKey(seq: string) {
  send(seq)
  term?.focus()
}

function toggleCtrl() {
  ctrlSticky.value = !ctrlSticky.value
  term?.focus()
}

function onResizeWindow() {
  if (termOpen.value && fit) {
    fit.fit()
    sendResize()
  }
}
window.addEventListener('resize', onResizeWindow)
onUnmounted(() => {
  window.removeEventListener('resize', onResizeWindow)
  closeConsole()
})
</script>

<template>
  <div v-if="loading" class="hint">Загрузка…</div>

  <template v-else>
    <p v-if="machines.length === 0" class="hint">
      Консоль к домашней машине из любой точки. Поставьте на машину shell-агент
      (кнопка «Добавить машину» даёт токен и команду установки) — и открывайте
      терминал прямо здесь, с мобилки 👇
    </p>
    <p class="warn-note">
      ⚠️ Даёт полный доступ к shell на машине под пользователем агента. Ставьте
      только на свои машины и храните токен в секрете.
    </p>

    <div v-for="m in machines" :key="m.id" class="tm-card">
      <div class="tm-head">
        <div class="tm-title">
          <span class="dot" :class="{ ok: m.online }" />
          ⌨️ {{ m.name }}
        </div>
        <span>
          <span v-if="!m.online" class="offline">офлайн</span>
          <button class="icon-btn" title="Настройки" @click="openSettings(m)">⚙️</button>
        </span>
      </div>
      <button class="open-btn" :disabled="!m.online" @click="openConsole(m)">
        {{ m.online ? '▶ Открыть консоль' : 'Агент не на связи' }}
      </button>
    </div>

    <button class="add-btn" @click="openCreate">＋ Добавить машину</button>
  </template>

  <!-- Модалка машины -->
  <div v-if="modal" class="modal" @click.self="modal = false">
    <div class="modal-content tm-modal">
      <h3>{{ editing ? 'Машина' : 'Добавить машину' }}</h3>
      <input v-model="form.name" placeholder="Имя (например, домашний ПК)" maxlength="100" />

      <template v-if="editing && editing.token">
        <div class="push-block">
          <div class="push-label">Токен агента</div>
          <code class="push-code" @click="copyText(editing.token)">{{ editing.token }}</code>
          <div class="push-label">Установка на машине (Linux)</div>
          <code class="push-code" @click="copyText(installCmd(editing))">{{ installCmd(editing) }}</code>
          <p class="kind-hint">
            Нажмите, чтобы скопировать. Агент запустится от вашего пользователя и
            даст доступ к его shell. Токен = полный доступ, храните в секрете.
          </p>
        </div>
      </template>

      <button class="btn primary" @click="save">💾 Сохранить</button>
      <button v-if="editing" class="btn danger" @click="removeMachine">
        {{ confirmDelete ? 'Точно удалить?' : '🗑 Удалить машину' }}
      </button>
      <button class="btn" @click="modal = false">{{ editing ? 'Закрыть' : 'Отмена' }}</button>
    </div>
  </div>

  <!-- Полноэкранная консоль -->
  <div v-if="termOpen" class="console">
    <div class="console-head">
      <span class="console-title">
        <span class="dot" :class="{ ok: termStatus === 'online' }" />
        {{ termName }}
        <span class="console-status">
          {{ termStatus === 'online' ? '' : termStatus === 'connecting' ? 'подключение…' : 'отключено' }}
        </span>
      </span>
      <span>
        <button v-if="termStatus === 'closed'" class="icon-btn" title="Переподключиться" @click="reconnect">🔄</button>
        <button class="icon-btn" title="Закрыть" @click="closeConsole">✕</button>
      </span>
    </div>
    <div ref="termHost" class="console-body"></div>
    <div class="keybar">
      <button class="keybtn" :class="{ on: ctrlSticky }" @click="toggleCtrl">Ctrl</button>
      <button v-for="k in KEYS" :key="k.label" class="keybtn" @click="pressKey(k.seq)">{{ k.label }}</button>
    </div>
  </div>
</template>

<style scoped>
.hint {
  text-align: center;
  color: var(--text-secondary);
  padding: 24px 0;
}
.warn-note {
  font-size: 12px;
  color: #f59e0b;
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 8px 10px;
  margin: 0 0 12px;
}

.tm-card {
  background: var(--card-color);
  border-radius: 12px;
  padding: 10px 14px;
  margin-bottom: 12px;
}
.tm-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.tm-title {
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
  flex-shrink: 0;
}
.dot.ok {
  background: #22c55e;
}
.offline {
  font-size: 11px;
  color: var(--text-secondary);
  margin-right: 6px;
}
.icon-btn {
  background: none;
  border: none;
  padding: 4px 6px;
}
.open-btn {
  display: block;
  width: 100%;
  margin-top: 10px;
  padding: 9px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
}
.open-btn:disabled {
  opacity: 0.5;
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

.tm-modal {
  text-align: left;
}
.tm-modal h3 {
  text-align: center;
}
.tm-modal input {
  width: 100%;
  margin-top: 8px;
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
.kind-hint {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 8px 0 0;
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

/* полноэкранная консоль поверх всего */
.console {
  position: fixed;
  inset: 0;
  z-index: 3000;
  display: flex;
  flex-direction: column;
  background: v-bind("isDark() ? '#0c0c0c' : '#ffffff'");
}
.console-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 12px;
  background: var(--bg-secondary);
  color: var(--text-color);
  flex-shrink: 0;
}
.console-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  font-size: 14px;
}
.console-status {
  font-size: 12px;
  color: var(--text-secondary);
}
.console-body {
  flex: 1;
  min-height: 0;
  padding: 4px 6px;
  overflow: hidden;
}
.keybar {
  display: flex;
  gap: 5px;
  padding: 6px 8px;
  overflow-x: auto;
  background: var(--bg-secondary);
  flex-shrink: 0;
  -webkit-overflow-scrolling: touch;
}
.keybtn {
  flex-shrink: 0;
  min-width: 38px;
  padding: 8px 10px;
  border: none;
  border-radius: 7px;
  background: var(--hover-bg-color);
  color: var(--text-color);
  font-size: 13px;
  font-family: ui-monospace, monospace;
}
.keybtn.on {
  background: var(--accent-color);
  color: #fff;
}
</style>
