<script setup lang="ts">
// Тесты — карточки вопросов с вариантами ответов и личным прогрессом.
// Три экрана в одном компоненте: список колод → прогон → итоги с разбором.
//
// Правильный ответ приходит с сервера ТОЛЬКО в ответе на answer: заранее его
// нет ни в одном запросе, иначе учебный режим обесценивается парой кликов в
// девтулзах. Порядок вопросов тоже серверный — перезагрузка не тасует колоду.
import { computed, onUnmounted, ref } from 'vue'
import { confirmAction } from '../../shared/telegram'
import { showToast } from '../../shared/toast'
import {
  fetchDecks, fetchGroups, fetchReview, fetchSession, finishSession,
  resetDeck, sendAnswer, startSession,
} from './api'
import {
  questionImageUrl,
  type ReviewItem, type TestDeck, type TestGroup, type TestQuestion,
  type TestScope, type TestSession,
} from './types'

const decks = ref<TestDeck[]>([])
const active = ref<Record<string, TestSession>>({})
const groups = ref<TestGroup[]>([])
const loading = ref(true)
const busy = ref(false)

// текущий прогон
const session = ref<TestSession | null>(null)
const question = ref<TestQuestion | null>(null)
const position = ref(0)
const chosen = ref<number | null>(null)
const correctIdx = ref<number | null>(null)
const review = ref<ReviewItem[] | null>(null)
const groupsOpen = ref(false)

const deck = computed(() => decks.value.find(d => d.id === session.value?.deck_id) ?? decks.value[0])
const answered = computed(() => correctIdx.value !== null)
const isExam = computed(() => session.value?.mode === 'exam')

// таймер экзамена
const now = ref(Date.now())
const ticker = window.setInterval(() => {
  now.value = Date.now()
  if (session.value?.expires_at && !session.value.finished_at) {
    if (new Date(session.value.expires_at).getTime() <= now.value) void timeUp()
  }
}, 1000)
onUnmounted(() => clearInterval(ticker))

const timeLeft = computed(() => {
  if (!session.value?.expires_at || session.value.finished_at) return ''
  const ms = new Date(session.value.expires_at).getTime() - now.value
  if (ms <= 0) return '0:00'
  const s = Math.floor(ms / 1000)
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
})

async function load() {
  loading.value = true
  try {
    const res = await fetchDecks()
    decks.value = res.decks
    active.value = res.active ?? {}
  } catch {
    showToast('Не удалось загрузить тесты')
  } finally {
    loading.value = false
  }
}
void load()

async function openGroups(deckId: number) {
  groupsOpen.value = !groupsOpen.value
  if (groupsOpen.value && groups.value.length === 0) {
    try {
      groups.value = (await fetchGroups(deckId)).groups
    } catch {
      showToast('Не удалось загрузить темы')
    }
  }
}

async function begin(deckId: number, mode: 'study' | 'exam', scope: TestScope, groupId?: number) {
  if (busy.value) return
  busy.value = true
  try {
    const { session: s } = await startSession({
      deck_id: deckId, mode, scope, group_id: groupId,
    })
    await openSession(s.id)
  } catch (e: unknown) {
    const msg = (e as { code?: string })?.code === 'no_questions'
      ? 'В этом наборе вопросов не осталось — начните колоду сначала 🎉'
      : 'Не удалось начать прогон'
    showToast(msg)
  } finally {
    busy.value = false
  }
}

async function openSession(id: number) {
  const res = await fetchSession(id)
  session.value = res.session
  question.value = res.question ?? null
  position.value = res.position ?? 0
  chosen.value = null
  correctIdx.value = null
  review.value = null
  if (res.session.finished_at) await showResults()
}

/** Ответ засчитывает сервер: он же решает, верно ли, и двигает прогресс. */
async function answer(idx: number) {
  if (!session.value || !question.value || answered.value || busy.value) return
  busy.value = true
  chosen.value = idx
  try {
    const res = await sendAnswer(session.value.id, question.value.id, idx)
    session.value = res.session
    correctIdx.value = res.correct_idx
    // в экзамене верный вариант не показываем — только в конце, в разборе
    if (isExam.value) {
      if (res.session.finished_at) await showResults()
      else nextFrom(res.next, res.position)
    }
  } catch {
    chosen.value = null
    showToast('Не удалось отправить ответ')
  } finally {
    busy.value = false
  }
}

/** Следующий вопрос в учебном режиме — по кнопке, чтобы успеть увидеть ответ. */
async function next() {
  if (!session.value) return
  if (session.value.finished_at) {
    await showResults()
    return
  }
  const res = await fetchSession(session.value.id)
  session.value = res.session
  nextFrom(res.question, res.position)
  if (res.session.finished_at) await showResults()
}

function nextFrom(q?: TestQuestion, pos?: number) {
  question.value = q ?? null
  position.value = pos ?? position.value + 1
  chosen.value = null
  correctIdx.value = null
}

async function timeUp() {
  if (!session.value) return
  const { session: s } = await finishSession(session.value.id)
  session.value = s
  showToast('Время вышло ⏰')
  await showResults()
}

async function stop() {
  if (!session.value) return
  const { session: s } = await finishSession(session.value.id)
  session.value = s
  await showResults()
}

async function showResults() {
  if (!session.value) return
  try {
    review.value = (await fetchReview(session.value.id)).items
  } catch {
    review.value = []
  }
  question.value = null
  await load()
}

function exit() {
  session.value = null
  question.value = null
  review.value = null
  void load()
}

async function doReset(d: TestDeck, hard: boolean) {
  const what = hard ? 'Стереть весь прогресс и статистику' : 'Начать колоду сначала'
  if (!(await confirmAction(`${what} «${d.title}»?`))) return
  await resetDeck(d.id, hard)
  showToast(hard ? 'Прогресс стёрт' : 'Начинаем сначала 🔄')
  await load()
}

function pct(d: TestDeck | TestGroup): number {
  return d.total ? Math.round((d.passed / d.total) * 100) : 0
}

const wrongItems = computed(() => (review.value ?? []).filter(i => i.is_correct === false))
</script>

<template>
  <div class="tests">
    <p v-if="loading" class="hint">Загрузка…</p>

    <!-- ЭКРАН 1: колоды -->
    <template v-else-if="!session">
      <p v-if="decks.length === 0" class="hint">
        Колод пока нет. Их заливает администратор через импорт.
      </p>
      <div v-for="d in decks" :key="d.id" class="card">
        <div class="deck-head">
          <h3>{{ d.title }}</h3>
          <span class="chip">{{ d.passed }} / {{ d.total }}</span>
        </div>
        <p v-if="d.description" class="hint">{{ d.description }}</p>

        <div class="bar"><div class="bar-fill" :style="{ width: pct(d) + '%' }" /></div>
        <p class="stat">
          Пройдено {{ pct(d) }}%<span v-if="d.wrong"> · с ошибками: {{ d.wrong }}</span>
        </p>

        <div class="row">
          <button class="btn primary" :disabled="busy" @click="begin(d.id, 'study', 'unpassed')">
            {{ d.passed ? '▶️ Продолжить' : '▶️ Начать' }}
          </button>
          <button class="btn" :disabled="busy || !d.wrong" @click="begin(d.id, 'study', 'wrong')">
            🔁 Ошибки{{ d.wrong ? ` (${d.wrong})` : '' }}
          </button>
        </div>
        <div class="row">
          <button class="btn" :disabled="busy" @click="begin(d.id, 'exam', 'all')">
            🎓 Экзамен · {{ d.exam_size }} вопросов, {{ d.exam_minutes }} мин
          </button>
          <button class="btn" :disabled="busy" @click="begin(d.id, 'study', 'all')">
            🔀 Все вопросы
          </button>
        </div>

        <button class="link" @click="openGroups(d.id)">
          {{ groupsOpen ? '▾' : '▸' }} По темам
        </button>
        <div v-if="groupsOpen" class="groups">
          <button v-for="g in groups" :key="g.id" class="group"
                  :disabled="busy" @click="begin(d.id, 'study', 'group', g.id)">
            <span class="g-title">{{ g.num }}. {{ g.title }}</span>
            <span class="g-num">{{ g.passed }}/{{ g.total }}</span>
          </button>
        </div>

        <div class="row foot">
          <button class="link" @click="doReset(d, false)">🔄 Начать сначала</button>
          <button class="link danger" @click="doReset(d, true)">🗑 Стереть прогресс</button>
        </div>
        <p v-if="d.source_url" class="src">
          Источник: <a :href="d.source_url" target="_blank" rel="noopener">{{ d.source_url }}</a>
          <span v-if="d.revision"> · комплект от {{ d.revision }}</span>
        </p>
      </div>
    </template>

    <!-- ЭКРАН 3: итоги (после завершения) -->
    <template v-else-if="review">
      <div class="card">
        <h3 v-if="isExam">
          {{ session.passed ? '🎉 Экзамен сдан' : '❌ Экзамен не сдан' }}
        </h3>
        <h3 v-else>Прогон завершён</h3>
        <p class="stat">
          Верно {{ session.correct }} из {{ session.answered }}<span
            v-if="isExam && deck"> · допускалось ошибок: {{ deck.exam_allowed_mistakes }}</span>
        </p>
        <div class="row">
          <button class="btn primary" @click="exit">К списку</button>
        </div>
      </div>

      <p v-if="wrongItems.length" class="hint">Ошибки ({{ wrongItems.length }}):</p>
      <div v-for="it in wrongItems" :key="it.question.id" class="card">
        <p class="q-text">{{ it.question.text }}</p>
        <img v-if="it.question.image" class="q-img"
             :src="questionImageUrl(it.question.image)" alt="" loading="lazy" />
        <div v-for="(o, i) in it.question.options" :key="i"
             class="opt" :class="{ right: i === it.correct_idx, wrong: i === it.chosen_idx }">
          {{ o }}
        </div>
      </div>
    </template>

    <!-- ЭКРАН 2: прогон -->
    <template v-else>
      <div class="run-head">
        <button class="link" @click="exit">← Выйти</button>
        <span class="chip">{{ (session.answered ?? 0) + (answered ? 0 : 1) }} / {{ session.total }}</span>
        <span v-if="timeLeft" class="chip time" :class="{ hot: timeLeft < '1:00' }">⏱ {{ timeLeft }}</span>
      </div>
      <div class="bar">
        <div class="bar-fill" :style="{ width: (session.total ? session.answered / session.total * 100 : 0) + '%' }" />
      </div>

      <div v-if="question" class="card">
        <p v-if="question.group_title" class="q-group">{{ question.group_title }}</p>
        <p class="q-text">{{ question.text }}</p>
        <img v-if="question.image" class="q-img"
             :src="questionImageUrl(question.image)" alt="" loading="lazy" />

        <button v-for="(o, i) in question.options" :key="i" class="opt btn-opt"
                :class="{
                  right: answered && i === correctIdx,
                  wrong: answered && i === chosen && i !== correctIdx,
                  picked: !answered && i === chosen,
                }"
                :disabled="answered || busy" @click="answer(i)">
          {{ o }}
        </button>

        <p v-if="answered && !isExam" class="verdict" :class="{ ok: chosen === correctIdx }">
          {{ chosen === correctIdx ? '✅ Верно' : '❌ Неверно' }}
        </p>
        <div class="row">
          <button v-if="answered && !isExam" class="btn primary" @click="next">Дальше →</button>
          <button class="link" @click="stop">Завершить прогон</button>
        </div>
      </div>
      <p v-else class="hint">Вопросы кончились.</p>
    </template>
  </div>
</template>

<style scoped>
.card {
  background: var(--card-color);
  border-radius: 12px;
  padding: 12px 14px;
  margin-bottom: 12px;
  backdrop-filter: var(--card-blur);
}

.deck-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

h3 {
  margin: 0;
  font-size: 16px;
}

.hint {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 6px 0;
}

.stat {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 6px 0 10px;
}

.chip {
  font-size: 12px;
  background: var(--bg-color);
  border-radius: 999px;
  padding: 3px 10px;
  white-space: nowrap;
}

.chip.time.hot {
  color: #ef4444;
}

.bar {
  height: 6px;
  border-radius: 999px;
  background: var(--bg-color);
  overflow: hidden;
  margin: 8px 0;
}

.bar-fill {
  height: 100%;
  background: var(--accent-color);
  transition: width 0.3s;
}

.row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 8px;
}

.row.foot {
  justify-content: space-between;
  margin-top: 12px;
}

.btn {
  flex: 1;
  min-width: 130px;
  background: var(--bg-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 14px;
  padding: 10px 12px;
  cursor: pointer;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn:disabled {
  opacity: 0.5;
}

.link {
  background: none;
  border: none;
  color: var(--accent-color);
  font-size: 13px;
  padding: 6px 0;
  cursor: pointer;
}

.link.danger {
  color: #ef4444;
}

.groups {
  margin-top: 6px;
}

.group {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  background: var(--bg-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 13px;
  padding: 8px 10px;
  margin-bottom: 6px;
  text-align: left;
  cursor: pointer;
}

.g-num {
  color: var(--text-secondary);
  white-space: nowrap;
}

.src {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 10px 0 0;
  word-break: break-all;
}

.run-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.q-group {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 0 0 6px;
}

.q-text {
  font-size: 15px;
  margin: 0 0 10px;
  line-height: 1.35;
}

.q-img {
  display: block;
  width: 100%;
  max-width: 520px;
  border-radius: 8px;
  margin-bottom: 10px;
}

.opt {
  display: block;
  width: 100%;
  text-align: left;
  background: var(--bg-color);
  border: 2px solid transparent;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 14px;
  line-height: 1.35;
  padding: 10px 12px;
  margin-bottom: 8px;
}

.btn-opt {
  cursor: pointer;
}

.btn-opt:disabled {
  cursor: default;
  opacity: 1;
}

.opt.picked {
  border-color: var(--accent-color);
}

.opt.right {
  border-color: #22c55e;
}

.opt.wrong {
  border-color: #ef4444;
}

.verdict {
  font-size: 14px;
  color: #ef4444;
  margin: 4px 0 0;
}

.verdict.ok {
  color: #22c55e;
}
</style>
