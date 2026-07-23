<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { pageAllowed } from '../shared/access'

defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const router = useRouter()
const route = useRoute()
const items = computed(() => [
  ...router.getRoutes().filter((r) => r.name === 'main'),
  ...router.getRoutes().filter((r) => r.meta.app && pageAllowed(String(r.name))),
])

function go(path: string) {
  router.push(path)
  emit('close')
}
</script>

<template>
  <Transition name="fade">
    <div v-if="open" class="menu-overlay" @click="emit('close')"></div>
  </Transition>

  <aside class="drawer" :class="{ open }">
    <h2 class="drawer-title">Habits</h2>
    <nav>
      <button
        v-for="item in items"
        :key="item.path"
        class="drawer-item"
        :class="{ active: route.path === item.path }"
        @click="go(item.path)"
      >
        <span class="drawer-icon">{{ item.meta.icon }}</span>
        <span>{{ item.name === 'main' ? 'Главная' : item.meta.title }}</span>
      </button>
    </nav>
  </aside>
</template>

<style scoped>
.menu-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  z-index: 900;
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.25s;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}

.drawer {
  position: fixed;
  top: 0;
  left: 0;
  bottom: 0;
  width: min(260px, 78vw);
  background: var(--card-color);
  z-index: 1000;
  transform: translateX(-100%);
  transition: transform 0.25s ease;
  padding: 16px 10px;
  overflow-y: auto;
  box-shadow: 2px 0 16px rgba(0, 0, 0, 0.3);
}

.drawer.open {
  transform: translateX(0);
}

.drawer-title {
  margin: 4px 10px 14px;
  font-size: 20px;
}

.drawer-item {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  padding: 11px 12px;
  border: none;
  border-radius: 10px;
  background: none;
  color: var(--text-color);
  font-size: 15px;
  text-align: left;
}

.drawer-item.active {
  background: var(--bg-secondary);
  color: var(--accent-color);
  font-weight: 600;
}

.drawer-icon {
  font-size: 18px;
  width: 24px;
  text-align: center;
}
</style>
