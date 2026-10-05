<script setup lang="ts">
// Модалка настроек страницы. Открывается шестерёнкой в шапке.
// Общая часть (закрепление заголовка) есть у любой страницы, ниже —
// компонент из реестра pageSettings, если у страницы есть свои настройки.
import { computed, defineAsyncComponent, watch } from 'vue'
import { useRoute } from 'vue-router'
import { pageSettings } from '../shared/pageSettings'
import { isHeaderPinned, pinAllHeaders, setHeaderPinned } from '../shared/pinnedHeader'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const route = useRoute()
const title = computed(() => route.meta.title ?? '')
const routeName = computed(() => (typeof route.name === 'string' ? route.name : ''))

const headerPinned = computed(() => isHeaderPinned(routeName.value))
function toggleHeaderPinned(e: Event) {
  setHeaderPinned(routeName.value, (e.target as HTMLInputElement).checked)
}

const settingsComponent = computed(() => {
  const loader = pageSettings[routeName.value]
  return loader ? defineAsyncComponent(loader) : null
})

// если страница сменилась, пока модалка открыта — закрываем
watch(
  () => route.name,
  () => {
    if (props.open) emit('close')
  },
)
</script>

<template>
  <Transition name="fade">
    <div v-if="open" class="ps-overlay" @click.self="emit('close')">
      <div class="ps-panel">
        <header class="ps-head">
          <h2>Настройки · {{ title }}</h2>
          <button class="ps-close" aria-label="Закрыть" @click="emit('close')">✕</button>
        </header>
        <div class="ps-body">
          <section class="section">
            <h3>Заголовок</h3>
            <label class="radio">
              <input
                type="checkbox"
                :checked="headerPinned"
                :disabled="pinAllHeaders"
                @change="toggleHeaderPinned"
              />
              <span>Закрепить заголовок при прокрутке</span>
            </label>
            <p v-if="pinAllHeaders" class="ps-note">
              Включено сразу для всех страниц — в Settings → «Заголовки страниц».
            </p>
          </section>
          <component :is="settingsComponent" v-if="settingsComponent" />
        </div>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.ps-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  z-index: 1100;
  display: flex;
  justify-content: center;
  align-items: flex-start;
  overflow-y: auto;
  padding: 24px 12px;
}

.ps-panel {
  width: 100%;
  max-width: 760px;
  background: var(--bg-color);
  border-radius: 12px;
  padding: 12px 14px 16px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
}

.ps-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.ps-head h2 {
  margin: 0;
  font-size: 18px;
}

.ps-close {
  background: none;
  border: none;
  color: var(--text-color);
  font-size: 18px;
  padding: 4px 8px;
}

.section {
  background: var(--card-color);
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 14px;
}

.section h3 {
  margin: 0 0 6px;
  font-size: 16px;
}

.radio {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 0;
  cursor: pointer;
}

.ps-note {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 2px 0 0;
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
