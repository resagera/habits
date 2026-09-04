<script setup lang="ts">
// Блок «Оформление»: выбор темы, правка своей темы, рамки, сохранение тем.
//
// Правка любого цвета не меняет встроенную тему на месте, а копирует её в
// черновик «Своя тема» — иначе к исходной уже не вернуться. Черновик живёт на
// сервере, поэтому не теряется при перезаходе.
import { computed, ref } from 'vue'
import { confirmAction } from '../../../shared/telegram'
import { showToast } from '../../../shared/toast'
import {
  activeTheme, deleteSavedTheme, DRAFT_ID, editToken, resetToken, savedThemes,
  saveThemeAs, selectAuto, selectTheme, setBgOff, setDraft, state, themeById,
  themeBgUrl,
} from '../../../shared/appearance'
import {
  BUILTIN_THEMES, contrastRatio, generateTheme, TOKEN_LABELS,
  type Theme, type ThemeKind, type ThemeTokens,
} from '../../../shared/themes'
import { isTokenMode } from '../../../shared/auth'

const busy = ref(false)
const genColor = ref('#8b7cf6')
const saveName = ref('')
const showSave = ref(false)

const lightThemes = computed(() => BUILTIN_THEMES.filter((t) => t.kind === 'light'))
const darkThemes = computed(() => BUILTIN_THEMES.filter((t) => t.kind === 'dark'))
const isAuto = computed(() => state.value.mode === 'auto')
const currentId = computed(() => (isAuto.value ? 'auto' : state.value.theme_id))
const tokens = computed(() => activeTheme.value.tokens)
const isDraft = computed(() => !isAuto.value && state.value.theme_id === DRAFT_ID)

/** Контраст текста на фоне: своя тема легко получается нечитаемой. */
const contrast = computed(() => {
  const r = contrastRatio(tokens.value.text, tokens.value.bg)
  return r === null ? null : Math.round(r * 10) / 10
})

async function guard(fn: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  try {
    await fn()
  } catch {
    showToast('Не удалось сохранить оформление')
  } finally {
    busy.value = false
  }
}

const pick = (id: string) => guard(() => selectTheme(id))
const auto = () => guard(() => selectAuto(state.value.auto_light, state.value.auto_dark))

function setAutoPair(kind: ThemeKind, id: string) {
  return guard(() =>
    selectAuto(
      kind === 'light' ? id : state.value.auto_light,
      kind === 'dark' ? id : state.value.auto_dark,
    ),
  )
}

function onToken(key: keyof ThemeTokens, value: string | number) {
  return guard(() => editToken(key, value as never))
}

function generate() {
  return guard(() => setDraft(generateTheme(genColor.value, activeTheme.value.kind), activeTheme.value.kind))
}

async function doSave() {
  const name = saveName.value.trim()
  if (!name) {
    showToast('Впишите название темы')
    return
  }
  await guard(async () => {
    await saveThemeAs(name)
    showSave.value = false
    saveName.value = ''
    showToast('Тема сохранена ✅')
  })
}

async function removeTheme(id: number, name: string) {
  if (!(await confirmAction(`Удалить тему «${name}»?`))) return
  await guard(() => deleteSavedTheme(id))
}

/** Стиль мини-превью темы: фон, карточка, текст, кнопка. */
function preview(t: Theme) {
  return {
    background: t.tokens.bg,
    color: t.tokens.text,
    borderColor: t.tokens.accent,
  }
}

function cardPreview(t: Theme) {
  return { background: `rgb(${t.tokens.card_rgb})` }
}

function dotPreview(t: Theme) {
  return { background: t.tokens.accent }
}

const savedAsThemes = computed<Theme[]>(() =>
  savedThemes.value.map((s) => themeById(`saved:${s.id}`, s.kind)),
)

/** Картинка фона на карточке своей темы — сразу видно, какой это был вид. */
function savedBg(id: string): string {
  const saved = savedThemes.value.find((s) => `saved:${s.id}` === id)
  return saved ? themeBgUrl(saved) : ''
}
</script>

<template>
  <div class="ap">
    <!-- выбор темы -->
    <div class="sub">
      <h4>Тема</h4>
      <button class="theme-card auto" :class="{ on: isAuto }" @click="auto">
        <span class="t-name">🌗 Как в системе</span>
        <span class="t-sub">светлая: {{ themeById(state.auto_light).name }} · тёмная:
          {{ themeById(state.auto_dark).name }}</span>
      </button>
      <div v-if="isAuto" class="pair">
        <label>Светлая</label>
        <select :value="state.auto_light" @change="setAutoPair('light', ($event.target as HTMLSelectElement).value)">
          <option v-for="t in lightThemes" :key="t.id" :value="t.id">{{ t.name }}</option>
        </select>
        <label>Тёмная</label>
        <select :value="state.auto_dark" @change="setAutoPair('dark', ($event.target as HTMLSelectElement).value)">
          <option v-for="t in darkThemes" :key="t.id" :value="t.id">{{ t.name }}</option>
        </select>
      </div>

      <p class="hint">Тёмные</p>
      <div class="themes">
        <button v-for="t in darkThemes" :key="t.id" class="theme-card"
                :class="{ on: currentId === t.id }" :style="preview(t)" @click="pick(t.id)">
          <span class="t-card" :style="cardPreview(t)"><i :style="dotPreview(t)" /></span>
          <span class="t-name">{{ t.name }}</span>
        </button>
      </div>

      <p class="hint">Светлые</p>
      <div class="themes">
        <button v-for="t in lightThemes" :key="t.id" class="theme-card"
                :class="{ on: currentId === t.id }" :style="preview(t)" @click="pick(t.id)">
          <span class="t-card" :style="cardPreview(t)"><i :style="dotPreview(t)" /></span>
          <span class="t-name">{{ t.name }}</span>
        </button>
      </div>

      <template v-if="savedAsThemes.length">
        <p class="hint">Мои темы</p>
        <div class="themes">
          <div v-for="t in savedAsThemes" :key="t.id" class="saved-wrap">
            <button class="theme-card" :class="{ on: currentId === t.id }" :style="preview(t)"
                    @click="pick(t.id)">
              <img v-if="savedBg(t.id)" class="t-bg" :src="savedBg(t.id)" alt="" loading="lazy" />
              <span class="t-card" :style="cardPreview(t)"><i :style="dotPreview(t)" /></span>
              <span class="t-name">{{ t.name }}</span>
            </button>
            <button class="del" title="Удалить тему"
                    @click="removeTheme(Number(t.id.slice(6)), t.name)">✕</button>
          </div>
        </div>
      </template>

      <div v-if="isDraft" class="draft-row">
        <span class="draft-mark">✎ Своя тема — не сохранена</span>
        <button class="btn small" @click="showSave = !showSave">Сохранить как…</button>
      </div>
      <div v-if="showSave" class="save-row">
        <input v-model="saveName" placeholder="Название темы" />
        <button class="btn small primary" :disabled="busy" @click="doSave">Сохранить</button>
      </div>
    </div>

    <!-- цвета -->
    <div class="sub">
      <h4>Цвета</h4>
      <p class="hint">
        Меняете цвет у встроенной темы — она копируется в «Свою тему», исходная
        остаётся на месте.
      </p>
      <div v-for="t in TOKEN_LABELS" :key="t.key" class="color-row">
        <label>{{ t.label }}</label>
        <input type="color" :value="tokens[t.key]"
               @change="onToken(t.key, ($event.target as HTMLInputElement).value)" />
        <button v-if="isDraft && state.draft && t.key in state.draft" class="reset"
                title="Вернуть цвет темы" @click="guard(() => resetToken(t.key))">↺</button>
      </div>
      <p v-if="contrast !== null" class="hint" :class="{ warn: contrast < 4.5 }">
        Контраст текста на фоне: {{ contrast }}:1<template v-if="contrast < 4.5">
          — мало, читаться будет плохо (норма от 4.5)</template>
      </p>
    </div>

    <!-- карточки -->
    <div class="sub">
      <h4>Карточки</h4>
      <label class="slider">
        <span>Непрозрачность: {{ Math.round(tokens.card_alpha * 100) }}%</span>
        <input type="range" min="20" max="100" step="5" :value="Math.round(tokens.card_alpha * 100)"
               @change="onToken('card_alpha', Number(($event.target as HTMLInputElement).value) / 100)" />
      </label>
      <label class="slider">
        <span>Размытие под карточкой: {{ tokens.card_blur }}px</span>
        <input type="range" min="0" max="30" :value="tokens.card_blur"
               @change="onToken('card_blur', Number(($event.target as HTMLInputElement).value))" />
      </label>
    </div>

    <!-- рамки -->
    <div class="sub">
      <h4>Рамки</h4>
      <p class="hint">
        Рамка рисуется внутрь блока, поэтому ничего не разъезжается: ширина
        забирается у самого блока.
      </p>
      <label class="slider">
        <span>Рамка карточек: {{ tokens.border_card_width }}px</span>
        <input type="range" min="0" max="6" :value="tokens.border_card_width"
               @change="onToken('border_card_width', Number(($event.target as HTMLInputElement).value))" />
      </label>
      <label class="slider">
        <span>Рамка кнопок: {{ tokens.border_btn_width }}px</span>
        <input type="range" min="0" max="6" :value="tokens.border_btn_width"
               @change="onToken('border_btn_width', Number(($event.target as HTMLInputElement).value))" />
      </label>
    </div>

    <!-- генератор -->
    <div class="sub">
      <h4>Собрать тему по цвету</h4>
      <p class="hint">Один акцентный цвет — и фон, карточки и текст подбираются к нему.</p>
      <div class="gen-row">
        <input v-model="genColor" type="color" />
        <button class="btn small" :disabled="busy" @click="generate">Собрать</button>
      </div>
    </div>

    <div v-if="isTokenMode" class="sub">
      <label class="chk">
        <input type="checkbox" :checked="state.bg_off === true"
               @change="guard(() => setBgOff(($event.target as HTMLInputElement).checked))" />
        <span>Без фоновой картинки (в этом браузере)</span>
      </label>
    </div>
  </div>
</template>

<style scoped>
.sub {
  margin-bottom: 16px;
}

h4 {
  margin: 0 0 6px;
  font-size: 15px;
}

.hint {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 6px 0;
}

.hint.warn {
  color: #f59e0b;
}

.themes {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(112px, 1fr));
  gap: 8px;
  margin-bottom: 8px;
}

.theme-card {
  position: relative;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  gap: 6px;
  border: 2px solid transparent;
  border-radius: 10px;
  padding: 8px;
  cursor: pointer;
  text-align: left;
  font-size: 12px;
  min-height: 64px;
}

.theme-card.on {
  outline: 2px solid var(--accent-color);
  outline-offset: 1px;
}

.theme-card.auto {
  width: 100%;
  background: var(--card-color);
  color: var(--text-color);
  margin-bottom: 8px;
}

/* картинка фона темы — подложкой под превью карточки и подписью */
.t-bg {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  opacity: 0.55;
}

.t-card {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  border-radius: 6px;
  height: 22px;
  padding: 0 6px;
}

.t-card i {
  width: 12px;
  height: 12px;
  border-radius: 50%;
}

.t-name {
  position: relative;
  font-weight: 600;
}

.t-sub {
  font-size: 11px;
  opacity: 0.75;
}

.saved-wrap {
  position: relative;
}

.saved-wrap .theme-card {
  width: 100%;
}

.del {
  position: absolute;
  top: 2px;
  right: 2px;
  background: rgba(0, 0, 0, 0.35);
  border: none;
  border-radius: 6px;
  color: #fff;
  cursor: pointer;
  font-size: 11px;
  padding: 2px 5px;
}

.pair {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 6px 8px;
  align-items: center;
  margin-bottom: 10px;
  font-size: 13px;
}

.pair select {
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  padding: 6px 8px;
}

.draft-row,
.save-row,
.gen-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
  flex-wrap: wrap;
}

.draft-mark {
  font-size: 13px;
  color: var(--accent-color);
}

.save-row input {
  flex: 1;
  min-width: 140px;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  padding: 8px 10px;
}

.color-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 4px 0;
  font-size: 14px;
}

.color-row label {
  flex: 1;
}

.color-row input[type='color'] {
  width: 42px;
  height: 28px;
  border: none;
  background: none;
  padding: 0;
}

.reset {
  background: none;
  border: none;
  color: var(--accent-color);
  cursor: pointer;
  font-size: 14px;
}

.slider {
  display: block;
  font-size: 13px;
  margin: 8px 0;
}

.slider input {
  width: 100%;
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

.btn.small {
  font-size: 13px;
  padding: 6px 10px;
}

.chk {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  cursor: pointer;
}
</style>
