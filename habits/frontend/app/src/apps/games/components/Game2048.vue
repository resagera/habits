<script setup lang="ts">
// 2048: плитки едут в одну сторону, одинаковые складываются.
//
// Незаконченная партия живёт в localStorage этого устройства: мини-приложение
// закрывают на полуслове постоянно, а гонять поле на сервер после каждого
// хода — лишний трафик ради того, что нужно только здесь.
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { haptic, hapticNotify } from '../../../shared/telegram'
import { onSwipe, type SwipeDir } from '../swipe'
import type { GameResult } from '../types'

defineProps<{ cardBg?: string }>() // общий интерфейс игр; этой игре рубашка не нужна
const emit = defineEmits<{ finish: [result: GameResult] }>()

const SIZE = 4
const KEY = 'game_2048_state'

const board = ref<number[]>(Array(SIZE * SIZE).fill(0))
const score = ref(0)
const over = ref(false)
const won = ref(false)
const field = ref<HTMLDivElement>()

const maxTile = computed(() => Math.max(...board.value))

function spawn() {
  const free: number[] = []
  board.value.forEach((v, i) => {
    if (!v) free.push(i)
  })
  if (!free.length) return
  board.value[free[Math.floor(Math.random() * free.length)]] = Math.random() < 0.9 ? 2 : 4
}

function reset() {
  board.value = Array(SIZE * SIZE).fill(0)
  score.value = 0
  over.value = false
  won.value = false
  spawn()
  spawn()
  save()
}

/** Сдвиг одной линии к началу со склейкой. Возвращает новую линию и очки. */
function slide(line: number[]): { line: number[]; gained: number } {
  const nums = line.filter((n) => n)
  const out: number[] = []
  let gained = 0
  for (let i = 0; i < nums.length; i++) {
    if (nums[i] === nums[i + 1]) {
      out.push(nums[i] * 2)
      gained += nums[i] * 2
      i++ // склеенная плитка в этом ходу больше не участвует
    } else {
      out.push(nums[i])
    }
  }
  while (out.length < SIZE) out.push(0)
  return { line: out, gained }
}

/** Индексы одной линии в порядке движения: ход — это всегда «к началу линии». */
function lineIndexes(dir: SwipeDir, n: number): number[] {
  const idx: number[] = []
  for (let i = 0; i < SIZE; i++) {
    if (dir === 'left') idx.push(n * SIZE + i)
    else if (dir === 'right') idx.push(n * SIZE + (SIZE - 1 - i))
    else if (dir === 'up') idx.push(i * SIZE + n)
    else idx.push((SIZE - 1 - i) * SIZE + n)
  }
  return idx
}

function move(dir: SwipeDir) {
  if (over.value) return
  const next = [...board.value]
  let gained = 0
  let moved = false
  for (let n = 0; n < SIZE; n++) {
    const idx = lineIndexes(dir, n)
    const line = idx.map((i) => next[i])
    const res = slide(line)
    gained += res.gained
    res.line.forEach((v, i) => {
      if (next[idx[i]] !== v) moved = true
      next[idx[i]] = v
    })
  }
  if (!moved) return

  board.value = next
  score.value += gained
  if (gained) haptic('light')
  spawn()
  if (!won.value && board.value.includes(2048)) {
    won.value = true
    hapticNotify('success')
  }
  if (!canMove()) {
    over.value = true
    hapticNotify('warning')
    emit('finish', { best: score.value, detail: { tile: maxTile.value } })
  }
  save()
}

function canMove(): boolean {
  if (board.value.includes(0)) return true
  for (let r = 0; r < SIZE; r++) {
    for (let c = 0; c < SIZE; c++) {
      const v = board.value[r * SIZE + c]
      if (c + 1 < SIZE && board.value[r * SIZE + c + 1] === v) return true
      if (r + 1 < SIZE && board.value[(r + 1) * SIZE + c] === v) return true
    }
  }
  return false
}

function save() {
  try {
    localStorage.setItem(KEY, JSON.stringify({ board: board.value, score: score.value, over: over.value }))
  } catch {
    /* приватный режим — партия просто не переживёт закрытие */
  }
}

function restore(): boolean {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return false
    const s = JSON.parse(raw)
    if (!Array.isArray(s.board) || s.board.length !== SIZE * SIZE) return false
    board.value = s.board
    score.value = Number(s.score) || 0
    over.value = !!s.over
    won.value = board.value.includes(2048)
    return true
  } catch {
    return false
  }
}

/** Незаконченную партию тоже засчитываем: иначе хороший счёт пропадёт при выходе. */
function report() {
  if (score.value > 0 && !over.value) {
    emit('finish', { best: score.value, detail: { tile: maxTile.value } })
  }
}

defineExpose({ report })

function onKey(e: KeyboardEvent) {
  const map: Record<string, SwipeDir> = {
    ArrowUp: 'up', ArrowDown: 'down', ArrowLeft: 'left', ArrowRight: 'right',
  }
  const dir = map[e.key]
  if (!dir) return
  e.preventDefault()
  move(dir)
}

let stopSwipe: (() => void) | undefined

onMounted(() => {
  if (!restore()) reset()
  window.addEventListener('keydown', onKey)
  if (field.value) stopSwipe = onSwipe(field.value, move)
})

onUnmounted(() => {
  window.removeEventListener('keydown', onKey)
  stopSwipe?.()
})

/** Цвета плиток — классические: они одинаково читаются и на тёмной теме, и на картинке. */
const COLORS: Record<number, string> = {
  2: '#eee4da', 4: '#ede0c8', 8: '#f2b179', 16: '#f59563', 32: '#f67c5f',
  64: '#f65e3b', 128: '#edcf72', 256: '#edcc61', 512: '#edc850',
  1024: '#edc53f', 2048: '#edc22e',
}

function tileStyle(v: number) {
  if (!v) return {}
  return {
    background: COLORS[v] ?? '#3c3a32',
    color: v <= 4 ? '#776e65' : '#fff',
    fontSize: v >= 1024 ? '20px' : v >= 128 ? '24px' : '28px',
  }
}
</script>

<template>
  <div class="g2048">
    <div class="bar">
      <span class="score">Очки: <b>{{ score }}</b></span>
      <button class="btn" @click="reset">Заново</button>
    </div>

    <div ref="field" class="field">
      <div class="grid">
        <div v-for="(v, i) in board" :key="i" class="cell" :style="tileStyle(v)">
          <span v-if="v">{{ v }}</span>
        </div>
      </div>

      <div v-if="over" class="overlay">
        <p class="big">Ходов больше нет</p>
        <p>Очки: {{ score }} · лучшая плитка {{ maxTile }}</p>
        <button class="btn primary" @click="reset">Ещё раз</button>
      </div>
    </div>

    <p v-if="won && !over" class="hint">🎉 Плитка 2048 собрана — можно играть дальше.</p>
    <p class="hint">Свайпы или стрелки на клавиатуре.</p>
  </div>
</template>

<style scoped>
.g2048 {
  max-width: 420px;
  margin: 0 auto;
}

.bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}

.score {
  font-size: 15px;
}

.field {
  position: relative;
}

.grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 8px;
  border-radius: 10px;
}

.cell {
  aspect-ratio: 1;
  border-radius: 8px;
  background: var(--cell-bg-color);
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  user-select: none;
  transition: background 0.1s;
}

.overlay {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.7);
  border-radius: 12px;
  color: #fff;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  text-align: center;
}

.overlay p {
  margin: 0;
}

.big {
  font-size: 18px;
  font-weight: 600;
}

.hint {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 8px 0 0;
  text-align: center;
}

.btn {
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  cursor: pointer;
  font-size: 14px;
  padding: 8px 12px;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}
</style>
