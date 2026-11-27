// Способ входа. В Telegram — initData (как было). Вне Telegram (браузер,
// расширение) — персональный токен доступа из Настроек, он же включает
// «токен-режим»: админка и оформление скрыты, часть API закрыта сервером.
import { computed, ref } from 'vue'
import { getInitData } from './telegram'

const TOKEN_KEY = 'access_token'

export type AuthMode = 'tma' | 'token' | 'none'

/** Токен из локального хранилища (пусто — не задан). */
export function getToken(): string {
  try {
    return localStorage.getItem(TOKEN_KEY) ?? ''
  } catch {
    return ''
  }
}

const token = ref(getToken())

export function setToken(value: string): void {
  token.value = value.trim()
  try {
    localStorage.setItem(TOKEN_KEY, token.value)
  } catch {
    /* приватный режим — токен проживёт до перезагрузки */
  }
}

export function clearToken(): void {
  token.value = ''
  try {
    localStorage.removeItem(TOKEN_KEY)
  } catch {
    /* ignore */
  }
}

/** Режим входа: считается на лету, чтобы ввод токена сразу открывал приложение. */
export const authMode = computed<AuthMode>(() => {
  if (getInitData()) return 'tma'
  if (token.value) return 'token'
  // npm run dev вне Telegram: бэкенд с DEV_AUTH_BYPASS принимает 'tma dev',
  // поэтому экран ввода токена не показываем (но введённый токен победит выше)
  if (import.meta.env.DEV) return 'tma'
  return 'none'
})

/** Вход по токену — прячем админку, оформление и выпуск токенов. */
export const isTokenMode = computed(() => authMode.value === 'token')

/** Нет ни initData, ни токена — показываем экран ввода токена. */
export const needsToken = computed(() => authMode.value === 'none')

/** Токен отвергнут сервером (401): сбрасываем и просим ввести заново. */
export function onTokenRejected(): void {
  if (authMode.value === 'token') clearToken()
}
