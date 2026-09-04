import { createApp } from 'vue'
import App from './App.vue'
import router, { restoreLastTab } from './router'
import { redeemStartParam } from './apps/checker/redeem'
import { loadAccess } from './shared/access'
import { loadPageOrder } from './shared/pageOrder'
import { loadMe } from './shared/me'
import { applyCachedAppearance, loadAppearance } from './shared/appearance'
import { loadPinnedHeaders } from './shared/pinnedHeader'
import { applyCachedBackground, loadBackground } from './shared/background'
import { initTelegram } from './shared/telegram'
import { installCopyField } from './shared/copyField'
import './shared/theme/theme.css'

initTelegram()
applyCachedAppearance()
installCopyField()

createApp(App).use(router).mount('#app')

// Публичные страницы (ссылка на статью, ссылка на файл сейфа) открываются без
// авторизации: закрытые ручки там дали бы 401 и ничего не добавили. Путь
// сверяем без начала: в проде приложение живёт под /app/habits/.
if (!/\/(read|l)\/[A-Za-z0-9_-]+$/.test(location.pathname)) {
  restoreLastTab()
  applyCachedBackground()
  loadBackground()
  loadAccess()
  loadPageOrder()
  loadMe()
  loadAppearance()
  loadPinnedHeaders()
  redeemStartParam()
}
