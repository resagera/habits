<script setup lang="ts">
/**
 * Пульт: телефон управляет плеером на ТВ-приставке.
 *
 * Прод здесь только шина — он пересылает JSON и ничего о нём не знает. Списки
 * папок приходят от самой приставки, которая берёт их у своего агента по
 * локальной сети. Поэтому пульт работает и с мобильного интернета, а пути
 * файлов и медиа через сервер не идут.
 */
import { computed, onBeforeUnmount, ref } from 'vue'
import { api } from '../../shared/api/client'
import { showToast } from '../../shared/toast'

interface Room { key: string; label: string; online: boolean }
interface Item {
  id: string
  name: string
  is_dir: boolean
  url?: string
  ready?: boolean
  info?: { verdict: string; vcodec: string; acodec: string; height: number } | null
}
interface Track {
  name: string; index: number; total: number
  paused: boolean; time?: number; duration?: number
}
interface Job {
  id: string; name: string; status: string; percent: number; speed: string; error: string
}

const rooms = ref<Room[]>([])
const address = ref('')
const connected = ref(false)
const tvOnline = ref(false)
const busy = ref(false)

const items = ref<Item[]>([])
const stack = ref<{ id: string; name: string }[]>([])
const loadingList = ref(false)
const video = ref<Track | null>(null)
const music = ref<Track | null>(null)
const jobs = ref<Job[]>([])

let ws: WebSocket | null = null
let retry: ReturnType<typeof setTimeout> | null = null
let closing = false
// Ключ комнаты запоминаем отдельно от кода: код одноразово вводят руками, а
// переподключаться после обрыва надо по ключу — код к тому времени уже не
// набран, да и заново набирать его никто не будет.
let lastKey = ''

async function loadRooms() {
  try {
    const r = await api.get<{ rooms: Room[] }>('/tv/rooms')
    rooms.value = r.rooms
    if (!address.value && r.rooms.length) address.value = r.rooms[0].key
  } catch {
    /* пустой список — не ошибка */
  }
}
void loadRooms()

/**
 * Подключение. Первый раз — по коду с экрана приставки: адрес компьютера знать
 * не нужно, а по DHCP он ещё и меняется. Дальше приставка сохраняется, и её
 * достаточно выбрать из списка — тогда сюда приходит ключ комнаты.
 */
async function connect(key?: string) {
  const body = key ? { key } : { code: address.value }
  if (!key && !address.value.trim()) {
    showToast('Введите код с экрана приставки')
    return
  }
  busy.value = true
  closing = false
  lastKey = key ?? ''
  try {
    const r = await api.post<{ ticket: string; online: boolean; room: Room }>('/tv/attach', body)
    tvOnline.value = r.online
    lastKey = r.room.key
    address.value = ''
    openSocket(r.ticket)
    void loadRooms()
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось подключиться')
  } finally {
    busy.value = false
  }
}

function openSocket(ticket: string) {
  ws?.close()
  // Пропуск одноразовый: заголовок Authorization в веб-сокете из браузера не
  // поставить, поэтому право на комнату подтверждается им.
  const base = import.meta.env.BASE_URL + 'api/v1/tv/remote/' + ticket
  const url = new URL(base, location.href)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  ws = new WebSocket(url.toString())
  ws.onopen = () => {
    connected.value = true
    send({ t: 'hello' })
    browse('')
  }
  ws.onclose = () => {
    connected.value = false
    if (!closing) scheduleRetry()
  }
  ws.onmessage = (e) => {
    let m: Record<string, unknown>
    try { m = JSON.parse(e.data as string) } catch { return }
    onMessage(m)
  }
}

/** Телефон засыпает и рвёт соединение — без переподключения пульт «умирает». */
function scheduleRetry() {
  if (retry) clearTimeout(retry)
  retry = setTimeout(() => { if (!closing && lastKey) void connect(lastKey) }, 3000)
}

function onMessage(m: Record<string, unknown>) {
  switch (m.t) {
    case 'presence':
      tvOnline.value = Number(m.tv) > 0
      break
    case 'items':
      loadingList.value = false
      if (m.error) showToast(String(m.error))
      items.value = (m.items as Item[]) ?? []
      break
    case 'state':
      video.value = (m.video as Track) ?? null
      music.value = (m.music as Track) ?? null
      break
    case 'jobs':
      jobs.value = (m.jobs as Job[]) ?? []
      break
  }
}

function send(obj: Record<string, unknown>) {
  if (ws && ws.readyState === 1) ws.send(JSON.stringify(obj))
  else showToast('Нет связи с приставкой')
}

function browse(id: string) {
  loadingList.value = true
  send({ t: 'browse', id })
}

function openDir(it: Item) {
  stack.value.push({ id: it.id, name: it.name })
  browse(it.id)
}

function goUp() {
  stack.value.pop()
  const parent = stack.value[stack.value.length - 1]
  browse(parent ? parent.id : '')
}

function playable(it: Item) {
  return it.ready || it.info?.verdict === 'ok'
}

/** Есть ли в файле картинка. Без видеодорожки его место в музыкальном плеере. */
function hasVideo(it: Item) { return !!it.info?.vcodec }

function open(it: Item, as?: 'music') {
  if (it.is_dir) { openDir(it); return }
  if (!playable(it)) { send({ t: 'transcode', id: it.id, name: it.name }); showToast('Поставил в очередь на перекодирование'); return }
  // Вся папка уходит очередью: сериал смотрят подряд, следующая серия должна
  // начинаться сама. Приставка соберёт её из того же списка, что видим мы, и
  // сама решит по содержимому, куда играть — в видео или в музыку.
  send({ t: 'open', id: it.id, as, items: items.value })
}

const path = computed(() => stack.value.map((s) => s.name).join(' / ') || 'Библиотеки')
const fmt = (t?: number) => {
  if (!t || !isFinite(t)) return '0:00'
  const s = Math.round(t)
  const m = Math.floor(s / 60)
  return `${Math.floor(m / 60) ? Math.floor(m / 60) + ':' + String(m % 60).padStart(2, '0') : m}:${String(s % 60).padStart(2, '0')}`
}

function cmd(a: string) { send({ t: 'cmd', a }) }
function mcmd(a: string) { send({ t: 'mcmd', a }) }
function seek(e: Event) {
  const to = Number((e.target as HTMLInputElement).value)
  send({ t: 'seek', to })
}

function disconnect() {
  closing = true
  if (retry) clearTimeout(retry)
  ws?.close()
  ws = null
  connected.value = false
}

async function forget(room: Room) {
  if (!confirm(`Убрать приставку ${room.key}?`)) return
  try {
    await api.delete(`/tv/rooms/${encodeURIComponent(room.key)}`)
    await loadRooms()
  } catch { showToast('Не удалось убрать') }
}

onBeforeUnmount(disconnect)
</script>

<template>
  <div class="page">
    <h2>📺 Пульт ТВ</h2>

    <div v-if="!connected" class="card">
      <p class="hint">
        Откройте на приставке страницу плеера — на ней крупно показан код.
        Введите его сюда, и приставка запомнится: дальше подключение в одно
        нажатие.
      </p>
      <div class="row">
        <input v-model="address" class="code" placeholder="ABCD 1234"
               autocapitalize="characters" autocomplete="off" spellcheck="false"
               @keyup.enter="connect()" />
        <button class="btn" :disabled="busy" @click="connect()">Подключиться</button>
      </div>
      <div v-if="rooms.length" class="rooms">
        <div v-for="r in rooms" :key="r.key" class="room">
          <button class="link" @click="connect(r.key)">
            {{ r.label || r.key }}
            <span :class="['dot', r.online ? 'on' : 'off']"></span>
          </button>
          <button class="mini" @click="forget(r)">✕</button>
        </div>
      </div>
    </div>

    <template v-else>
      <div class="status">
        <span :class="['dot', tvOnline ? 'on' : 'off']"></span>
        {{ tvOnline ? 'Приставка на связи' : 'Приставка не отвечает — откройте на ней плеер' }}
        <button class="link" @click="disconnect">отключиться</button>
      </div>

      <div class="card player">
        <b>{{ video ? video.name : 'Ничего не играет' }}</b>
        <div v-if="video" class="meta">Серия {{ video.index }} из {{ video.total }}</div>
        <input v-if="video" class="scrub" type="range" min="0" :max="Math.max(1, video.duration || 1)"
               :value="video.time || 0" step="1" @change="seek" />
        <div v-if="video" class="meta">{{ fmt(video.time) }} / {{ fmt(video.duration) }}</div>
        <div class="pad">
          <button @click="cmd('prev')">⏮</button>
          <button @click="cmd('back')">⏪ 30</button>
          <button class="wide" @click="cmd('playpause')">{{ video && !video.paused ? '⏸' : '▶' }}</button>
          <button @click="cmd('fwd')">⏩ 30</button>
          <button @click="cmd('next')">⏭</button>
        </div>
        <div class="pad">
          <button @click="cmd('stop')">⏹ Стоп</button>
          <button @click="cmd('full')">⛶ Во весь экран</button>
        </div>
      </div>

      <div class="card">
        <b>🎵 {{ music ? music.name : 'Музыка не выбрана' }}</b>
        <div class="pad">
          <button @click="mcmd('prev')">⏮</button>
          <button class="wide" @click="mcmd('playpause')">{{ music && !music.paused ? '⏸' : '▶' }}</button>
          <button @click="mcmd('next')">⏭</button>
          <button @click="mcmd('stop')">⏹</button>
        </div>
        <p class="hint">
          Музыка ставит видео на паузу и не сбивает его место — вернётесь ровно туда,
          где остановились.
        </p>
      </div>

      <div class="card">
        <div class="crumbs">
          <button v-if="stack.length" class="mini" @click="goUp">← Назад</button>
          <span class="meta">{{ path }}</span>
        </div>
        <div v-if="loadingList" class="hint">Загружаю…</div>
        <div v-for="it in items" :key="it.id" class="item">
          <button class="name" @click="open(it)">
            {{ it.is_dir ? '📁 ' : (!hasVideo(it) ? '🎵 ' : '🎬 ') }}{{ it.name }}
            <span v-if="!it.is_dir && !playable(it)" class="warn">
              ⚠ нужен MP4 — нажмите, чтобы перекодировать
            </span>
          </button>
          <!-- фильм можно пустить фоном как звук; у файла без картинки такой
               кнопки нет — он и так уйдёт в музыку -->
          <button v-if="!it.is_dir && playable(it) && hasVideo(it)" class="mini"
                  title="Играть фоном как музыку" @click="open(it, 'music')">🎵</button>
        </div>
        <p v-if="!loadingList && !items.length" class="hint">Пусто</p>
      </div>

      <div v-if="jobs.length" class="card">
        <b>Перекодирование</b>
        <div v-for="j in jobs.slice(0, 6)" :key="j.id" class="job">
          {{ j.name }} — {{ j.status === 'running' ? j.percent + '% ' + (j.speed || '') : j.status }}
          <span v-if="j.error" class="warn">{{ j.error }}</span>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.page { padding: 12px; display: flex; flex-direction: column; gap: 12px; }
h2 { margin: 0; font-size: 20px; }
.card { background: var(--card-color); border-radius: 12px; padding: 12px; display: flex;
  flex-direction: column; gap: 8px; }
.hint { color: var(--text-secondary); font-size: 13px; margin: 0; }
.meta { color: var(--text-secondary); font-size: 13px; }
.row { display: flex; gap: 8px; }
.row input { flex: 1; min-width: 0; }
/* Код набирают вручную — крупно и моноширинно, чтобы не путать знаки. */
.code { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 18px;
  letter-spacing: 3px; text-transform: uppercase; }
input { background: var(--bg-secondary); color: var(--text-color); border: 1px solid var(--border-color);
  border-radius: 8px; padding: 10px; font: inherit; }
.btn { background: var(--accent-color, #4c8dff); color: #fff; border: 0; border-radius: 8px;
  padding: 10px 14px; font: inherit; }
.status { display: flex; align-items: center; gap: 8px; font-size: 14px; }
.dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; }
.dot.on { background: #46c46b; }
.dot.off { background: #d05050; }
/* Кнопки крупные: пультом пользуются в темноте и не глядя. */
.pad { display: flex; gap: 6px; flex-wrap: wrap; }
.pad button { flex: 1; min-width: 56px; padding: 14px 8px; font-size: 16px;
  background: var(--bg-secondary); color: var(--text-color);
  border: 1px solid var(--border-color); border-radius: 10px; }
.pad button.wide { flex: 2; }
.scrub { width: 100%; }
.crumbs { display: flex; align-items: center; gap: 8px; }
.item { display: flex; align-items: center; gap: 6px; border-bottom: 1px solid var(--border-color); }
.item:last-child { border-bottom: 0; }
.name { flex: 1; min-width: 0; text-align: left; background: none; border: 0; color: var(--text-color);
  font: inherit; padding: 10px 2px; overflow: hidden; text-overflow: ellipsis; }
.warn { display: block; color: #e3b341; font-size: 12px; }
.mini { background: none; border: 0; color: var(--text-secondary); font: inherit; padding: 8px; }
.link { background: none; border: 0; color: var(--accent-color, #4c8dff); font: inherit; padding: 0; }
.room { display: flex; align-items: center; gap: 8px; }
.job { font-size: 13px; color: var(--text-secondary); }
</style>
