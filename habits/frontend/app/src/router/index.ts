import { createRouter, createWebHistory } from 'vue-router'

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    icon?: string
    /** показывать в меню и на плитках главной */
    app?: boolean
  }
}

// Каждое мини-приложение — отдельный лениво загружаемый чанк.
const routes = [
  { path: '/', name: 'main', component: () => import('../apps/main/MainView.vue'), meta: { title: 'Habits', icon: '🏠' } },
  { path: '/tracker', name: 'tracker', component: () => import('../apps/tracker/TrackerView.vue'), meta: { title: 'Tracker', icon: '📊', app: true } },
  { path: '/checker', name: 'checker', component: () => import('../apps/checker/CheckerView.vue'), meta: { title: 'Checker', icon: '✅', app: true } },
  { path: '/tasks', name: 'tasks', component: () => import('../apps/tasks/TasksView.vue'), meta: { title: 'Tasks', icon: '🗂', app: true } },
  { path: '/diary', name: 'diary', component: () => import('../apps/diary/DiaryView.vue'), meta: { title: 'Diary', icon: '📔', app: true } },
  { path: '/metrics', name: 'metrics', component: () => import('../apps/metrics/MetricsView.vue'), meta: { title: 'Metrics', icon: '📈', app: true } },
  { path: '/passwords', name: 'passwords', component: () => import('../apps/passwords/PasswordsView.vue'), meta: { title: 'Passwords', icon: '🔑', app: true } },
  { path: '/reminders', name: 'reminders', component: () => import('../apps/reminders/RemindersView.vue'), meta: { title: 'Reminders', icon: '🔔', app: true } },
  { path: '/converter', name: 'converter', component: () => import('../apps/converter/ConverterView.vue'), meta: { title: 'Converter', icon: '💱', app: true } },
  { path: '/currency', redirect: '/converter' }, // старое имя вкладки Exchanges
  { path: '/links', name: 'links', component: () => import('../apps/links/LinksView.vue'), meta: { title: 'Links', icon: '🔗', app: true } },
  { path: '/articles', name: 'articles', component: () => import('../apps/articles/ArticlesView.vue'), meta: { title: 'Articles', icon: '📄', app: true } },
  { path: '/servers', name: 'servers', component: () => import('../apps/servers/ServersView.vue'), meta: { title: 'Servers', icon: '🖥', app: true } },
  { path: '/files', name: 'files', component: () => import('../apps/files/FilesView.vue'), meta: { title: 'My Files', icon: '📁', app: true } },
  { path: '/terminal', name: 'terminal', component: () => import('../apps/terminal/TerminalView.vue'), meta: { title: 'Terminal', icon: '⌨️', app: true } },
  { path: '/contacts', name: 'contacts', component: () => import('../apps/contacts/ContactsView.vue'), meta: { title: 'Contacts', icon: '👥', app: true } },
  { path: '/projects', name: 'projects', component: () => import('../apps/projects/ProjectsView.vue'), meta: { title: 'Projects', icon: '📦', app: true } },
  { path: '/food', name: 'food', component: () => import('../apps/food/FoodView.vue'), meta: { title: 'Food', icon: '🍽', app: true } },
  { path: '/automation', name: 'automation', component: () => import('../apps/automation/AutomationView.vue'), meta: { title: 'Автоматизация', icon: '🤖', app: true } },
  { path: '/help', name: 'help', component: () => import('../apps/help/HelpView.vue'), meta: { title: 'Help', icon: '🆘', app: true } },
  { path: '/settings', name: 'settings', component: () => import('../apps/settings/SettingsView.vue'), meta: { title: 'Settings', icon: '⚙️', app: true } },
  // Публичное чтение статьи по ссылке (работает в браузере без Telegram)
  { path: '/read/:token([0-9a-f]{24})', name: 'read', component: () => import('../apps/articles/PublicReadView.vue'), meta: { title: 'Habits' } },
  // Всё неизвестное — на главную (в т.ч. если что-то попало в путь при запуске)
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

// ВАЖНО: history-режим, а не hash. Telegram Mini Apps передают параметры
// запуска во фрагменте URL (#tgWebAppData=...), и hash-роутер принимал их
// за несуществующий маршрут — приложение открывалось пустым.
// Бэкенд отдаёт index.html для любого неизвестного пути (SPA fallback).
const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
})

export const REMEMBER_TAB_KEY = 'remember_last_tab'
export const LAST_TAB_KEY = 'last_tab'

export function rememberTabEnabled(): boolean {
  // по умолчанию включено; '0' остаётся у тех, кто отключал опцию раньше
  return localStorage.getItem(REMEMBER_TAB_KEY) !== '0'
}

export function setRememberTab(enabled: boolean): void {
  localStorage.setItem(REMEMBER_TAB_KEY, enabled ? '1' : '0')
  if (!enabled) localStorage.removeItem(LAST_TAB_KEY)
}

router.afterEach((to) => {
  if (rememberTabEnabled() && to.name !== 'main' && to.name !== 'read') {
    localStorage.setItem(LAST_TAB_KEY, to.fullPath)
  }
})

// Страницы с персональным доступом: недоступные уводят на главную.
router.beforeEach(async (to) => {
  const { accessLoaded, pageAllowed } = await import('../shared/access')
  if (accessLoaded.value && typeof to.name === 'string' && !pageAllowed(to.name)) {
    return { path: '/' }
  }
  return true
})

/** Восстановление последней вкладки при запуске (вызывается из main.ts). */
export async function restoreLastTab(): Promise<void> {
  await router.isReady()
  const last = localStorage.getItem(LAST_TAB_KEY)
  if (rememberTabEnabled() && last && router.currentRoute.value.name === 'main') {
    await router.replace(last)
  }
}

export default router
