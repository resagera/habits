<script setup lang="ts">
// Кадрирование и поворот картинки прямо в браузере: обработки картинок на
// сервере нет (миниатюры тоже делает страница), а обрезать фон под свой экран
// хочется без сторонних редакторов.
//
// Всё рисуется в одном canvas: сама картинка, затемнение вне рамки, рамка и
// уголки. Так не приходится держать отдельный слой-оверлей и сводить его
// координаты с картинкой.
import { nextTick, onMounted, onUnmounted, ref } from 'vue'

const props = defineProps<{ src: string }>()
const emit = defineEmits<{ close: []; save: [blob: Blob] }>()

const view = ref<HTMLCanvasElement>()
const box = ref<HTMLDivElement>()
const ready = ref(false)
const busy = ref(false)
const lockScreen = ref(false)

/** Картинка, уже повёрнутая на выбранный угол: с ней работают и рамка, и вывод. */
let base: HTMLCanvasElement | null = null
let source: HTMLImageElement | null = null
let angle = 0
let scale = 1 // base px → экранные px
let crop = { x: 0, y: 0, w: 0, h: 0 }

const HANDLE = 26 // радиус захвата уголка в экранных пикселях
const MIN = 40 // минимальная сторона рамки в пикселях картинки

function buildBase() {
  if (!source) return
  const c = document.createElement('canvas')
  const swap = angle === 90 || angle === 270
  c.width = swap ? source.naturalHeight : source.naturalWidth
  c.height = swap ? source.naturalWidth : source.naturalHeight
  const ctx = c.getContext('2d')!
  ctx.translate(c.width / 2, c.height / 2)
  ctx.rotate((angle * Math.PI) / 180)
  ctx.drawImage(source, -source.naturalWidth / 2, -source.naturalHeight / 2)
  base = c
}

function fullCrop() {
  if (!base) return
  crop = { x: 0, y: 0, w: base.width, h: base.height }
  if (lockScreen.value) applyRatio()
}

/** Подогнать рамку под пропорции экрана — самый частый случай для фона. */
function applyRatio() {
  if (!base) return
  const ratio = window.innerWidth / window.innerHeight
  let w = crop.w
  let h = w / ratio
  if (h > base.height) {
    h = base.height
    w = h * ratio
  }
  crop.w = Math.min(w, base.width)
  crop.h = Math.min(h, base.height)
  crop.x = Math.min(crop.x, base.width - crop.w)
  crop.y = Math.min(crop.y, base.height - crop.h)
}

function layout() {
  const canvas = view.value
  const el = box.value
  if (!canvas || !el || !base) return
  const pad = 16
  const maxW = el.clientWidth - pad
  const maxH = el.clientHeight - pad
  scale = Math.min(maxW / base.width, maxH / base.height, 1)
  canvas.width = Math.round(base.width * scale)
  canvas.height = Math.round(base.height * scale)
  redraw()
}

function redraw() {
  const canvas = view.value
  if (!canvas || !base) return
  const ctx = canvas.getContext('2d')!
  ctx.clearRect(0, 0, canvas.width, canvas.height)
  ctx.drawImage(base, 0, 0, canvas.width, canvas.height)

  const x = crop.x * scale
  const y = crop.y * scale
  const w = crop.w * scale
  const h = crop.h * scale

  ctx.fillStyle = 'rgba(0, 0, 0, 0.55)'
  ctx.fillRect(0, 0, canvas.width, y)
  ctx.fillRect(0, y + h, canvas.width, canvas.height - y - h)
  ctx.fillRect(0, y, x, h)
  ctx.fillRect(x + w, y, canvas.width - x - w, h)

  ctx.strokeStyle = '#fff'
  ctx.lineWidth = 2
  ctx.strokeRect(x, y, w, h)
  ctx.fillStyle = '#fff'
  for (const [cx, cy] of [[x, y], [x + w, y], [x, y + h], [x + w, y + h]]) {
    ctx.fillRect(cx - 6, cy - 6, 12, 12)
  }
}

// --- перетаскивание рамки ---

type Drag = { mode: 'move' | 'corner'; corner: number; dx: number; dy: number }
let drag: Drag | null = null

function point(e: PointerEvent) {
  const r = view.value!.getBoundingClientRect()
  return { x: (e.clientX - r.left) / scale, y: (e.clientY - r.top) / scale }
}

function onDown(e: PointerEvent) {
  if (!base) return
  const p = point(e)
  const corners = [
    { x: crop.x, y: crop.y },
    { x: crop.x + crop.w, y: crop.y },
    { x: crop.x, y: crop.y + crop.h },
    { x: crop.x + crop.w, y: crop.y + crop.h },
  ]
  const near = corners.findIndex(
    (c) => Math.hypot(c.x - p.x, c.y - p.y) * scale < HANDLE,
  )
  if (near >= 0) drag = { mode: 'corner', corner: near, dx: 0, dy: 0 }
  else if (p.x >= crop.x && p.x <= crop.x + crop.w && p.y >= crop.y && p.y <= crop.y + crop.h) {
    drag = { mode: 'move', corner: -1, dx: p.x - crop.x, dy: p.y - crop.y }
  } else return
  // захват указателя не критичен: без него палец, ушедший за холст, просто
  // перестанет тянуть рамку
  try {
    view.value?.setPointerCapture(e.pointerId)
  } catch {
    /* ignore */
  }
}

function onMove(e: PointerEvent) {
  if (!drag || !base) return
  const p = point(e)
  if (drag.mode === 'move') {
    crop.x = Math.max(0, Math.min(p.x - drag.dx, base.width - crop.w))
    crop.y = Math.max(0, Math.min(p.y - drag.dy, base.height - crop.h))
  } else {
    // тянем угол: противоположный остаётся на месте
    const right = drag.corner === 1 || drag.corner === 3
    const bottom = drag.corner === 2 || drag.corner === 3
    const fixedX = right ? crop.x : crop.x + crop.w
    const fixedY = bottom ? crop.y : crop.y + crop.h
    let px = Math.max(0, Math.min(p.x, base.width))
    let py = Math.max(0, Math.min(p.y, base.height))
    let w = Math.abs(px - fixedX)
    let h = Math.abs(py - fixedY)
    if (lockScreen.value) {
      const ratio = window.innerWidth / window.innerHeight
      h = w / ratio
      py = bottom ? fixedY + h : fixedY - h
      if (py < 0 || py > base.height) {
        h = bottom ? base.height - fixedY : fixedY
        w = h * ratio
        px = right ? fixedX + w : fixedX - w
      }
    }
    if (w < MIN || h < MIN) return
    crop.x = Math.min(fixedX, px)
    crop.y = Math.min(fixedY, py)
    crop.w = w
    crop.h = h
  }
  redraw()
}

function onUp(e: PointerEvent) {
  if (!drag) return
  drag = null
  try {
    view.value?.releasePointerCapture(e.pointerId)
  } catch {
    /* ignore */
  }
}

// --- кнопки ---

function rotate(delta: number) {
  angle = (angle + delta + 360) % 360
  buildBase()
  fullCrop()
  layout()
}

function toggleLock() {
  lockScreen.value = !lockScreen.value
  if (lockScreen.value) applyRatio()
  redraw()
}

function reset() {
  fullCrop()
  redraw()
}

/**
 * Готовый файл. Больше 3000 px по длинной стороне не отдаём: фон всё равно
 * растягивается под экран, а 5 МБ на загрузку — потолок сервера.
 */
async function save() {
  if (!base) return
  busy.value = true
  try {
    const max = 3000
    const k = Math.min(1, max / Math.max(crop.w, crop.h))
    const out = document.createElement('canvas')
    out.width = Math.max(1, Math.round(crop.w * k))
    out.height = Math.max(1, Math.round(crop.h * k))
    out.getContext('2d')!.drawImage(
      base, crop.x, crop.y, crop.w, crop.h, 0, 0, out.width, out.height,
    )
    const blob = await new Promise<Blob | null>((res) =>
      out.toBlob((b) => res(b), 'image/webp', 0.92),
    )
    const jpeg = blob ?? (await new Promise<Blob | null>((res) =>
      out.toBlob((b) => res(b), 'image/jpeg', 0.92),
    ))
    if (jpeg) emit('save', jpeg)
  } finally {
    busy.value = false
  }
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}

let observer: ResizeObserver | undefined

onMounted(() => {
  const img = new Image()
  img.onload = async () => {
    source = img
    buildBase()
    fullCrop()
    ready.value = true
    // раскладку считаем сразу после отрисовки: requestAnimationFrame в скрытой
    // вкладке не приходит вовсе, и холст остался бы размером 300×150
    await nextTick()
    layout()
  }
  img.src = props.src
  window.addEventListener('resize', layout)
  window.addEventListener('keydown', onKey)
  if (box.value && 'ResizeObserver' in window) {
    observer = new ResizeObserver(() => layout())
    observer.observe(box.value)
  }
})

onUnmounted(() => {
  observer?.disconnect()
  window.removeEventListener('resize', layout)
  window.removeEventListener('keydown', onKey)
})
</script>

<template>
  <div class="crop">
    <div ref="box" class="stage">
      <canvas ref="view" @pointerdown="onDown" @pointermove="onMove"
              @pointerup="onUp" @pointercancel="onUp"></canvas>
      <p v-if="!ready" class="wait">Загрузка…</p>
    </div>

    <div class="tools">
      <button class="btn" title="Повернуть влево" @click="rotate(-90)">↺</button>
      <button class="btn" title="Повернуть вправо" @click="rotate(90)">↻</button>
      <button class="btn" @click="reset">Вся картинка</button>
      <button class="btn" :class="{ primary: lockScreen }" @click="toggleLock">Как экран</button>
    </div>
    <div class="tools">
      <button class="btn" @click="$emit('close')">Отмена</button>
      <button class="btn primary" :disabled="busy || !ready" @click="save">
        Сохранить копию
      </button>
    </div>
  </div>
</template>

<style scoped>
.crop {
  position: fixed;
  inset: 0;
  z-index: 1400;
  background: rgba(0, 0, 0, 0.96);
  display: flex;
  flex-direction: column;
}

.stage {
  flex: 1;
  min-height: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}

canvas {
  touch-action: none; /* иначе телефон уводит жест в прокрутку страницы */
  border-radius: 6px;
}

.wait {
  position: absolute;
  color: rgba(255, 255, 255, 0.7);
}

.tools {
  display: flex;
  gap: 8px;
  padding: 0 12px 8px;
}

.tools:last-child {
  padding-bottom: calc(10px + env(safe-area-inset-bottom));
}

.btn {
  flex: 1;
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  cursor: pointer;
  font-size: 14px;
  padding: 11px 6px;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn:disabled {
  opacity: 0.6;
}
</style>
