<script setup lang="ts">
// Лабиринт. Поле рисуется в canvas: у «гигантского» размера сорок тысяч
// клеток, DOM столько не вынесет, а рисовать нужно только видимое.
//
// Стенки — сплошная сетка полос, поверх которой «прорезаются» проходы
// (clearRect). Холст прозрачный, поэтому в проходах виден фон игры, а
// прорезать дешевле, чем рисовать каждую стенку отдельным прямоугольником.
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { haptic, hapticNotify } from '../../../shared/telegram'
import { api } from '../../../shared/api/client'
import { resolveBgUrl } from '../../../shared/background'
import { showToast } from '../../../shared/toast'
import { fmtTime, type GameResult, type GameScore } from '../types'
import { imagePattern, wallPattern, WALL_STYLES } from '../maze/walls'
import { emptyStick, knobOffset, type Stick } from '../maze/controls'

const props = defineProps<{ scores?: GameScore[] }>()
const emit = defineEmits<{ finish: [result: GameResult]; settings: [] }>()

interface Maze {
  seed: number
  cols: number
  rows: number
  cells: string[]
  start: [number, number]
  goal: [number, number]
  shortest: number
}

interface Texture {
  id: number
  title: string
  url: string
}

/** Размеры: во сколько раз лабиринт больше того, что помещается в экран. */
const SIZES = [
  { code: 'normal', title: 'Обычный', hint: 'весь помещается в экран', mul: 1 },
  { code: 'big', title: 'Большой', hint: 'вдвое больше экрана, поле едет за точкой', mul: 2 },
  { code: 'huge', title: 'Гигантский', hint: 'в десять раз больше — надолго', mul: 10 },
] as const

/** Масштаб: ширина прохода и толщина стенки в пикселях. */
const SCALES = [
  { code: 's', title: 'Мелкий', pass: 15, wall: 11 },
  { code: 'm', title: 'Обычный', pass: 21, wall: 15 },
  { code: 'l', title: 'Крупный', pass: 29, wall: 21 },
]

/** Скорость точки в клетках в секунду. */
const SPEEDS = [
  { code: 'slow', title: 'Шагом', cells: 3.5 },
  { code: 'normal', title: 'Обычная', cells: 6 },
  { code: 'fast', title: 'Быстро', cells: 10 },
]

const PREF_KEY = 'maze_prefs'

const prefs = ref<{ scale: string; speed: string }>({ scale: 'm', speed: 'normal' })
try {
  Object.assign(prefs.value, JSON.parse(localStorage.getItem(PREF_KEY) || '{}'))
} catch {
  /* битые настройки — остаёмся на умолчаниях */
}

const scale = computed(() => SCALES.find((s) => s.code === prefs.value.scale) ?? SCALES[1])
const speed = computed(() => SPEEDS.find((s) => s.code === prefs.value.speed) ?? SPEEDS[1])
const pitch = computed(() => scale.value.pass + scale.value.wall)

const sizeCode = ref('')
const maze = ref<Maze | null>(null)
const loading = ref(false)
const seconds = ref(0)
const done = ref(false)
const explored = ref(0)
const textures = ref<Texture[]>([])
const styleTitle = ref('')

const box = ref<HTMLDivElement>()
const canvas = ref<HTMLCanvasElement>()
const stickEl = ref<HTMLDivElement>()

let ctx: CanvasRenderingContext2D | null = null
let pattern: CanvasPattern | null = null
let visited: Uint8Array = new Uint8Array(0)
let dead: Uint8Array = new Uint8Array(0)
/** стек текущего пути: по нему видно, из какой ветки мы вернулись */
let path: number[] = []
let pos = { x: 0, y: 0 }
let dir = { x: 0, y: 0 }
let stick: Stick = emptyStick()
/** цель шага по кнопке-крестику (центр соседней клетки) */
let stepTarget: { x: number; y: number } | null = null
let raf = 0
let last = 0
let elapsedMs = 0
let running = false
let view = { w: 0, h: 0 }
let resizeTimer: ReturnType<typeof setTimeout> | undefined

const knob = ref({ x: 0, y: 0 })

function bestOf(code: string): string {
  const s = props.scores?.find((x) => x.game === `maze:${code}`)
  return s?.best ? `рекорд ${fmtTime(s.best)}` : ''
}

onMounted(async () => {
  window.addEventListener('keydown', onKeyDown)
  window.addEventListener('resize', onResize)
  window.addEventListener('orientationchange', onResize)
  try {
    const res = await api.get<{ textures: Texture[] }>('/games/textures?game=maze')
    textures.value = res.textures ?? []
  } catch {
    /* текстур нет — остаются нарисованные кодом стили */
  }
})

onUnmounted(() => {
  stickUp()
  cancelAnimationFrame(raf)
  clearTimeout(resizeTimer)
  window.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('resize', onResize)
  window.removeEventListener('orientationchange', onResize)
})

/** Сколько клеток помещается в окно при обычном масштабе — от этого все размеры. */
function fitCells() {
  const el = box.value
  const w = el?.clientWidth ?? 320
  const h = Math.max(240, window.innerHeight - (el?.getBoundingClientRect().top ?? 160) - 80)
  const base = SCALES[1].pass + SCALES[1].wall
  return {
    cols: Math.max(5, Math.floor((w - SCALES[1].wall) / base)),
    rows: Math.max(5, Math.floor((h - SCALES[1].wall) / base)),
    w,
    h,
  }
}

async function start(code: string) {
  const size = SIZES.find((s) => s.code === code)
  if (!size || loading.value) return
  loading.value = true
  try {
    const fit = fitCells()
    view = { w: fit.w, h: fit.h }
    const m = await api.get<Maze>(
      `/games/maze?cols=${fit.cols * size.mul}&rows=${fit.rows * size.mul}`,
    )
    maze.value = m
    sizeCode.value = code
    visited = new Uint8Array(m.cols * m.rows)
    dead = new Uint8Array(m.cols * m.rows)
    path = [m.start[1] * m.cols + m.start[0]]
    visited[path[0]] = 1
    explored.value = 1
    pos = { x: centerOf(m.start[0]), y: centerOf(m.start[1]) }
    dir = { x: 0, y: 0 }
    stepTarget = null
    stick = emptyStick()
    knob.value = { x: 0, y: 0 }
    seconds.value = 0
    elapsedMs = 0
    running = false
    done.value = false
    await setupCanvas()
  } catch {
    showToast('Не удалось построить лабиринт')
  } finally {
    loading.value = false
  }
}

/** Стиль стен: нарисованные кодом плитки вперемешку с теми, что загрузил админ. */
async function pickWallStyle() {
  const c = ctx
  if (!c) return
  const total = WALL_STYLES.length + textures.value.length
  const i = Math.floor(Math.random() * total)
  if (i < WALL_STYLES.length) {
    pattern = wallPattern(c, WALL_STYLES[i].code)
    styleTitle.value = WALL_STYLES[i].title
    return
  }
  const t = textures.value[i - WALL_STYLES.length]
  styleTitle.value = t.title || 'Текстура'
  pattern = wallPattern(c, WALL_STYLES[0].code) // пока грузится картинка
  const loaded = await imagePattern(c, resolveBgUrl(t.url))
  if (loaded) pattern = loaded
}

async function setupCanvas() {
  await new Promise((r) => setTimeout(r, 0)) // ждём, пока canvas появится в DOM
  const c = canvas.value
  if (!c) return
  sizeCanvas()
  ctx = c.getContext('2d')
  if (!ctx) return
  await pickWallStyle()
  last = performance.now()
  cancelAnimationFrame(raf)
  raf = requestAnimationFrame(loop)
}

function sizeCanvas() {
  const c = canvas.value
  if (!c || !ctx) {
    if (!c) return
  }
  const dpr = Math.min(window.devicePixelRatio || 1, 2)
  c!.width = Math.round(view.w * dpr)
  c!.height = Math.round(view.h * dpr)
  c!.style.width = `${view.w}px`
  c!.style.height = `${view.h}px`
  const g = c!.getContext('2d')
  if (g) {
    g.setTransform(dpr, 0, 0, dpr, 0, 0)
    ctx = g
  }
}

/** Поворот телефона и смена размера окна: поле пересчитывается, партия живёт. */
function onResize() {
  if (!maze.value) return
  clearTimeout(resizeTimer)
  resizeTimer = setTimeout(() => {
    const fit = fitCells()
    view = { w: fit.w, h: fit.h }
    sizeCanvas()
  }, 150)
}

const centerOf = (i: number) => scale.value.wall + i * pitch.value + scale.value.pass / 2
const cellAt = (v: number) => Math.round((v - scale.value.wall - scale.value.pass / 2) / pitch.value)

function openSides(x: number, y: number): number {
  const m = maze.value
  if (!m || y < 0 || y >= m.rows || x < 0 || x >= m.cols) return 0
  return parseInt(m.cells[y][x], 16)
}

/** Есть ли из клетки выход, где мы ещё не были. */
function hasUnvisitedExit(index: number): boolean {
  const m = maze.value
  if (!m) return false
  const x = index % m.cols
  const y = Math.floor(index / m.cols)
  const sides = openSides(x, y)
  const steps: [number, number, number][] = [
    [0, -1, 1], [1, 0, 2], [0, 1, 4], [-1, 0, 8],
  ]
  return steps.some(([dx, dy, bit]) => {
    if (!(sides & bit)) return false
    const nx = x + dx
    const ny = y + dy
    if (nx < 0 || ny < 0 || nx >= m.cols || ny >= m.rows) return false
    return !visited[ny * m.cols + nx]
  })
}

/**
 * Вошли в клетку. Если это шаг назад по своему же следу, ветка, из которой
 * вернулись, тускнеет — но только когда в ней не осталось неисследованных
 * выходов: иначе туда ещё имеет смысл заглянуть.
 */
function enterCell(x: number, y: number) {
  const m = maze.value
  if (!m) return
  const i = y * m.cols + x
  if (path.length >= 2 && path[path.length - 2] === i) {
    const left = path.pop()!
    if (!hasUnvisitedExit(left)) dead[left] = 1
  } else {
    path.push(i)
  }
  if (!visited[i]) {
    visited[i] = 1
    explored.value += 1
  }
}

const canGo = (sides: number, dx: number, dy: number) =>
  (dx === 1 && (sides & 2) !== 0) || (dx === -1 && (sides & 8) !== 0) ||
  (dy === 1 && (sides & 4) !== 0) || (dy === -1 && (sides & 1) !== 0)

/**
 * Куда ехать из центра клетки. Порядок попыток:
 *   1. куда отклонён джойстик сильнее всего;
 *   2. дальше по коридору, если туда ещё можно (как в Пакмане);
 *   3. вторая ось джойстика, если он отклонён по диагонали;
 *   4. единственный выход, кроме «назад» — изгибы коридора проходятся сами.
 *
 * Без пунктов 2 и 4 вести точку одним пальцем невыносимо: держишь вправо,
 * коридор свернул вниз — и она встала намертво.
 */
function pickDir(sides: number): { x: number; y: number } | null {
  const horizontalFirst = Math.abs(stick.vec.x) >= Math.abs(stick.vec.y)
  const primary = horizontalFirst
    ? { x: Math.sign(stick.vec.x), y: 0 }
    : { x: 0, y: Math.sign(stick.vec.y) }
  const secondary = horizontalFirst
    ? { x: 0, y: Math.sign(stick.vec.y) }
    : { x: Math.sign(stick.vec.x), y: 0 }

  if ((primary.x || primary.y) && canGo(sides, primary.x, primary.y)) return primary
  if ((dir.x || dir.y) && canGo(sides, dir.x, dir.y)) return dir
  if ((secondary.x || secondary.y) && canGo(sides, secondary.x, secondary.y)) return secondary

  const exits = ([[1, 0], [-1, 0], [0, 1], [0, -1]] as [number, number][])
    .filter(([dx, dy]) => canGo(sides, dx, dy) && !(dx === -dir.x && dy === -dir.y))
  return exits.length === 1 ? { x: exits[0][0], y: exits[0][1] } : null
}

function step(dt: number) {
  const m = maze.value
  if (!m || done.value) return
  const cx = cellAt(pos.x)
  const cy = cellAt(pos.y)
  const sides = openSides(cx, cy)
  const move = speed.value.cells * pitch.value * dt

  // шаг по кнопке-крестику: едем ровно до центра соседней клетки
  if (stepTarget) {
    const dx = stepTarget.x - pos.x
    const dy = stepTarget.y - pos.y
    const dist = Math.hypot(dx, dy)
    running = true
    if (dist <= move) {
      pos = { ...stepTarget }
      stepTarget = null
      dir = { x: 0, y: 0 }
    } else {
      pos.x += (dx / dist) * move
      pos.y += (dy / dist) * move
    }
    afterMove(cx, cy)
    return
  }

  if (!stick.active) {
    dir = { x: 0, y: 0 }
    return
  }

  const atCenter =
    Math.abs(pos.x - centerOf(cx)) < 0.8 && Math.abs(pos.y - centerOf(cy)) < 0.8
  if (atCenter) {
    const d = pickDir(sides)
    if (!d) {
      dir = { x: 0, y: 0 }
      return
    }
    dir = d
    if (dir.x) pos.y = centerOf(cy)
    if (dir.y) pos.x = centerOf(cx)
  }
  if (!dir.x && !dir.y) return

  running = true
  let travel = move
  if (!canGo(sides, dir.x, dir.y)) {
    const avail = dir.x ? (centerOf(cx) - pos.x) * dir.x : (centerOf(cy) - pos.y) * dir.y
    if (avail <= 0) {
      dir = { x: 0, y: 0 }
      return
    }
    travel = Math.min(travel, avail)
  }
  pos.x += dir.x * travel
  pos.y += dir.y * travel
  afterMove(cx, cy)
}

function afterMove(fromX: number, fromY: number) {
  const m = maze.value
  if (!m) return
  const nx = cellAt(pos.x)
  const ny = cellAt(pos.y)
  if (nx !== fromX || ny !== fromY) {
    enterCell(nx, ny)
    haptic('light')
  }
  if (nx === m.goal[0] && ny === m.goal[1]) win()
}

function win() {
  const m = maze.value
  if (!m) return
  done.value = true
  running = false
  hapticNotify('success')
  emit('finish', {
    best: seconds.value,
    detail: { cells: explored.value, shortest: m.shortest },
    variant: sizeCode.value,
  })
}

function loop(now: number) {
  // вкладка «просыпается» — не телепортируемся и не досчитываем время
  const dt = Math.min((now - last) / 1000, 0.05)
  last = now
  if (running && !done.value) {
    elapsedMs += dt * 1000
    const s = Math.floor(elapsedMs / 1000)
    if (s !== seconds.value) seconds.value = s
  }
  step(dt)
  draw()
  raf = requestAnimationFrame(loop)
}

const clamp = (v: number, a: number, b: number) => Math.max(a, Math.min(v, b))

function camera() {
  const m = maze.value!
  const { wall } = scale.value
  const p = pitch.value
  const worldW = m.cols * p + wall
  const worldH = m.rows * p + wall
  return {
    x: worldW <= view.w ? (worldW - view.w) / 2 : clamp(pos.x - view.w / 2, 0, worldW - view.w),
    y: worldH <= view.h ? (worldH - view.h) / 2 : clamp(pos.y - view.h / 2, 0, worldH - view.h),
    worldW,
    worldH,
  }
}

function draw() {
  const m = maze.value
  const c = ctx
  if (!m || !c) return
  const { pass, wall } = scale.value
  const p = pitch.value
  const cam = camera()

  c.clearRect(0, 0, view.w, view.h)
  c.save()
  c.translate(-cam.x, -cam.y)

  const x0 = Math.max(0, Math.floor(cam.x / p) - 1)
  const y0 = Math.max(0, Math.floor(cam.y / p) - 1)
  const x1 = Math.min(m.cols - 1, Math.ceil((cam.x + view.w) / p))
  const y1 = Math.min(m.rows - 1, Math.ceil((cam.y + view.h) / p))

  // пройденный путь; тупики — серым, чтобы не возвращаться
  const paint = (afterWalls: boolean) => {
    for (let y = y0; y <= y1; y++) {
      for (let x = x0; x <= x1; x++) {
        const i = y * m.cols + x
        if (!visited[i]) continue
        c.fillStyle = dead[i] ? 'rgba(130, 130, 130, 0.33)' : 'rgba(99, 179, 237, 0.3)'
        const px = wall + x * p
        const py = wall + y * p
        if (!afterWalls) c.fillRect(px, py, pass, pass)
        const sides = openSides(x, y)
        // перемычки рисуем и до, и после стен: clearRect проходов их срезает
        if (sides & 2 && visited[i + 1]) c.fillRect(px + pass, py, wall, pass)
        if (sides & 4 && visited[i + m.cols]) c.fillRect(px, py + pass, pass, wall)
      }
    }
  }
  paint(false)

  c.fillStyle = pattern ?? '#8b5a2b'
  const left = Math.max(0, x0 * p)
  const top = Math.max(0, y0 * p)
  const right = Math.min(cam.worldW, (x1 + 1) * p + wall)
  const bottom = Math.min(cam.worldH, (y1 + 1) * p + wall)
  for (let x = x0; x <= x1 + 1; x++) c.fillRect(x * p, top, wall, bottom - top)
  for (let y = y0; y <= y1 + 1; y++) c.fillRect(left, y * p, right - left, wall)

  for (let y = y0; y <= y1; y++) {
    for (let x = x0; x <= x1; x++) {
      const sides = openSides(x, y)
      const px = wall + x * p
      const py = wall + y * p
      if (sides & 2) c.clearRect(px + pass, py, wall, pass)
      if (sides & 4) c.clearRect(px, py + pass, pass, wall)
    }
  }
  paint(true)

  const gx = wall + m.goal[0] * p
  const gy = wall + m.goal[1] * p
  c.fillStyle = 'rgba(34, 197, 94, 0.85)'
  c.fillRect(gx, gy, pass, pass)

  c.beginPath()
  c.arc(pos.x, pos.y, pass * 0.33, 0, Math.PI * 2)
  c.fillStyle = '#fff'
  c.fill()
  c.lineWidth = 3
  c.strokeStyle = '#2563eb'
  c.stroke()

  c.restore()
}

// --- джойстик ---

function stickVec(e: PointerEvent) {
  const el = stickEl.value
  if (!el) return
  const r = el.getBoundingClientRect()
  stick.vec = {
    x: e.clientX - (r.left + r.width / 2),
    y: e.clientY - (r.top + r.height / 2),
  }
  knob.value = knobOffset(stick, r.width / 2)
}

function stickDown(e: PointerEvent) {
  stick.active = true
  stepTarget = null // палец важнее незаконченного шага по стрелке
  stickVec(e)
  try {
    stickEl.value?.setPointerCapture(e.pointerId)
  } catch {
    /* захват не критичен */
  }
  // отпускание ловим на окне: палец часто уходит за пределы круга, и события
  // самого элемента до нас уже не доходят — точка «залипала» на месте
  window.addEventListener('pointerup', stickUp)
  window.addEventListener('pointercancel', stickUp)
  // ведём и за пределами круга: захват указателя может не сработать
  window.addEventListener('pointermove', stickMove)
}

function stickMove(e: PointerEvent) {
  if (!stick.active) return
  e.preventDefault()
  stickVec(e)
}

function stickUp() {
  stick = emptyStick()
  knob.value = { x: 0, y: 0 }
  dir = { x: 0, y: 0 }
  window.removeEventListener('pointerup', stickUp)
  window.removeEventListener('pointercancel', stickUp)
  window.removeEventListener('pointermove', stickMove)
}

// --- шаг на одну клетку ---

function stepOnce(dx: number, dy: number) {
  const m = maze.value
  if (!m || done.value || stepTarget || stick.active) return
  const cx = cellAt(pos.x)
  const cy = cellAt(pos.y)
  if (!canGo(openSides(cx, cy), dx, dy)) {
    haptic('medium')
    return
  }
  stepTarget = { x: centerOf(cx + dx), y: centerOf(cy + dy) }
}

function onKeyDown(e: KeyboardEvent) {
  const map: Record<string, [number, number]> = {
    ArrowUp: [0, -1], ArrowDown: [0, 1], ArrowLeft: [-1, 0], ArrowRight: [1, 0],
  }
  const d = map[e.key]
  if (!d || !maze.value) return
  e.preventDefault()
  stepOnce(d[0], d[1])
}

// --- кнопки ---

function cyclePref(key: 'scale' | 'speed') {
  const list = key === 'scale' ? SCALES : SPEEDS
  const i = list.findIndex((o) => o.code === prefs.value[key])
  prefs.value[key] = list[(i + 1) % list.length].code
  localStorage.setItem(PREF_KEY, JSON.stringify(prefs.value))
  if (maze.value) {
    const cx = cellAt(pos.x)
    const cy = cellAt(pos.y)
    pos = { x: centerOf(cx), y: centerOf(cy) }
    stepTarget = null
  }
}

function toSizes() {
  cancelAnimationFrame(raf)
  maze.value = null
  sizeCode.value = ''
}

defineExpose({ report: () => {} })
</script>

<template>
  <div ref="box" class="maze">
    <template v-if="!maze">
      <p class="hint">
        Доведите точку до зелёного квадрата. Вести — джойстиком в правом нижнем
        углу, крестик рядом двигает ровно на одну клетку (на компьютере — стрелки).
      </p>
      <button v-for="s in SIZES" :key="s.code" class="size" :disabled="loading" @click="start(s.code)">
        <span class="s-title">{{ s.title }}</span>
        <span class="s-hint">{{ s.hint }}</span>
        <span v-if="bestOf(s.code)" class="s-best">{{ bestOf(s.code) }}</span>
      </button>
      <p v-if="loading" class="hint">Строим лабиринт…</p>
    </template>

    <template v-else>
      <div class="bar">
        <button class="btn" title="Фон лабиринта" @click="emit('settings')">⚙</button>
        <button class="btn" @click="cyclePref('speed')">🏃 {{ speed.title }}</button>
        <button class="btn" @click="cyclePref('scale')">🔍 {{ scale.title }}</button>
        <span class="stat">{{ fmtTime(seconds) }}</span>
      </div>

      <div class="field">
        <canvas ref="canvas"></canvas>

        <!-- крестик: шаг ровно на одну клетку -->
        <div class="dpad">
          <button class="pad up" @click="stepOnce(0, -1)">▲</button>
          <button class="pad left" @click="stepOnce(-1, 0)">◀</button>
          <button class="pad right" @click="stepOnce(1, 0)">▶</button>
          <button class="pad down" @click="stepOnce(0, 1)">▼</button>
        </div>

        <!-- джойстик: ведём точку, пока держим -->
        <div ref="stickEl" class="stick" @pointerdown.prevent="stickDown"
             @pointermove="stickMove" @pointerup="stickUp" @pointercancel="stickUp">
          <span class="knob"
                :style="{ transform: `translate(calc(-50% + ${knob.x * 34}px), calc(-50% + ${knob.y * 34}px))` }"></span>
        </div>

        <div v-if="done" class="overlay">
          <p class="big">Выход найден!</p>
          <p>{{ fmtTime(seconds) }} · прошли {{ explored }} клеток при кратчайших {{ maze.shortest }}</p>
          <div class="row">
            <button class="btn primary" @click="start(sizeCode)">Ещё лабиринт</button>
            <button class="btn" @click="toSizes">Другой размер</button>
          </div>
        </div>
      </div>

      <p class="hint small">
        Пройдено {{ explored }} из {{ maze.cols * maze.rows }} · стены «{{ styleTitle }}» ·
        <button class="link" @click="toSizes">другой размер</button>
      </p>
    </template>
  </div>
</template>

<style scoped>
.maze {
  position: relative;
}

.field {
  position: relative;
}

.hint {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 10px;
}

.hint.small {
  margin: 6px 0 0;
  text-align: center;
}

.size {
  display: block;
  width: 100%;
  text-align: left;
  background: var(--card-color);
  border: none;
  border-radius: 10px;
  color: var(--text-color);
  cursor: pointer;
  padding: 10px 14px;
  margin-bottom: 8px;
}

.s-title {
  display: block;
  font-size: 16px;
}

.s-hint {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
}

.s-best {
  display: block;
  font-size: 12px;
  color: var(--accent-color);
}

.bar {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 8px;
}

.stat {
  margin-left: auto;
  font-size: 15px;
}

canvas {
  display: block;
  border-radius: 10px;
  touch-action: none;
}

/*
  Управление поверх поля: еле заметное, чтобы не закрывать лабиринт. Контур
  виднее заливки — кнопку надо находить, а не разглядывать.
*/
.stick {
  position: absolute;
  right: 14px;
  bottom: 16px;
  width: 116px;
  height: 116px;
  border-radius: 50%;
  border: 2px solid rgba(255, 255, 255, 0.45);
  background: rgba(255, 255, 255, 0.07);
  touch-action: none;
  cursor: grab;
}

.knob {
  position: absolute;
  left: 50%;
  top: 50%;
  width: 46px;
  height: 46px;
  border-radius: 50%;
  border: 2px solid rgba(255, 255, 255, 0.55);
  background: rgba(255, 255, 255, 0.12);
  transform: translate(-50%, -50%);
  transition: transform 0.05s linear;
}

.dpad {
  position: absolute;
  right: 36px;
  bottom: 148px;
  width: 108px;
  height: 108px;
}

.pad {
  position: absolute;
  width: 36px;
  height: 36px;
  border-radius: 8px;
  border: 1px solid rgba(255, 255, 255, 0.4);
  background: rgba(255, 255, 255, 0.06);
  color: rgba(255, 255, 255, 0.65);
  font-size: 12px;
  line-height: 1;
  cursor: pointer;
}

.pad.up {
  left: 36px;
  top: 0;
}

.pad.down {
  left: 36px;
  bottom: 0;
}

.pad.left {
  left: 0;
  top: 36px;
}

.pad.right {
  right: 0;
  top: 36px;
}

.overlay {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.72);
  border-radius: 12px;
  color: #fff;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  text-align: center;
  padding: 12px;
}

.overlay p {
  margin: 0;
}

.big {
  font-size: 18px;
  font-weight: 600;
}

.row {
  display: flex;
  gap: 8px;
}

.btn {
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  cursor: pointer;
  font-size: 13px;
  padding: 7px 10px;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn:disabled {
  opacity: 0.6;
}

.link {
  background: none;
  border: none;
  color: var(--accent-color);
  cursor: pointer;
  font-size: 12px;
  padding: 0;
}
</style>
