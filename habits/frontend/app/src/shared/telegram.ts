interface TelegramWebApp {
  initData: string
  colorScheme: 'light' | 'dark'
  ready(): void
  expand(): void
  onEvent(event: 'themeChanged', cb: () => void): void
  openLink?(url: string): void
}

declare global {
  interface Window {
    Telegram?: { WebApp: TelegramWebApp }
  }
}

export function tg(): TelegramWebApp | undefined {
  return window.Telegram?.WebApp
}

export function getInitData(): string {
  return tg()?.initData ?? ''
}

export function initTelegram(): void {
  const app = tg()
  if (!app) return
  app.ready()
  app.expand()
  app.onEvent('themeChanged', applyTheme)
}

function currentColorScheme(): 'light' | 'dark' {
  const scheme = tg()?.colorScheme
  if (scheme) return scheme
  return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
}

export type ThemePreference = 'auto' | 'light' | 'dark'

const THEME_KEY = 'theme_preference'

export function getThemePreference(): ThemePreference {
  const v = localStorage.getItem(THEME_KEY)
  return v === 'light' || v === 'dark' ? v : 'auto'
}

export function setThemePreference(pref: ThemePreference): void {
  localStorage.setItem(THEME_KEY, pref)
  applyTheme()
}

export function applyTheme(): void {
  const pref = getThemePreference()
  document.documentElement.dataset.theme = pref === 'auto' ? currentColorScheme() : pref
}

/** Открыть внешнюю ссылку: внутри Telegram — через SDK, иначе новой вкладкой. */
export function openExternalLink(url: string): void {
  const app = tg()
  if (app?.openLink) {
    app.openLink(url)
  } else {
    window.open(url, '_blank', 'noopener')
  }
}
