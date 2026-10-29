<script setup lang="ts">
// Страница «Игры»: список игр, партия и настройки конкретной игры.
//
// Игра — это компонент, а не роут: внутри партии хочется «назад к играм», а
// не назад в меню браузера, и адрес страницы при этом не должен меняться.
import { computed, ref, onMounted, shallowRef } from 'vue'
import { applyBackground, currentBackground, resolveBgUrl } from '../../shared/background'
import { showToast } from '../../shared/toast'
import { loadGames, saveBackgrounds, sendResult } from './api'
import Game2048 from './components/Game2048.vue'
import GameFifteen from './components/GameFifteen.vue'
import GameMaze from './components/GameMaze.vue'
import GamePuzzle from './components/GamePuzzle.vue'
import GameReaction from './components/GameReaction.vue'
import GamePairs from './components/GamePairs.vue'
import GameSettings from './components/GameSettings.vue'
import {
  bestScore, fmtBest, GAMES, type GameCode, type GameImage, type GameInfo,
  type GameResult, type GamesState, type SlotCode,
} from './types'

const state = ref<GamesState>({ scores: [], backgrounds: {} })
const active = ref<GameInfo | null>(null)
const settingsFor = ref<GameInfo | null>(null)
/** картинки выбираются один раз на партию — иначе менялись бы при каждой перерисовке */
const stageBg = ref('')
const cardBg = ref('')
const pictureBg = ref('')
const gameRef = shallowRef<{ report: () => void } | null>(null)

const components = {
  '2048': Game2048, fifteen: GameFifteen, pairs: GamePairs,
  maze: GameMaze, puzzle: GamePuzzle, reaction: GameReaction,
}

onMounted(async () => {
  try {
    state.value = await loadGames()
  } catch {
    /* нет сети — играть можно, рекорды просто не покажутся */
  }
})

/** Пропсы разные у разных игр: рубашка карт нужна «паре», рекорды — тем, у кого режимы. */
const gameProps = computed<Record<string, unknown>>(() => {
  const code = active.value?.code
  const scores = state.value.scores
  switch (code) {
    case 'pairs':
      return { cardBg: cardBg.value, scores }
    case 'puzzle':
      return { pictureBg: pictureBg.value }
    case 'maze':
    case 'reaction':
      return { scores }
    default:
      return {}
  }
})

/**
 * Случайная картинка набора в том виде, в каком её хранит сервер. Префикс
 * приложения добавляется там, где адрес уходит в CSS: applyBackground делает
 * это сам, и дважды добавленный «/» превращал путь в чужой домен.
 */
function pick(game: GameCode, slot: SlotCode): string {
  const list: GameImage[] = state.value.backgrounds[game]?.[slot] ?? []
  return list.length ? list[Math.floor(Math.random() * list.length)].url : ''
}

/**
 * Фон партии — это фон всей страницы, а не только поля: игра занимает экран
 * целиком, и картинка в рамке выглядела заплаткой. Прежний фон запоминаем и
 * возвращаем на выходе.
 */
let savedBg: ReturnType<typeof currentBackground> = null

function start(info: GameInfo) {
  stageBg.value = pick(info.code, 'game')
  cardBg.value = resolveBgUrl(pick(info.code, 'cards'))
  pictureBg.value = resolveBgUrl(pick(info.code, 'picture'))
  if (stageBg.value) {
    savedBg = currentBackground()
    // затемнение 30 %: поверх фото должно читаться поле любой игры
    applyBackground(stageBg.value, 'cover', 0, -30)
  }
  active.value = info
}

function restoreBg() {
  if (!savedBg) return
  applyBackground(savedBg.url, savedBg.position, savedBg.blur, savedBg.dim)
  savedBg = null
}

/** Выход из партии: даём игре досчитать незаконченный результат (2048). */
function back() {
  gameRef.value?.report()
  restoreBg()
  active.value = null
}

async function onFinish(result: GameResult) {
  const info = active.value
  if (!info) return
  try {
    const base = result.game ?? info.code
    const code = result.variant ? `${base}:${result.variant}` : base
    const { score } = await sendResult(code, result)
    state.value = {
      ...state.value,
      scores: [...state.value.scores.filter((s) => s.game !== code), score],
    }
  } catch {
    showToast('Результат не сохранился — нет связи')
  }
}

async function onSaveBackgrounds(slot: SlotCode, ids: number[]) {
  const info = settingsFor.value
  if (!info) return
  try {
    state.value = await saveBackgrounds(info.code, slot, ids)
  } catch {
    showToast('Не удалось сохранить картинки')
  }
}

const played = computed(() => state.value.scores.reduce((sum, s) => sum + s.played, 0))
</script>

<template>
  <!-- партия -->
  <template v-if="active">
    <div class="head">
      <button class="btn" @click="back">← К играм</button>
      <span class="now">{{ active.icon }} {{ active.title }}</span>
    </div>
    <div class="stage" :class="{ onpic: !!stageBg }">
      <component :is="components[active.code]" ref="gameRef" v-bind="gameProps"
                 @finish="onFinish"
                 @settings="restoreBg(); settingsFor = active; active = null" />
    </div>
  </template>

  <!-- настройки одной игры -->
  <template v-else-if="settingsFor">
    <div class="head">
      <button class="btn" @click="settingsFor = null">← К играм</button>
      <span class="now">{{ settingsFor.icon }} {{ settingsFor.title }} — настройки</span>
    </div>
    <GameSettings :game="settingsFor" :state="state" @save="onSaveBackgrounds" />
  </template>

  <!-- список игр -->
  <template v-else>
    <div v-for="g in GAMES" :key="g.code" class="card">
      <button class="open" @click="start(g)">
        <span class="icon">{{ g.icon }}</span>
        <span class="text">
          <span class="title">
            {{ g.title }}
            <!-- игры на двоих: человечек зовёт второго игрока -->
            <span v-if="g.invite" class="invite" title="Можно позвать друга">🧑‍🤝‍🧑</span>
          </span>
          <span class="about">{{ g.about }}</span>
          <span class="best">{{ fmtBest(g, bestScore(g, state.scores)) }}</span>
        </span>
      </button>
      <button class="gear" title="Настройки игры" @click="settingsFor = g">⚙</button>
    </div>
    <p v-if="played" class="hint">Сыграно партий: {{ played }}.</p>
  </template>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
}

.now {
  font-size: 15px;
  color: var(--text-secondary);
}

.stage {
  border-radius: 12px;
  padding: 12px;
  background-size: cover;
  background-position: center;
}

/*
  Картинку рисует страница (фон приложения подменяется на время партии), а
  сцена переопределяет переменные темы: внутри всё светлое независимо от темы
  приложения, и каждой игре не нужно знать, лежит под ней картинка или нет.
*/
.stage.onpic {
  --text-color: #fff;
  --text-secondary: rgba(255, 255, 255, 0.78);
  --card-color: rgba(0, 0, 0, 0.45);
  --cell-bg-color: rgba(255, 255, 255, 0.16);
  --bg-color: rgba(0, 0, 0, 0.35);
  color: #fff;
}

.card {
  display: flex;
  align-items: stretch;
  gap: 6px;
  background: var(--card-color);
  border-radius: 10px;
  margin-bottom: 10px;
  overflow: hidden;
}

.open {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 1;
  min-width: 0;
  text-align: left;
  background: none;
  border: none;
  color: var(--text-color);
  cursor: pointer;
  padding: 12px 0 12px 14px;
}

.gear {
  background: none;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 18px;
  padding: 0 14px;
}

.icon {
  font-size: 28px;
}

.text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.title {
  font-size: 16px;
}

.invite {
  font-size: 13px;
  margin-left: 4px;
}

.about,
.best {
  font-size: 12px;
  color: var(--text-secondary);
}

.best {
  color: var(--accent-color);
}

.hint {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 8px 0 0;
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
</style>
