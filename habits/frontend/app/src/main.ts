import { createApp } from 'vue'
import App from './App.vue'
import router, { restoreLastTab } from './router'
import { redeemStartParam } from './apps/checker/redeem'
import { loadAccess } from './shared/access'
import { applyCachedBackground, loadBackground } from './shared/background'
import { initTelegram, applyTheme } from './shared/telegram'
import './shared/theme/theme.css'

initTelegram()
applyTheme()

createApp(App).use(router).mount('#app')
restoreLastTab()
applyCachedBackground()
loadBackground()
loadAccess()
redeemStartParam()
