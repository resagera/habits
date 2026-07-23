<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import AppMenu from './components/AppMenu.vue'
import AppToast from './components/AppToast.vue'

const route = useRoute()
const menuOpen = ref(false)

// закрываем меню при смене роута (на случай навигации извне меню)
watch(() => route.path, () => (menuOpen.value = false))
</script>

<template>
  <!-- iOS-безопасный слой фона: fixed-див вместо background-attachment: fixed -->
  <div id="app-background" aria-hidden="true"></div>
  <div id="app-background-dim" aria-hidden="true"></div>

  <header class="app-header">
    <button class="burger" aria-label="Меню" @click="menuOpen = true">
      <svg width="22" height="22" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
        <rect x="3" y="6" width="18" height="2" rx="1" fill="currentColor" />
        <rect x="3" y="11" width="18" height="2" rx="1" fill="currentColor" />
        <rect x="3" y="16" width="12" height="2" rx="1" fill="currentColor" />
      </svg>
    </button>
    <h1>{{ route.meta.title ?? 'Habits' }}</h1>
  </header>

  <main class="app-main">
    <RouterView />
  </main>

  <AppMenu :open="menuOpen" @close="menuOpen = false" />
  <AppToast />
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
}

.app-main {
  padding: 12px 16px 24px;
  max-width: 760px;
  margin: 0 auto;
}
</style>
