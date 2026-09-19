<script setup lang="ts">
// Настройки приложения. Оформление (темы, своя тема, фон) вынесено в
// отдельные компоненты — раньше оно занимало здесь полфайла.
import { onMounted, ref } from 'vue'
import { APP_VERSION } from '../../shared/version'
import { me } from '../../shared/me'
import AccessTokens from './components/AccessTokens.vue'
import AppearanceBlock from './components/AppearanceBlock.vue'
import BackgroundPicker from './components/BackgroundPicker.vue'
import VisiblePages from './components/VisiblePages.vue'
import { isTokenMode } from '../../shared/auth'
import { loadCollapsed, saveCollapsed } from '../../shared/collapsed'
import { pinAllHeaders, setPinAllHeaders } from '../../shared/pinnedHeader'

// свёрнутость блока «Оформление» хранится на сервере: в localStorage она
// терялась так же, как настройка закреплённого заголовка
const appearanceOpen = ref(true)

const APPEARANCE_KEY = 1 // единственный сворачиваемый блок настроек

onMounted(async () => {
  appearanceOpen.value = !(await loadCollapsed('settings')).has(APPEARANCE_KEY)
})

function toggleAppearance() {
  appearanceOpen.value = !appearanceOpen.value
  saveCollapsed('settings', new Set(appearanceOpen.value ? [] : [APPEARANCE_KEY]))
}

function onPinAll(e: Event) {
  setPinAllHeaders((e.target as HTMLInputElement).checked)
}
</script>

<template>
  <!--
    Оформление: выбор темы и своя тема — в AppearanceBlock, фон с папками и
    общей галереей — в BackgroundPicker. Раньше всё это жило прямо здесь и
    занимало полфайла.

    Блок доступен и при входе по токену: тема в браузере своя (сервер сам
    пишет в отдельную колонку), а фон общий.
  -->
  <section class="section">
    <button class="collapse-head" @click="toggleAppearance">
      <h3>Оформление</h3>
      <span class="chev">{{ appearanceOpen ? '▾' : '▸' }}</span>
    </button>

    <div v-show="appearanceOpen" class="collapse-body">
      <AppearanceBlock />
      <BackgroundPicker />
    </div>
  </section>

  <!--
    Закреплённые заголовки — отдельная секция, а не часть «Оформления»:
    настройка общая для всех режимов входа (Telegram, веб, расширение), тогда
    как «Оформление» в браузере подписано «на Telegram не влияет».
  -->
  <section class="section">
    <h3>Заголовки страниц</h3>
    <label class="radio">
      <input type="checkbox" :checked="pinAllHeaders" @change="onPinAll($event)" />
      <span>Закрепить заголовок на всех страницах</span>
    </label>
    <p class="hint-text">
      При прокрутке шапка с меню и шестерёнкой остаётся вверху. Можно включать
      и точечно — шестерёнкой на нужной странице.
    </p>
  </section>

  <VisiblePages />

  <AccessTokens v-if="!isTokenMode" />

  <section class="section">
    <h3>О приложении</h3>
    <p class="hint-text">
      Версия {{ APP_VERSION }}<template v-if="me">
        · {{ me.first_name || me.username || 'пользователь' }} (id {{ me.id }})</template
      >
    </p>
  </section>

</template>

<style scoped>
.section {
  background: var(--card-color);
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 14px;
}

.section h3 {
  margin: 0 0 10px;
  font-size: 16px;
}

.collapse-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  background: none;
  border: none;
  padding: 0;
  color: var(--text-color);
  cursor: pointer;
}

.collapse-head h3 {
  margin: 0;
  font-size: 16px;
}

.chev {
  font-size: 14px;
  color: var(--text-secondary);
}

.collapse-body {
  margin-top: 12px;
}

.sub {
  padding: 10px 0;
  border-top: 1px solid var(--hover-bg-color);
}

.sub:first-child {
  padding-top: 0;
  border-top: none;
}

.sub h4 {
  margin: 0 0 10px;
  font-size: 15px;
}

.radio {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 0;
  cursor: pointer;
}

.row {
  display: flex;
  gap: 8px;
}

.color-wrap {
  display: flex;
  align-items: center;
  gap: 4px;
}

.color-mini {
  width: 46px !important;
  height: 32px;
  padding: 2px;
  margin: 0 !important;
}

.mini-x {
  background: var(--bg-secondary);
  border: none;
  border-radius: 6px;
  padding: 4px 8px;
  color: var(--text-color);
}

.bot-hint {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 8px 0;
  text-align: left;
}

.bot-hint.waiting {
  text-align: center;
  color: var(--accent-color);
}

.btn {
  flex: 1;
  padding: 10px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
}

.btn:disabled {
  opacity: 0.5;
}

.lbl-inline {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 6px 0 4px;
}

.full-w {
  width: 100%;
  margin-bottom: 8px;
  font: inherit;
  background: var(--bg-secondary);
  color: var(--text-color);
  border: 1px solid var(--hover-bg-color);
  border-radius: 6px;
  padding: 8px;
}

.hint-text {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 10px;
}

.bg-controls .row {
  margin-bottom: 8px;
}

.bg-url-row input {
  flex: 1;
  min-width: 0;
}

.btn.slim {
  flex: none;
}

.bg-pos {
  display: flex;
  align-items: center;
  gap: 8px;
}

.bg-slider {
  display: block;
  margin-top: 10px;
  font-size: 13px;
  color: var(--text-secondary);
}

.bg-slider input {
  width: 100%;
  padding: 0;
  border: none;
  background: none;
  accent-color: var(--accent-color);
}

.bg-pos select {
  flex: 1;
}

.bg-gallery {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(76px, 1fr));
  gap: 8px;
  margin-top: 10px;
}

.bg-thumb-wrap {
  position: relative;
}

.bg-thumb {
  width: 100%;
  aspect-ratio: 1 / 1;
  object-fit: cover;
  border-radius: 8px;
  cursor: pointer;
  border: 2px solid transparent;
}

.bg-thumb.current {
  border-color: var(--accent-color);
}

.bg-thumb-del {
  position: absolute;
  top: 2px;
  right: 2px;
  background: rgba(0, 0, 0, 0.6);
  color: #fff;
  border: none;
  border-radius: 50%;
  width: 20px;
  height: 20px;
  font-size: 11px;
  line-height: 1;
}

.hidden-input {
  display: none;
}

/* карточки-«стекло»: размытие фона под .section (класс неоднозначный —
   правило scoped, чтобы не задеть одноимённые не-карточки) */
:root[data-card-glass] .section {
  backdrop-filter: blur(var(--card-blur, 0px));
  -webkit-backdrop-filter: blur(var(--card-blur, 0px));
}
</style>
