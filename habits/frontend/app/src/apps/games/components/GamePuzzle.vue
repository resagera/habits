<script setup lang="ts">
// Пазл из своей картинки. Кусочки меняются местами по двум нажатиям, а не
// перетаскиванием: на телефоне перетаскивание внутри скроллящейся страницы
// работает ненадёжно, а обмен понятен сразу.
//
// Картинка — из набора «Картинка пазла» в настройках игры; если набора нет,
// рисуем свою: пазл должен работать и без единой загруженной картинки.
import { onMounted, ref } from 'vue'
import { haptic, hapticNotify } from '../../../shared/telegram'
import { fmtTime, type GameResult } from '../types'

const props = defineProps<{ pictureBg?: string }>()
const emit = defineEmits<{ finish: [result: GameResult]; settings: [] }>()

/** Сторона рабочего квадрата: по ней режется картинка и из неё же превью. */
const SQUARE = 900

/** Размер сетки — вариант игры: у каждого свой рекорд. */
const SIZES = [3, 4, 5, 6]

const size = ref(0)
const order = ref<number[]>([]) // order[позиция] = номер кусочка
const picked = ref<number | null>(null)
const moves = ref(0)
const seconds = ref(0)
const done = ref(false)
const peek = ref(false)
/**
 * Картинка, приведённая к квадрату: кусочки — квадратные клетки, и
 * прямоугольное фото в них либо сжималось, либо не совпадало с превью.
 * Поэтому один раз вырезаем центральный квадрат и дальше везде берём его —
 * и в кусочках, и в превью, и в подсказке «показать».
 */
const image = ref('')
const ready = ref(false)
let timer: ReturnType<typeof setInterval> | undefined

onMounted(async () => {
  image.value = props.pictureBg ? await squareCrop(props.pictureBg) : drawFallback()
  ready.value = true
})

function squareCrop(url: string): Promise<string> {
  return new Promise((resolve) => {
    const img = new Image()
    img.onload = () => {
      const c = document.createElement('canvas')
      c.width = SQUARE
      c.height = SQUARE
      const g = c.getContext('2d')
      if (!g) {
        resolve(url)
        return
      }
      // вырезаем центральный квадрат: так же, как показывает background-size: cover
      const side = Math.min(img.naturalWidth, img.naturalHeight)
      const sx = (img.naturalWidth - side) / 2
      const sy = (img.naturalHeight - side) / 2
      g.drawImage(img, sx, sy, side, side, 0, 0, SQUARE, SQUARE)
      resolve(c.toDataURL('image/webp', 0.92))
    }
    img.onerror = () => resolve(url)
    img.src = url
  })
}

/**
 * Запасная картинка, если своих нет: цветные полосы и круги на холсте. Так
 * игра работает сразу, а настройки остаются «можно подставить своё».
 */
function drawFallback(): string {
  const c = document.createElement('canvas')
  c.width = SQUARE
  c.height = SQUARE
  const g = c.getContext('2d')
  if (!g) return ''
  const hue = Math.floor(Math.random() * 360)
  const grad = g.createLinearGradient(0, 0, SQUARE, SQUARE)
  grad.addColorStop(0, `hsl(${hue}, 70%, 55%)`)
  grad.addColorStop(1, `hsl(${(hue + 80) % 360}, 70%, 40%)`)
  g.fillStyle = grad
  g.fillRect(0, 0, SQUARE, SQUARE)
  for (let i = 0; i < 18; i++) {
    g.beginPath()
    g.arc(Math.random() * SQUARE, Math.random() * SQUARE, 30 + Math.random() * 110, 0, Math.PI * 2)
    g.fillStyle = `hsla(${(hue + i * 20) % 360}, 80%, ${40 + Math.random() * 40}%, 0.55)`
    g.fill()
  }
  return c.toDataURL('image/webp', 0.9)
}

function start(n: number) {
  size.value = n
  const total = n * n
  let next: number[]
  do {
    next = Array.from({ length: total }, (_, i) => i).sort(() => Math.random() - 0.5)
  } while (next.every((v, i) => v === i)) // перемешали «в собранное» — ещё раз
  order.value = next
  picked.value = null
  moves.value = 0
  seconds.value = 0
  done.value = false
  stopTimer()
}

function startTimer() {
  if (timer || done.value) return
  timer = setInterval(() => {
    seconds.value += 1
  }, 1000)
}

function stopTimer() {
  clearInterval(timer)
  timer = undefined
}

function tap(index: number) {
  if (done.value) return
  startTimer()
  if (picked.value === null) {
    picked.value = index
    haptic('light')
    return
  }
  if (picked.value === index) {
    picked.value = null
    return
  }
  const next = [...order.value]
  ;[next[picked.value], next[index]] = [next[index], next[picked.value]]
  order.value = next
  picked.value = null
  moves.value += 1
  haptic('light')
  if (next.every((v, i) => v === i)) {
    done.value = true
    stopTimer()
    hapticNotify('success')
    emit('finish', {
      best: seconds.value,
      detail: { moves: moves.value },
      variant: String(size.value),
    })
  }
}

/** Кусочек картинки: сетка n×n — это background-size n*100%. */
function pieceStyle(piece: number) {
  const n = size.value
  const r = Math.floor(piece / n)
  const c = piece % n
  return {
    backgroundImage: `url(${image.value})`,
    backgroundSize: `${n * 100}% ${n * 100}%`,
    backgroundPosition: `${(c / (n - 1)) * 100}% ${(r / (n - 1)) * 100}%`,
  }
}

defineExpose({ report: () => {} })
</script>

<template>
  <div class="puzzle">
    <template v-if="!size">
      <p class="hint">
        Соберите картинку: нажмите на два кусочка — они поменяются местами.
        Картинку можно подставить свою — «⚙» вверху.
      </p>
      <div class="sizes">
        <button v-for="n in SIZES" :key="n" class="btn size" :disabled="!ready" @click="start(n)">
          {{ n }}×{{ n }}
        </button>
      </div>
      <img v-if="image" class="preview" :src="image" alt="" />
      <p v-else class="hint">Готовим картинку…</p>
    </template>

    <template v-else>
      <div class="bar">
        <button class="btn" title="Картинка пазла" @click="emit('settings')">⚙</button>
        <span>Ходов: <b>{{ moves }}</b> · {{ fmtTime(seconds) }}</span>
        <button class="btn" @pointerdown="peek = true" @pointerup="peek = false"
                @pointerleave="peek = false">👁 Показать</button>
      </div>

      <div class="field">
        <div v-if="peek" class="peek" :style="{ backgroundImage: `url(${image})` }"></div>
        <div v-else class="grid" :style="{ gridTemplateColumns: `repeat(${size}, 1fr)` }">
          <button v-for="(piece, i) in order" :key="i" class="piece"
                  :class="{ on: picked === i, home: piece === i }"
                  :style="pieceStyle(piece)" @click="tap(i)"></button>
        </div>

        <div v-if="done" class="overlay">
          <p class="big">Картинка собрана!</p>
          <p>{{ fmtTime(seconds) }} · ходов {{ moves }}</p>
          <div class="row">
            <button class="btn primary" @click="start(size)">Ещё раз</button>
            <button class="btn" @click="size = 0">Другой размер</button>
          </div>
        </div>
      </div>

      <p class="hint small">
        Кусочки на своих местах подсвечены ·
        <button class="link" @click="size = 0">другой размер</button>
      </p>
    </template>
  </div>
</template>

<style scoped>
.puzzle {
  max-width: 420px;
  margin: 0 auto;
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

.sizes {
  display: flex;
  gap: 8px;
  margin-bottom: 10px;
}

.btn.size {
  flex: 1;
  font-size: 15px;
  padding: 12px 0;
}

/* превью — тот же квадрат, что и поле: иначе кажется, что картинки разные */
.preview {
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  border-radius: 10px;
  display: block;
}

.bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 8px;
  font-size: 14px;
}

.field {
  position: relative;
}

.grid {
  display: grid;
  gap: 2px;
}

.piece {
  aspect-ratio: 1;
  border: none;
  border-radius: 4px;
  background-color: var(--cell-bg-color);
  background-repeat: no-repeat;
  cursor: pointer;
  outline: 2px solid transparent;
}

/* кусочек на своём месте — тонкая зелёная рамка: видно, что уже сошлось */
.piece.home {
  outline-color: rgba(34, 197, 94, 0.75);
}

.piece.on {
  outline-color: var(--accent-color);
  outline-width: 3px;
}

.peek {
  width: 100%;
  aspect-ratio: 1;
  background-size: cover;
  background-position: center;
  border-radius: 8px;
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

.link {
  background: none;
  border: none;
  color: var(--accent-color);
  cursor: pointer;
  font-size: 12px;
  padding: 0;
}
</style>
