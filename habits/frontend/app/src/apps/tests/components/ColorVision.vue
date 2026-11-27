<script setup lang="ts">
// Тест на цветовосприятие: таблицы Ишихары, которые приложение рисует само.
//
// Готовых картинок нет намеренно: оригинальные таблицы — чужая
// интеллектуальная собственность, а фиксированный набор заучивается наизусть.
// Свои таблицы каждый раз новые, и, главное, мы знаем про каждый кружок,
// фигура это или фон, — поэтому после ответа таблицу можно перекрасить
// шестью способами и увидеть, что именно вы не различили.
//
// Это скрининг, а не диагноз: так и написано на экране результата.
import { computed, nextTick, onMounted, ref } from 'vue'
import { api } from '../../../shared/api/client'
import { hapticNotify } from '../../../shared/telegram'
import { showToast } from '../../../shared/toast'
import { drawPlate, makePlate, PALETTES, type PaletteMode, type Plate, type PlateKind } from '../color/plates'

const emit = defineEmits<{ close: [] }>()

interface Result {
  id: number
  taken_at: string
  total: number
  correct: number
  protan: number
  deutan: number
  tritan: number
}

/**
 * Состав теста. Первая таблица — контрольная: её видят все, и по ней понятно,
 * что человек разобрался в задании. Дальше красно-зелёные (протан и дейтан —
 * самые частые) и две сине-жёлтые.
 */
const PLAN: PlateKind[] = [
  'demo', 'deutan', 'protan', 'deutan', 'tritan', 'protan', 'deutan', 'tritan', 'deutan',
]

const KIND_TITLE: Record<PlateKind, string> = {
  demo: 'контрольная',
  protan: 'красно-зелёная (протан)',
  deutan: 'красно-зелёная (дейтан)',
  tritan: 'сине-жёлтая (тритан)',
}

type Screen = 'intro' | 'test' | 'done'

const screen = ref<Screen>('intro')
const plates = ref<Plate[]>([])
const answers = ref<string[]>([])
const index = ref(0)
const input = ref('')
const checked = ref(false)
const mode = ref<PaletteMode>('normal')
const history = ref<Result[]>([])
const busy = ref(false)
const canvas = ref<HTMLCanvasElement>()

const plate = computed(() => plates.value[index.value] ?? null)
const isLast = computed(() => index.value >= plates.value.length - 1)
const correct = computed(() => input.value.trim() === plate.value?.answer)

onMounted(loadHistory)

async function loadHistory() {
  try {
    const res = await api.get<{ results: Result[] }>('/tests/color-vision')
    history.value = res.results ?? []
  } catch {
    history.value = []
  }
}

async function start() {
  // размер под экран: на телефоне таблица должна помещаться целиком
  const size = Math.min(340, Math.max(240, window.innerWidth - 60))
  plates.value = PLAN.map((kind) => makePlate(kind, size))
  answers.value = []
  index.value = 0
  input.value = ''
  checked.value = false
  mode.value = 'normal'
  screen.value = 'test'
  await nextTick()
  redraw()
}

function redraw() {
  if (canvas.value && plate.value) drawPlate(canvas.value, plate.value, mode.value)
}

function setMode(next: PaletteMode) {
  mode.value = next
  redraw()
}

function check() {
  if (checked.value) return
  checked.value = true
  answers.value[index.value] = input.value.trim()
  hapticNotify(correct.value ? 'success' : 'warning')
}

function skip() {
  input.value = ''
  check()
}

async function next() {
  if (!checked.value) return
  if (isLast.value) {
    await finish()
    return
  }
  index.value += 1
  input.value = ''
  checked.value = false
  mode.value = 'normal'
  await nextTick()
  redraw()
}

const score = computed(() => {
  let correctCount = 0
  const errors: Record<string, number> = { protan: 0, deutan: 0, tritan: 0, demo: 0 }
  plates.value.forEach((p, i) => {
    if (answers.value[i] === p.answer) correctCount += 1
    else errors[p.kind] += 1
  })
  return { correct: correctCount, errors }
})

/** Вывод по итогам. Формулировки осторожные: это скрининг, а не диагноз. */
const verdict = computed(() => {
  const e = score.value.errors
  const redGreen = e.protan + e.deutan
  // контрольную таблицу видно при любом зрении: не узнали — дело в экране
  const control = e.demo > 0
    ? 'Контрольная таблица не узнана — проверьте яркость экрана, ночной режим и цветовые фильтры, иначе остальное считать нельзя. '
    : ''
  if (control && redGreen + e.tritan === 0) return control.trim()
  if (redGreen === 0 && e.tritan === 0) {
    return 'Все таблицы узнаны — признаков нарушения цветовосприятия тест не нашёл.'
  }
  const parts: string[] = []
  if (redGreen >= 2) {
    parts.push(
      e.protan > e.deutan
        ? 'ошибки в основном на таблицах протан-типа (слабее восприятие красного)'
        : 'ошибки в основном на таблицах дейтан-типа (слабее восприятие зелёного)',
    )
  } else if (redGreen === 1) {
    parts.push('одна ошибка на красно-зелёной таблице — чаще всего это случайность')
  }
  if (e.tritan >= 2) parts.push('есть ошибки на сине-жёлтых таблицах (тритан-тип)')
  else if (e.tritan === 1) parts.push('одна ошибка на сине-жёлтой таблице')
  return control + parts.join('; ') + '.'
})

async function finish() {
  busy.value = true
  try {
    await api.post('/tests/color-vision', {
      total: plates.value.length,
      correct: score.value.correct,
      protan: score.value.errors.protan,
      deutan: score.value.errors.deutan,
      tritan: score.value.errors.tritan,
    })
    await loadHistory()
  } catch {
    showToast('Результат не сохранился — нет связи')
  } finally {
    busy.value = false
    screen.value = 'done'
  }
}

/** Разбор после теста: любую таблицу можно открыть заново и перекрасить. */
async function review(i: number) {
  index.value = i
  input.value = answers.value[i] ?? ''
  checked.value = true
  mode.value = 'normal'
  screen.value = 'test'
  await nextTick()
  redraw()
}

function when(iso: string): string {
  return new Date(iso).toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' })
}
</script>

<template>
  <div class="cv">
    <div class="head">
      <button class="btn" @click="screen === 'test' && plates.length ? (screen = 'done') : emit('close')">
        ← {{ screen === 'test' ? 'К итогам' : 'К тестам' }}
      </button>
      <span class="title">🎨 Цветовосприятие</span>
    </div>

    <!-- вступление -->
    <template v-if="screen === 'intro'">
      <p class="hint">
        Девять таблиц: в кружках спрятано двузначное число. Смотрите на экран
        с обычной яркостью, не в ночном режиме и без цветных фильтров — иначе
        тест ничего не покажет. Отвечайте сразу, не всматриваясь дольше
        нескольких секунд.
      </p>
      <p class="hint">
        После каждого ответа таблицу можно перекрасить шестью способами —
        усилить разницу, подставить коррекцию, посмотреть одну светлоту или
        подсветить ответ. Так видно, что именно вы не различили.
      </p>
      <p class="hint warn">
        Это скрининг, а не диагноз: по экрану цвета не проверяют. При
        сомнениях — к офтальмологу.
      </p>
      <button class="btn primary wide" @click="start">Пройти тест</button>

      <template v-if="history.length">
        <p class="sub">Прошлые прохождения</p>
        <div v-for="h in history" :key="h.id" class="row-line">
          <span>{{ when(h.taken_at) }}</span>
          <span class="dim">
            {{ h.correct }} из {{ h.total }}<template v-if="h.protan + h.deutan">
              · кр-зел {{ h.protan + h.deutan }}</template><template v-if="h.tritan">
              · син-жёлт {{ h.tritan }}</template>
          </span>
        </div>
      </template>
    </template>

    <!-- таблица -->
    <template v-else-if="screen === 'test' && plate">
      <p class="counter">
        Таблица {{ index + 1 }} из {{ plates.length }} · {{ KIND_TITLE[plate.kind] }}
      </p>
      <canvas ref="canvas" class="plate"></canvas>

      <template v-if="!checked">
        <p class="hint">Какое число видите?</p>
        <div class="row">
          <input v-model="input" class="in" inputmode="numeric" maxlength="2"
                 placeholder="например, 74" @keyup.enter="check" />
          <button class="btn primary" :disabled="!input.trim()" @click="check">Ответить</button>
        </div>
        <button class="link" @click="skip">Не вижу числа</button>
      </template>

      <template v-else>
        <p class="verdict" :class="{ ok: correct }">
          <template v-if="correct">Верно: {{ plate.answer }}</template>
          <template v-else>
            Здесь {{ plate.answer }}<template v-if="input.trim()">, вы ответили {{ input }}</template>
          </template>
        </p>
        <p class="hint">Перекрасьте таблицу — так видно, что именно не различилось:</p>
        <div class="palettes">
          <button v-for="p in PALETTES" :key="p.code" class="btn small"
                  :class="{ primary: mode === p.code }" @click="setMode(p.code)">
            {{ p.title }}
          </button>
        </div>
        <p class="hint small">{{ PALETTES.find(p => p.code === mode)?.hint }}</p>
        <button class="btn primary wide" :disabled="busy" @click="next">
          {{ isLast ? 'Итоги' : 'Дальше' }}
        </button>
      </template>
    </template>

    <!-- итоги -->
    <template v-else>
      <p class="score">Узнано таблиц: {{ score.correct }} из {{ plates.length }}</p>
      <p class="hint">{{ verdict }}</p>
      <p class="hint warn">
        Тест на экране зависит от монитора, яркости и ночных фильтров —
        результат стоит воспринимать как повод проверить зрение, а не как
        заключение.
      </p>

      <p class="sub">Разбор</p>
      <div v-for="(p, i) in plates" :key="i" class="row-line clickable" @click="review(i)">
        <span>{{ i + 1 }}. {{ KIND_TITLE[p.kind] }}</span>
        <span :class="answers[i] === p.answer ? 'ok' : 'bad'">
          {{ answers[i] === p.answer ? '✓' : '✗' }} {{ p.answer }}
          <template v-if="answers[i] && answers[i] !== p.answer">(ответ: {{ answers[i] }})</template>
          <template v-else-if="!answers[i]">(не видно)</template>
        </span>
      </div>

      <div class="row">
        <button class="btn primary" @click="start">Пройти заново</button>
        <button class="btn" @click="screen = 'intro'">К началу</button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.cv {
  max-width: 420px;
  margin: 0 auto;
}

.head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
}

.title {
  font-size: 15px;
  color: var(--text-secondary);
}

.hint {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 8px 0;
}

.hint.small {
  font-size: 12px;
  text-align: center;
  min-height: 30px;
}

.hint.warn {
  color: #f59e0b;
}

.counter {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 8px;
  text-align: center;
}

.plate {
  display: block;
  margin: 0 auto 10px;
  border-radius: 50%;
}

.row {
  display: flex;
  gap: 8px;
  margin: 8px 0;
}

.in {
  flex: 1;
  min-width: 0;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 18px;
  padding: 10px 12px;
  text-align: center;
}

.palettes {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  justify-content: center;
}

.verdict {
  text-align: center;
  font-size: 15px;
  margin: 8px 0;
  color: #ef4444;
}

.verdict.ok {
  color: #22c55e;
}

.score {
  font-size: 17px;
  margin: 4px 0 8px;
}

.sub {
  font-size: 14px;
  margin: 14px 0 6px;
}

.row-line {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 13px;
  padding: 7px 0;
  border-top: 1px solid var(--hover-bg-color);
}

.row-line.clickable {
  cursor: pointer;
}

.dim {
  color: var(--text-secondary);
}

.ok {
  color: #22c55e;
}

.bad {
  color: #ef4444;
}

.btn {
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  cursor: pointer;
  font-size: 14px;
  padding: 9px 12px;
}

.btn.small {
  font-size: 12px;
  padding: 6px 9px;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn.wide {
  width: 100%;
  margin-top: 8px;
}

.btn:disabled {
  opacity: 0.6;
}

.link {
  background: none;
  border: none;
  color: var(--accent-color);
  cursor: pointer;
  font-size: 13px;
  padding: 6px 0;
  display: block;
  margin: 0 auto;
}
</style>
