<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import AppMenu from './components/AppMenu.vue'
import AppToast from './components/AppToast.vue'
import PageSettingsModal from './components/PageSettingsModal.vue'
import TokenGate from './components/TokenGate.vue'
import { needsToken } from './shared/auth'
import { isHeaderPinned } from './shared/pinnedHeader'

const route = useRoute()
const menuOpen = ref(false)
const pageSettingsOpen = ref(false)

// Закреплённая шапка — по настройке конкретной страницы (шестерёнка).
const headerPinned = computed(() => isHeaderPinned(typeof route.name === 'string' ? route.name : ''))

// Шестерёнка настроек страницы — на страницах-приложениях, кроме самих
// «Настроек» и «Админки» (они и есть настройки).
const showGear = computed(
  () => route.meta.app === true && route.name !== 'settings' && route.name !== 'admin',
)

// публичная страница (ссылка на статью или файл) — без меню, шапки и входа
const isPublic = computed(() => route.meta.public === true)

// закрываем меню при смене роута (на случай навигации извне меню)
watch(() => route.path, () => (menuOpen.value = false))
</script>

<template>
  <!-- публичная страница: только содержимое, без входа и без меню -->
  <template v-if="isPublic">
    <RouterView />
    <AppToast />
  </template>
  <!-- вне Telegram и без токена — экран входа вместо приложения -->
  <TokenGate v-else-if="needsToken" />
  <template v-else>
  <!-- iOS-безопасный слой фона: fixed-див вместо background-attachment: fixed -->
  <div id="app-background" aria-hidden="true"></div>
  <div id="app-background-dim" aria-hidden="true"></div>

  <header class="app-header" :class="{ pinned: headerPinned }">
    <button class="burger" aria-label="Меню" @click="menuOpen = true">
      <svg width="22" height="22" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
        <rect x="3" y="6" width="18" height="2" rx="1" fill="currentColor" />
        <rect x="3" y="11" width="18" height="2" rx="1" fill="currentColor" />
        <rect x="3" y="16" width="12" height="2" rx="1" fill="currentColor" />
      </svg>
    </button>
    <h1>{{ route.meta.title ?? 'Habits' }}</h1>
    <button
      v-if="showGear"
      class="page-gear"
      aria-label="Настройки страницы"
      @click="pageSettingsOpen = true"
    >
      <svg width="20" height="20" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
        <path
          fill="currentColor"
          d="M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8Zm0 6a2 2 0 1 1 0-4 2 2 0 0 1 0 4Zm7.4-2c0-.34-.03-.66-.07-.99l1.7-1.32a.5.5 0 0 0 .12-.64l-1.6-2.78a.5.5 0 0 0-.6-.22l-2 .8a6.6 6.6 0 0 0-1.72-1l-.3-2.12a.5.5 0 0 0-.5-.42h-3.2a.5.5 0 0 0-.5.42l-.3 2.12c-.62.25-1.2.59-1.72 1l-2-.8a.5.5 0 0 0-.6.22l-1.6 2.78a.5.5 0 0 0 .12.64l1.7 1.32c-.04.33-.07.65-.07.99s.03.66.07.99l-1.7 1.32a.5.5 0 0 0-.12.64l1.6 2.78a.5.5 0 0 0 .6.22l2-.8c.52.41 1.1.75 1.72 1l.3 2.12a.5.5 0 0 0 .5.42h3.2a.5.5 0 0 0 .5-.42l.3-2.12c.62-.25 1.2-.59 1.72-1l2 .8a.5.5 0 0 0 .6-.22l1.6-2.78a.5.5 0 0 0-.12-.64l-1.7-1.32c.04-.33.07-.65.07-.99Z"
        />
      </svg>
    </button>
  </header>

  <main class="app-main">
    <RouterView />
  </main>

  <AppMenu :open="menuOpen" @close="menuOpen = false" />
  <PageSettingsModal :open="pageSettingsOpen" @close="pageSettingsOpen = false" />
  <AppToast />
  </template>
</template>

<style scoped>
.app-header {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 16px 0;
  max-width: 760px;
  margin: 0 auto;
}

/* Закреплённая шапка: липнет к верху при прокрутке. Сплошной фон, чтобы
   контент не просвечивал под ней; лёгкая тень отделяет от списка. */
.app-header.pinned {
  position: sticky;
  top: 0;
  z-index: 50;
  padding-bottom: 10px;
  background: var(--bg-color);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.18);
}

.burger {
  background: none;
  border: none;
  color: var(--text-color);
  padding: 6px;
  display: flex;
}

.app-header h1 {
  margin: 0;
  font-size: 22px;
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.page-gear {
  background: none;
  border: none;
  color: var(--text-color);
  padding: 6px;
  display: flex;
  flex: none;
  opacity: 0.85;
}

.app-main {
  padding: 12px 16px 24px;
  max-width: 760px;
  margin: 0 auto;
}
</style>
