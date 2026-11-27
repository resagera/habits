<script setup lang="ts">
// Настройки приложения. Каждая секция — SettingsSection: заголовок со
// стрелкой, свёрнутость на сервере (localStorage в Telegram-webview
// периодически очищается) и участие в поиске по настройкам.
//
// Оформление (темы, своя тема, фон) живёт в отдельных компонентах — раньше
// оно занимало здесь полфайла.
import { computed, onMounted, provide, ref } from 'vue'
import { APP_VERSION } from '../../shared/version'
import { me } from '../../shared/me'
import AccessTokens from './components/AccessTokens.vue'
import AppearanceBlock from './components/AppearanceBlock.vue'
import BackgroundPicker from './components/BackgroundPicker.vue'
import ProfileBlock from './components/ProfileBlock.vue'
import SettingsSection from './components/SettingsSection.vue'
import NotifyBlock from './components/NotifyBlock.vue'
import TransferBlock from './components/TransferBlock.vue'
import DataBlock from './components/DataBlock.vue'
import VisiblePages from './components/VisiblePages.vue'
import { isTokenMode } from '../../shared/auth'
import { loadCollapsed, saveCollapsed } from '../../shared/collapsed'
import { pinAllHeaders, setPinAllHeaders } from '../../shared/pinnedHeader'
import { collapseKey, matchSection, searchKey, SECTIONS } from './sections'

const search = ref('')
const collapsed = ref<Set<number>>(new Set())

provide(searchKey, search)
provide(collapseKey, {
  isOpen: (id: number) => !collapsed.value.has(id),
  toggle(id: number) {
    const next = new Set(collapsed.value)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    collapsed.value = next
    saveCollapsed('settings', next)
  },
})

onMounted(async () => {
  collapsed.value = await loadCollapsed('settings')
})

/** Секции, которые вообще есть на странице у этого входа. */
const available = computed(() =>
  Object.values(SECTIONS).filter((s) => s.id !== SECTIONS.tokens.id || !isTokenMode.value),
)

const found = computed(() => available.value.filter((s) => matchSection(s, search.value)))

function onPinAll(e: Event) {
  setPinAllHeaders((e.target as HTMLInputElement).checked)
}
</script>

<template>
  <div class="search">
    <input v-model="search" placeholder="Поиск по настройкам: тема, фон, токен…" />
    <button v-if="search" class="clear" aria-label="Очистить" @click="search = ''">✕</button>
  </div>

  <p v-if="!found.length" class="empty">Ничего не нашлось. Попробуйте другое слово.</p>

  <SettingsSection :s="SECTIONS.profile">
    <ProfileBlock />
  </SettingsSection>

  <!--
    Блок доступен и при входе по токену: тема в браузере своя (сервер сам
    пишет в отдельную колонку), а фон общий.
  -->
  <SettingsSection :s="SECTIONS.appearance">
    <AppearanceBlock />
    <BackgroundPicker />
  </SettingsSection>

  <!--
    Закреплённые заголовки — отдельная секция, а не часть «Оформления»:
    настройка общая для всех режимов входа (Telegram, веб, расширение), тогда
    как «Оформление» в браузере подписано «на Telegram не влияет».
  -->
  <SettingsSection :s="SECTIONS.headers">
    <label class="radio">
      <input type="checkbox" :checked="pinAllHeaders" @change="onPinAll($event)" />
      <span>Закрепить заголовок на всех страницах</span>
    </label>
    <p class="hint-text">
      При прокрутке шапка с меню и шестерёнкой остаётся вверху. Можно включать
      и точечно — шестерёнкой на нужной странице.
    </p>
  </SettingsSection>

  <SettingsSection :s="SECTIONS.pages">
    <VisiblePages />
  </SettingsSection>

  <SettingsSection v-if="!isTokenMode" :s="SECTIONS.tokens">
    <AccessTokens />
  </SettingsSection>

  <SettingsSection :s="SECTIONS.notifications">
    <NotifyBlock />
  </SettingsSection>

  <SettingsSection :s="SECTIONS.transfer">
    <TransferBlock />
  </SettingsSection>

  <SettingsSection :s="SECTIONS.data">
    <DataBlock />
  </SettingsSection>

  <SettingsSection :s="SECTIONS.about">
    <p class="hint-text">
      Версия {{ APP_VERSION }}<template v-if="me">
        · {{ me.first_name || me.username || 'пользователь' }} (id {{ me.id }})</template
      >
    </p>
  </SettingsSection>
</template>

<style scoped>
.search {
  position: relative;
  margin-bottom: 14px;
}

.search input {
  width: 100%;
  box-sizing: border-box;
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 15px;
  padding: 10px 34px 10px 12px;
}

.clear {
  position: absolute;
  right: 6px;
  top: 50%;
  transform: translateY(-50%);
  background: none;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 14px;
  padding: 6px;
}

.empty {
  color: var(--text-secondary);
  font-size: 14px;
  margin: 0 0 14px;
}

.radio {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 0;
  font-size: 15px;
  cursor: pointer;
}

.hint-text {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--text-secondary);
}
</style>
