<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { pageAllowed } from '../shared/access'
import { isMenuPinned, pageRank, sortPages, toggleMenuPin } from '../shared/pageOrder'
import { isAdmin } from '../shared/me'
import { APP_VERSION } from '../shared/version'

defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const router = useRouter()
const route = useRoute()

// «Главная» всегда первой и вне сортировки — это точка входа, а не страница
// с данными. Остальное (включая админские пункты) сортируется вместе:
// закреплённые сверху, дальше заполненные, внизу пустые.
const home = computed(() => router.getRoutes().filter((r) => r.name === 'main'))
const rest = computed(() =>
  sortPages([
    ...router.getRoutes().filter((r) => r.meta.app && pageAllowed(String(r.name))),
    ...(isAdmin.value ? router.getRoutes().filter((r) => r.meta.admin) : []),
  ]),
)

// Разделитель перед первой пустой страницей: без него граница между «чем
// пользуюсь» и «остальным» не видна и порядок кажется случайным.
const firstEmpty = computed(() => rest.value.findIndex((r) => pageRank(String(r.name)) === 2))

const items = computed(() => [...home.value, ...rest.value])

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
    <h2 class="drawer-title">
      Habits
      <!-- версия приложения — только админу, как и версии страниц рядом -->
      <span v-if="isAdmin" class="drawer-app-ver">v{{ APP_VERSION }}</span>
    </h2>
    <nav>
      <template v-for="(item, i) in items" :key="item.path">
        <div v-if="firstEmpty >= 0 && i === firstEmpty + 1" class="drawer-split">без данных</div>
        <div class="drawer-row">
          <button
            class="drawer-item"
            :class="{ active: route.path === item.path, pinnable: item.name !== 'main' }"
            @click="go(item.path)"
          >
            <span class="drawer-icon">{{ item.meta.icon }}</span>
            <span class="drawer-name">{{ item.name === 'main' ? 'Главная' : item.meta.title }}</span>
            <!-- версия страницы — только админу: остальным она ничего не говорит -->
            <span v-if="isAdmin && item.meta.version" class="drawer-ver">
              v{{ item.meta.version }}
            </span>
          </button>
          <!-- закрепить: прижата к правому краю меню, «Главная» и так первая -->
          <button
            v-if="item.name !== 'main'"
            class="drawer-pin"
            :class="{ on: isMenuPinned(String(item.name)) }"
            :title="isMenuPinned(String(item.name)) ? 'Открепить' : 'Закрепить вверху'"
            @click.stop="toggleMenuPin(String(item.name))"
          >
            📌
          </button>
        </div>
      </template>
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
  display: flex;
  align-items: baseline;
  gap: 8px;
  margin: 4px 10px 14px;
  font-size: 20px;
}

.drawer-row {
  position: relative;
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

/* место под кнопку закрепления, чтобы длинное имя не заезжало под неё */
.drawer-item.pinnable {
  padding-right: 38px;
}

.drawer-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Граница между «чем пользуюсь» и остальным: без неё порядок читается как
   случайный. Линия с подписью, а не пустой отступ. */
.drawer-split {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 10px 12px 6px;
  font-size: 11px;
  color: var(--text-secondary);
  white-space: nowrap;
}

.drawer-split::after {
  content: '';
  flex: 1;
  height: 1px;
  background: var(--text-secondary);
  opacity: 0.25;
}

/* Приклеена к правому краю меню и лежит НАД пунктом, а не внутри него:
   кнопка в кнопке — невалидная вёрстка. */
.drawer-pin {
  position: absolute;
  right: 4px;
  top: 50%;
  transform: translateY(-50%);
  width: 30px;
  height: 30px;
  padding: 0;
  border: none;
  border-radius: 8px;
  background: none;
  font-size: 14px;
  line-height: 1;
  opacity: 0.25;
  filter: grayscale(1);
}

.drawer-pin.on {
  opacity: 1;
  filter: none;
}

.drawer-pin:active {
  background: var(--bg-secondary);
}

.drawer-app-ver {
  font-size: 12px;
  font-weight: 400;
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
}

/* Прижата вправо и приглушена: это служебная подпись, а не часть названия. */
.drawer-ver {
  margin-left: auto;
  font-size: 12px;
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
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
