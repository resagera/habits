<script setup lang="ts">
// Реакция в двух режимах.
//
// «Классика» — пять попыток нажать, как только поле станет зелёным; рекорд в
// миллисекундах, меньше — лучше. «Мишени» — квадрат появляется в случайном
// месте и держится всё меньше; рекорд в попаданиях, больше — лучше. Поэтому
// у режимов разные коды рекорда: одной колонкой «лучше» их не описать.
import { computed, onUnmounted, ref } from 'vue'
import { haptic, hapticNotify } from '../../../shared/telegram'
import type { GameResult, GameScore } from '../types'

const props = defineProps<{ scores?: GameScore[] }>()
const emit = defineEmits<{ finish: [result: GameResult] }>()

const TRIES = 5
/** Сколько держится мишень: первая долго, дальше всё короче. */
const START_MS = 1500
const MIN_MS = 380
const FADE = 0.93
/** Промахи мимо мишени: три — и партия закончена. */
const LIVES = 3

type Mode = 'classic' | 'targets'
type Phase = 'idle' | 'wait' | 'go' | 'early' | 'done'

const mode = ref<Mode | null>(null)

// --- классика ---
const phase = ref<Phase>('idle')
const results = ref<number[]>([])
const lastMs = ref(0)
let greenAt = 0
let timer: ReturnType<typeof setTimeout> | undefined

// --- мишени ---
const hits = ref(0)
const lives = ref(LIVES)
const target = ref<{ x: number; y: number; size: number } | null>(null)
const over = ref(false)
const window_ = ref(START_MS)
let missTimer: ReturnType<typeof setTimeout> | undefined

const best = computed(() => (results.value.length ? Math.min(...results.value) : 0))
const avg = computed(() =>
  results.value.length
    ? Math.round(results.value.reduce((a, b) => a + b, 0) / results.value.length)
    : 0,
)

function bestClassic(): string {
  const s = props.scores?.find((x) => x.game === 'reaction')
  return s?.best ? `рекорд ${s.best} мс` : ''
}

function bestTargets(): string {
  const s = props.scores?.find((x) => x.game === 'targets')
  return s?.best ? `рекорд ${s.best} попаданий` : ''
}

// --- классика ---

function arm() {
  phase.value = 'wait'
  clearTimeout(timer)
  // ожидание случайное: при ровном ритме его начинаешь угадывать
  timer = setTimeout(() => {
    greenAt = performance.now()
    phase.value = 'go'
    haptic('medium')
  }, 1500 + Math.random() * 3500)
}

function tap() {
  switch (phase.value) {
    case 'idle':
    case 'early':
      arm()
      break
    case 'wait':
      clearTimeout(timer)
      phase.value = 'early'
      hapticNotify('warning')
      break
    case 'go': {
      const ms = Math.round(performance.now() - greenAt)
      lastMs.value = ms
      results.value = [...results.value, ms]
      haptic('light')
      if (results.value.length >= TRIES) {
        phase.value = 'done'
        hapticNotify('success')
        emit('finish', { best: best.value, detail: { avg: avg.value, tries: TRIES } })
      } else {
        arm()
      }
      break
    }
    case 'done':
      results.value = []
      lastMs.value = 0
      arm()
      break
  }
}

// --- мишени ---

const field = ref<HTMLDivElement>()

function startTargets() {
  hits.value = 0
  lives.value = LIVES
  window_.value = START_MS
  over.value = false
  spawn()
}

function spawn() {
  const el = field.value
  const w = el?.clientWidth ?? 300
  const h = el?.clientHeight ?? 300
  const size = Math.max(42, Math.min(84, Math.round(Math.min(w, h) / 5)))
  target.value = {
    x: Math.random() * (w - size),
    y: Math.random() * (h - size),
    size,
  }
  clearTimeout(missTimer)
  missTimer = setTimeout(missed, window_.value)
}

/** Не успели — партия закончена: в этом вся соль режима. */
function missed() {
  target.value = null
  over.value = true
  hapticNotify('error')
  emit('finish', {
    game: 'targets',
    best: hits.value,
    detail: { window: Math.round(window_.value) },
  })
}

function hitTarget() {
  if (over.value) return
  hits.value += 1
  haptic('light')
  window_.value = Math.max(MIN_MS, window_.value * FADE)
  spawn()
}

/** Мимо: три промаха по фону тоже заканчивают партию. */
function missClick() {
  if (over.value || !target.value) return
  lives.value -= 1
  hapticNotify('warning')
  if (lives.value <= 0) {
    clearTimeout(missTimer)
    missed()
  }
}

onUnmounted(() => {
  clearTimeout(timer)
  clearTimeout(missTimer)
})

defineExpose({ report: () => {} })

const label = computed(() => {
  switch (phase.value) {
    case 'idle':
      return 'Нажмите, чтобы начать'
    case 'wait':
      return 'Ждите зелёного…'
    case 'go':
      return 'ЖМИ!'
    case 'early':
      return 'Рано! Нажмите, чтобы повторить'
    default:
      return 'Готово'
  }
})
</script>

<template>
  <div class="reaction">
    <!-- выбор режима -->
    <template v-if="!mode">
      <button class="mode" @click="mode = 'classic'">
        <span class="m-title">Классика</span>
        <span class="m-hint">Пять попыток: нажать, как только поле станет зелёным</span>
        <span v-if="bestClassic()" class="m-best">{{ bestClassic() }}</span>
      </button>
      <button class="mode" @click="mode = 'targets'; startTargets()">
        <span class="m-title">Мишени</span>
        <span class="m-hint">
          Квадрат появляется в разных местах и держится всё меньше. Промахи мимо
          стоят жизни, не успели — конец.
        </span>
        <span v-if="bestTargets()" class="m-best">{{ bestTargets() }}</span>
      </button>
    </template>

    <!-- классика -->
    <template v-else-if="mode === 'classic'">
      <div class="pad" :class="phase" @pointerdown.prevent="tap">
        <p class="label">{{ label }}</p>
        <p v-if="lastMs && phase !== 'go'" class="ms">{{ lastMs }} мс</p>
        <p v-if="phase === 'done'" class="ms">лучшая {{ best }} мс · средняя {{ avg }} мс</p>
      </div>
      <p class="hint">
        Попытка {{ Math.min(results.length + 1, TRIES) }} из {{ TRIES }}.
        Рекордом считается лучшая попытка серии ·
        <button class="link" @click="mode = null">другой режим</button>
      </p>
      <div v-if="results.length" class="tries">
        <span v-for="(r, i) in results" :key="i" class="try" :class="{ best: r === best }">{{ r }}</span>
      </div>
    </template>

    <!-- мишени -->
    <template v-else>
      <div class="bar">
        <span>Попаданий: <b>{{ hits }}</b></span>
        <span class="lives">{{ '❤'.repeat(Math.max(lives, 0)) }}</span>
        <span class="win">{{ Math.round(window_) }} мс</span>
      </div>
      <div ref="field" class="field" @pointerdown.prevent="missClick">
        <button v-if="target" class="target"
                :style="{ left: `${target.x}px`, top: `${target.y}px`, width: `${target.size}px`, height: `${target.size}px` }"
                @pointerdown.stop.prevent="hitTarget"></button>

        <div v-if="over" class="overlay">
          <p class="big">Мимо!</p>
          <p>Попаданий: {{ hits }}</p>
          <div class="row">
            <button class="btn primary" @click="startTargets">Ещё раз</button>
            <button class="btn" @click="mode = null">Другой режим</button>
          </div>
        </div>
      </div>
      <p class="hint">Жмите по квадрату, мимо — минус жизнь.</p>
    </template>
  </div>
</template>

<style scoped>
.reaction {
  max-width: 420px;
  margin: 0 auto;
}

.mode {
  display: block;
  width: 100%;
  text-align: left;
  background: var(--card-color);
  border: none;
  border-radius: 10px;
  color: var(--text-color);
  cursor: pointer;
  padding: 12px 14px;
  margin-bottom: 10px;
}

.m-title {
  display: block;
  font-size: 16px;
}

.m-hint {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
}

.m-best {
  display: block;
  font-size: 12px;
  color: var(--accent-color);
}

.pad {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  height: 260px;
  border-radius: 14px;
  cursor: pointer;
  user-select: none;
  color: #fff;
  background: #475569;
  transition: background 0.08s;
}

.pad.wait {
  background: #b91c1c;
}

.pad.go {
  background: #16a34a;
}

.pad.early {
  background: #a16207;
}

.pad.done {
  background: #1d4ed8;
}

.label {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
}

.ms {
  margin: 0;
  font-size: 15px;
  opacity: 0.9;
}

.bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 15px;
  margin-bottom: 8px;
}

.lives {
  letter-spacing: 2px;
}

.win {
  font-size: 13px;
  color: var(--text-secondary);
}

/* поле мишеней: своя подложка, иначе на тёмном фоне не видно границ игры */
.field {
  position: relative;
  height: 60vh;
  max-height: 460px;
  border-radius: 14px;
  background: rgba(0, 0, 0, 0.25);
  overflow: hidden;
  touch-action: manipulation;
}

.target {
  position: absolute;
  border: none;
  border-radius: 10px;
  background: #22c55e;
  box-shadow: 0 0 0 3px rgba(255, 255, 255, 0.35);
  cursor: pointer;
  animation: pop 0.12s ease-out;
}

@keyframes pop {
  from {
    transform: scale(0.6);
  }
}

.overlay {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.72);
  color: #fff;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  text-align: center;
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

.hint {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 10px 0 6px;
  text-align: center;
}

.tries {
  display: flex;
  justify-content: center;
  gap: 6px;
  flex-wrap: wrap;
}

.try {
  background: var(--cell-bg-color);
  border-radius: 8px;
  font-size: 13px;
  padding: 4px 8px;
}

.try.best {
  background: var(--accent-color);
  color: #fff;
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

.link {
  background: none;
  border: none;
  color: var(--accent-color);
  cursor: pointer;
  font-size: 12px;
  padding: 0;
}
</style>
