// Реестр индивидуальных настроек страниц. Ключ — имя роута; значение —
// лениво загружаемый компонент с настройками этой страницы. Страницы, которых
// здесь нет, показывают заглушку «настроек пока нет» (см. PageSettingsModal).
import type { Component } from 'vue'

export const pageSettings: Record<string, () => Promise<{ default: Component }>> = {
  diary: () => import('../apps/diary/DiarySettings.vue'),
  links: () => import('../apps/links/LinksSettings.vue'),
  passwords: () => import('../apps/passwords/PasswordsSettings.vue'),
  tests: () => import('../apps/tests/TestsSettings.vue'),
  finance: () => import('../apps/finance/FinanceSettings.vue'),
}

/** Есть ли у страницы (по имени роута) индивидуальные настройки. */
export function hasPageSettings(name: string | null | undefined): boolean {
  return !!name && name in pageSettings
}
