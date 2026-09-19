interface TelegramWebApp {
  initData: string
  colorScheme: 'light' | 'dark'
  ready(): void
  expand(): void
  onEvent(event: 'themeChanged', cb: () => void): void
  openLink?(url: string): void
  showConfirm?(message: string, cb: (ok: boolean) => void): void
  isVersionAtLeast?(version: string): boolean
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
  app.onEvent('themeChanged', () => {
    applyTheme()
    schemeListeners.forEach((fn) => fn())
  })
}

// Подписка на смену светлой/тёмной схемы: в Telegram — событие темы, в
// браузере и расширении — системный медиа-запрос. Нужна режиму «как в
// системе», где тема выбирается парой (светлая + тёмная).
const schemeListeners: (() => void)[] = []

export function onColorSchemeChange(fn: () => void): void {
  schemeListeners.push(fn)
  window.matchMedia('(prefers-color-scheme: light)').addEventListener('change', fn)
}

export function currentColorScheme(): 'light' | 'dark' {
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

/**
 * Подтверждение действия. В Telegram — через SDK: системный window.confirm в
 * его вебвью не показывается и молча возвращает false, из-за чего удаление
 * (темы, картинки, папки) выглядело как «кнопка не работает».
 */
export function confirmAction(message: string): Promise<boolean> {
  const app = tg()
  // showConfirm появился в Bot API 6.2. В старых клиентах метод существует, но
  // молча ничего не делает — колбэк не приходит, и действие «зависает»
  // (именно так «не удалялась» своя тема). Поэтому проверяем версию и на всякий
  // случай страхуемся от исключения.
  if (app?.showConfirm && app.isVersionAtLeast?.('6.2')) {
    return new Promise((resolve) => {
      try {
        app.showConfirm!(message, resolve)
      } catch {
        resolve(window.confirm(message))
      }
    })
  }
  return Promise.resolve(window.confirm(message))
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
