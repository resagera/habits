import { createRouter, createWebHistory } from 'vue-router'

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    icon?: string
    /** показывать в меню и на плитках главной */
    app?: boolean
    /** страница только для администраторов (в меню и доступе) */
    admin?: boolean
    /**
     * Версия, в которой страница появилась или последний раз заметно менялась.
     * Видна админу в меню. Проставляется РУКАМИ при заметной правке страницы —
     * вывести её из истории нельзя: релиз обычно трогает несколько страниц.
     * Пусто у страниц старше журнала изменений — врать «1.0» ни к чему.
     */
    version?: string
    /**
     * Страница открывается без авторизации (публичная ссылка). Такие
     * страницы рисуются без меню и шапки и не дёргают закрытые ручки.
     */
    public?: boolean
  }
}

// Каждое мини-приложение — отдельный лениво загружаемый чанк.
const routes = [
  { path: '/', name: 'main', component: () => import('../apps/main/MainView.vue'), meta: { title: 'Habits', icon: '🏠', version: '2.80' } },
  { path: '/tracker', name: 'tracker', component: () => import('../apps/tracker/TrackerView.vue'), meta: { title: 'Tracker', icon: '📊', app: true, version: '2.81' } },
  { path: '/calendar', name: 'calendar', component: () => import('../apps/calendar/CalendarView.vue'), meta: { title: 'Calendar', icon: '📅', app: true, version: '2.81' } },
  { path: '/checker', name: 'checker', component: () => import('../apps/checker/CheckerView.vue'), meta: { title: 'Checker', icon: '✅', app: true, version: '2.83' } },
  { path: '/finance', name: 'finance', component: () => import('../apps/finance/FinanceView.vue'), meta: { title: 'Finance', icon: '💰', app: true, version: '2.73' } },
  { path: '/mail', name: 'mail', component: () => import('../apps/mail/MailView.vue'), meta: { title: 'Почта', icon: '📬', app: true, version: '2.74' } },
  { path: '/tests', name: 'tests', component: () => import('../apps/tests/TestsView.vue'), meta: { title: 'Тесты', icon: '🧠', app: true, version: '2.63' } },
  { path: '/tasks', name: 'tasks', component: () => import('../apps/tasks/TasksView.vue'), meta: { title: 'Tasks', icon: '🗂', app: true } },
  { path: '/diary', name: 'diary', component: () => import('../apps/diary/DiaryView.vue'), meta: { title: 'Diary', icon: '📔', app: true } },
  { path: '/metrics', name: 'metrics', component: () => import('../apps/metrics/MetricsView.vue'), meta: { title: 'Metrics', icon: '📈', app: true } },
  { path: '/passwords', name: 'passwords', component: () => import('../apps/passwords/PasswordsView.vue'), meta: { title: 'Passwords', icon: '🔑', app: true } },
  { path: '/reminders', name: 'reminders', component: () => import('../apps/reminders/RemindersView.vue'), meta: { title: 'Reminders', icon: '🔔', app: true } },
  { path: '/converter', name: 'converter', component: () => import('../apps/converter/ConverterView.vue'), meta: { title: 'Converter', icon: '💱', app: true, version: '2.78' } },
  { path: '/currency', redirect: '/converter' }, // старое имя вкладки Exchanges
  { path: '/links', name: 'links', component: () => import('../apps/links/LinksView.vue'), meta: { title: 'Links', icon: '🔗', app: true } },
  { path: '/articles', name: 'articles', component: () => import('../apps/articles/ArticlesView.vue'), meta: { title: 'Articles', icon: '📄', app: true } },
  { path: '/servers', name: 'servers', component: () => import('../apps/servers/ServersView.vue'), meta: { title: 'Servers', icon: '🖥', app: true, version: '2.13' } },
  { path: '/vault', name: 'vault', component: () => import('../apps/vault/VaultView.vue'), meta: { title: 'Сейф', icon: '🔐', app: true, version: '2.85' } },
  { path: '/files', name: 'files', component: () => import('../apps/files/FilesView.vue'), meta: { title: 'My Files', icon: '📁', app: true, version: '2.18' } },
  { path: '/terminal', name: 'terminal', component: () => import('../apps/terminal/TerminalView.vue'), meta: { title: 'Terminal', icon: '⌨️', app: true, version: '2.20' } },
  { path: '/tv', name: 'tv', component: () => import('../apps/tv/RemoteView.vue'), meta: { title: 'Пульт ТВ', icon: '📺', app: true, version: '2.76' } },
  { path: '/contacts', name: 'contacts', component: () => import('../apps/contacts/ContactsView.vue'), meta: { title: 'Contacts', icon: '👥', app: true, version: '2.24' } },
  { path: '/projects', name: 'projects', component: () => import('../apps/projects/ProjectsView.vue'), meta: { title: 'Projects', icon: '📦', app: true, version: '2.26' } },
  { path: '/food', name: 'food', component: () => import('../apps/food/FoodView.vue'), meta: { title: 'Food', icon: '🍽', app: true, version: '2.55' } },
  { path: '/automation', name: 'automation', component: () => import('../apps/automation/AutomationView.vue'), meta: { title: 'Автоматизация', icon: '🤖', app: true, version: '2.29' } },
  { path: '/ai', name: 'ai', component: () => import('../apps/ai/AIView.vue'), meta: { title: 'AI', icon: '✨', app: true, version: '2.48' } },
  { path: '/help', name: 'help', component: () => import('../apps/help/HelpView.vue'), meta: { title: 'Help', icon: '🆘', app: true } },
  { path: '/settings', name: 'settings', component: () => import('../apps/settings/SettingsView.vue'), meta: { title: 'Settings', icon: '⚙️', app: true, version: '2.80' } },
  { path: '/admin', name: 'admin', component: () => import('../apps/admin/AdminView.vue'), meta: { title: 'Админка', icon: '🛠', admin: true, version: '2.45' } },
  { path: '/releases', name: 'releases', component: () => import('../apps/releases/ReleasesView.vue'), meta: { title: 'Релизы', icon: '🚀', admin: true, version: '2.47' } },
  // Публичное чтение статьи по ссылке (работает в браузере без Telegram)
  { path: '/read/:token([0-9a-f]{24})', name: 'read', component: () => import('../apps/articles/PublicReadView.vue'), meta: { title: 'Habits', public: true } },
  // Временная ссылка на файл сейфа: пароль спрашивается здесь же
  { path: '/l/:token([A-Za-z0-9_-]{43})', name: 'vault-link', component: () => import('../apps/vault/LinkView.vue'), meta: { title: 'Файл из сейфа', public: true } },
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
  if (rememberTabEnabled() && to.name !== 'main' && !to.meta.public) {
    localStorage.setItem(LAST_TAB_KEY, to.fullPath)
  }
})

// Страницы с персональным доступом: недоступные уводят на главную.
router.beforeEach(async (to) => {
  if (to.meta.public) return true // публичная страница: доступы не спрашиваем
  // «Админка» — только для администраторов.
  if (to.meta.admin) {
    const { me, isAdmin, loadMe } = await import('../shared/me')
    if (!me.value) await loadMe()
    if (!isAdmin.value) return { path: '/' }
  }
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
