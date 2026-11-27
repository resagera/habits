<script setup lang="ts">
// Пятнашки. Картинка, выбранная для игры, лежит под всем полем (её рисует
// страница), а костяшки — полупрозрачные белые плашки с рамкой: так они
// читаются на любом фото, не закрывая его.
//
// Перемешиваем не случайной перестановкой, а сотней случайных ходов от
// собранного состояния: так позиция гарантированно решаема.
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { haptic, hapticNotify } from '../../../shared/telegram'
import { fmtTime } from '../types'
import type { GameResult } from '../types'

defineProps<{ cardBg?: string }>() // общий интерфейс игр; этой игре рубашка не нужна
const emit = defineEmits<{ finish: [result: GameResult] }>()

const SIZE = 4
const COUNT = SIZE * SIZE

/** tiles[i] — что лежит в клетке i: 1..15, 0 — пустая. */
const tiles = ref<number[]>([])
const moves = ref(0)
const seconds = ref(0)
const done = ref(false)
let timer: ReturnType<typeof setInterval> | undefined

const solved = computed(() => tiles.value.every((v, i) => (i === COUNT - 1 ? v === 0 : v === i + 1)))

function neighbors(i: number): number[] {
  const r = Math.floor(i / SIZE)
  const c = i % SIZE
  const out: number[] = []
  if (r > 0) out.push(i - SIZE)
  if (r < SIZE - 1) out.push(i + SIZE)
  if (c > 0) out.push(i - 1)
  if (c < SIZE - 1) out.push(i + 1)
  return out
}

function shuffle() {
  const next = Array.from({ length: COUNT }, (_, i) => (i + 1) % COUNT)
  let hole = COUNT - 1
  let prev = -1
  for (let k = 0; k < 200; k++) {
    const options = neighbors(hole).filter((i) => i !== prev)
    const pick = options[Math.floor(Math.random() * options.length)]
    next[hole] = next[pick]
    next[pick] = 0
    prev = hole
    hole = pick
  }
  tiles.value = next
}

function reset() {
  stopTimer()
  shuffle()
  if (solved.value) shuffle() // бывает, хотя и редко
  moves.value = 0
  seconds.value = 0
  done.value = false
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

function tap(i: number) {
  if (done.value) return
  const hole = tiles.value.indexOf(0)
  if (!neighbors(i).includes(hole)) return
  const next = [...tiles.value]
  next[hole] = next[i]
  next[i] = 0
  tiles.value = next
  moves.value += 1
  haptic('light')
  startTimer()
  if (solved.value) {
    done.value = true
    stopTimer()
    hapticNotify('success')
    emit('finish', { best: seconds.value, detail: { moves: moves.value } })
  }
}

onMounted(reset)
onUnmounted(stopTimer)

defineExpose({ report: () => {} })
</script>

<template>
  <div class="g15">
    <div class="bar">
      <span>Ходов: <b>{{ moves }}</b> · {{ fmtTime(seconds) }}</span>
      <button class="btn" @click="reset">Заново</button>
    </div>

    <div class="field">
      <div class="grid">
        <button v-for="(v, i) in tiles" :key="i" class="tile" :class="{ hole: !v }" @click="tap(i)">
          <span v-if="v">{{ v }}</span>
        </button>
      </div>

      <div v-if="done" class="overlay">
        <p class="big">Собрано!</p>
        <p>{{ fmtTime(seconds) }} · ходов {{ moves }}</p>
        <button class="btn primary" @click="reset">Ещё раз</button>
      </div>
    </div>

    <p class="hint">Двигайте костяшки в пустую клетку: 1, 2, 3… и так до 15.</p>
  </div>
</template>

<style scoped>
.g15 {
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
  grid-template-columns: repeat(4, 1fr);
  gap: 4px;
}

/* белая плашка с рамкой: читается и на тёмной теме, и поверх любого фото */
.tile {
  position: relative;
  aspect-ratio: 1;
  border: 1px solid rgba(255, 255, 255, 0.55);
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.3);
  color: var(--text-color);
  font-size: 20px;
  font-weight: 600;
  cursor: pointer;
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.45);
}

.tile.hole {
  background: transparent;
  border-color: transparent;
  cursor: default;
  text-shadow: none;
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
