<script setup lang="ts">
// «Найди пару»: шестнадцать карт, восемь пар эмодзи.
//
// Рубашка карт — своя картинка из настроек игры (фон всего поля выбирается
// отдельно и рисуется страницей). Никаких ассетов для игры не нужно вовсе.
import { onUnmounted, ref } from 'vue'
import { haptic, hapticNotify } from '../../../shared/telegram'
import { fmtTime } from '../types'
import type { GameResult, GameScore } from '../types'

const props = defineProps<{ cardBg?: string; scores?: GameScore[] }>()
const emit = defineEmits<{ finish: [result: GameResult] }>()

/**
 * Набор символов. Пары берутся из него случайно, поэтому один и тот же расклад
 * почти не повторяется, а на больших сетках хватает непохожих картинок.
 */
const DECK = [
  '🍋', '🍓', '🍇', '🥝', '🌶', '🥑', '🍉', '🫐', '🍒', '🥕', '🌽', '🍄',
  '🐬', '🦊', '🦉', '🐢', '🦋', '🐙', '🦔', '🐝', '🦩', '🐳', '🦕', '🐞',
  '⚽️', '🎸', '🚲', '✈️', '🧭', '🪁', '⛵️', '🚂', '🎲', '🧩', '🎧', '🔭',
  '🌵', '🌻', '🍁', '❄️', '🌈', '🔥', '🌙', '⭐️', '🪐', '⚓️', '💎', '🎈',
]

/** Сетки: не только квадратные — на телефоне вытянутая удобнее. */
const GRIDS = [
  { code: '4x4', cols: 4, rows: 4, title: '4×4', hint: '8 пар' },
  { code: '4x5', cols: 4, rows: 5, title: '4×5', hint: '10 пар' },
  { code: '4x6', cols: 4, rows: 6, title: '4×6', hint: '12 пар' },
  { code: '5x6', cols: 5, rows: 6, title: '5×6', hint: '15 пар' },
  { code: '6x6', cols: 6, rows: 6, title: '6×6', hint: '18 пар' },
]

interface Card {
  id: number
  face: string
  open: boolean
  matched: boolean
}

const grid = ref<typeof GRIDS[number] | null>(null)
const cards = ref<Card[]>([])
const moves = ref(0)
const seconds = ref(0)
const done = ref(false)
let timer: ReturnType<typeof setInterval> | undefined
let locked = false

function bestOf(code: string): string {
  const s = props.scores?.find((x) => x.game === `pairs:${code}`)
  return s?.best ? `рекорд ${fmtTime(s.best)}` : ''
}

function start(g: typeof GRIDS[number]) {
  grid.value = g
  reset()
}

function reset() {
  const g = grid.value
  if (!g) return
  stopTimer()
  const pairs = (g.cols * g.rows) / 2
  const faces = [...DECK].sort(() => Math.random() - 0.5).slice(0, pairs)
  const deck = [...faces, ...faces]
    .map((face, i) => ({ id: i, face, open: false, matched: false }))
    .sort(() => Math.random() - 0.5)
  cards.value = deck
  moves.value = 0
  seconds.value = 0
  done.value = false
  locked = false
}

function startTimer() {
  if (timer) return
  timer = setInterval(() => {
    seconds.value += 1
  }, 1000)
}

function stopTimer() {
  clearInterval(timer)
  timer = undefined
}

function flip(card: Card) {
  if (locked || card.open || card.matched || done.value) return
  startTimer()
  card.open = true
  haptic('light')

  const open = cards.value.filter((c) => c.open && !c.matched)
  if (open.length < 2) return

  moves.value += 1
  if (open[0].face === open[1].face) {
    open.forEach((c) => (c.matched = true))
    hapticNotify('success')
    if (cards.value.every((c) => c.matched)) {
      done.value = true
      stopTimer()
      emit('finish', {
        best: seconds.value,
        detail: { moves: moves.value },
        variant: grid.value?.code,
      })
    }
    return
  }
  // пара не сошлась — даём разглядеть и закрываем
  locked = true
  setTimeout(() => {
    open.forEach((c) => (c.open = false))
    locked = false
  }, 800)
}

onUnmounted(stopTimer)

defineExpose({ report: () => {} })
</script>

<template>
  <div class="pairs">
    <!-- выбор сетки -->
    <template v-if="!grid">
      <p class="hint">
        Открывайте по две карты и запоминайте, что где лежит. Символы каждый
        раз новые, рекорд у каждой сетки свой.
      </p>
      <button v-for="g in GRIDS" :key="g.code" class="grid-pick" @click="start(g)">
        <span class="g-title">{{ g.title }}</span>
        <span class="g-hint">{{ g.hint }}</span>
        <span v-if="bestOf(g.code)" class="g-best">{{ bestOf(g.code) }}</span>
      </button>
    </template>

    <template v-else>
    <div class="bar">
      <span>Ходов: <b>{{ moves }}</b> · {{ fmtTime(seconds) }}</span>
      <button class="btn" @click="reset">Заново</button>
    </div>

    <div class="field">
      <div class="grid" :style="{ gridTemplateColumns: `repeat(${grid.cols}, 1fr)` }">
        <button v-for="c in cards" :key="c.id" class="card"
                :class="{ open: c.open || c.matched, matched: c.matched }"
                :style="!c.open && !c.matched && props.cardBg ? { backgroundImage: `url(${props.cardBg})` } : {}"
                @click="flip(c)">
          <span v-if="c.open || c.matched">{{ c.face }}</span>
          <span v-else-if="!props.cardBg" class="back">🎴</span>
        </button>
      </div>

      <div v-if="done" class="overlay">
        <p class="big">Все пары открыты</p>
        <p>{{ fmtTime(seconds) }} · ходов {{ moves }}</p>
        <div class="row">
          <button class="btn primary" @click="reset">Ещё раз</button>
          <button class="btn" @click="grid = null">Другая сетка</button>
        </div>
      </div>
    </div>

    <p class="hint small">
      Сетка {{ grid.title }} ·
      <button class="link" @click="grid = null">другая сетка</button>
    </p>
    </template>
  </div>
</template>

<style scoped>
.pairs {
  max-width: 420px;
  margin: 0 auto;
}

.bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
  font-size: 15px;
}

.field {
  position: relative;
}

.grid {
  display: grid;
  gap: 6px;
}

.grid-pick {
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

.g-title {
  display: block;
  font-size: 16px;
}

.g-hint {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
}

.g-best {
  display: block;
  font-size: 12px;
  color: var(--accent-color);
}

.row {
  display: flex;
  gap: 8px;
}

.link {
  background: none;
  border: none;
  color: var(--accent-color);
  cursor: pointer;
  font-size: 12px;
  padding: 0;
}

.card {
  aspect-ratio: 1;
  border: none;
  border-radius: 10px;
  background: var(--cell-bg-color);
  background-size: cover;
  background-position: center;
  color: var(--text-color);
  font-size: 30px;
  cursor: pointer;
  transition: transform 0.12s, opacity 0.2s;
}

.card.open {
  background: var(--card-color);
  background-image: none !important;
  transform: scale(1.03);
}

.card.matched {
  opacity: 0.55;
  cursor: default;
}

.back {
  opacity: 0.55;
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
  margin: 0 0 10px;
}

.hint.small {
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
