// Данные текущего пользователя (/me). Загружаются один раз при старте и
// переиспользуются: меню и страница «Админка» показываются только админу,
// а Settings/страницы не дёргают /me повторно.
import { computed, ref } from 'vue'
import { api } from './api/client'

export interface Me {
  id: number
  username?: string
  first_name?: string
  is_admin?: boolean
}

export const me = ref<Me | null>(null)
export const isAdmin = computed(() => me.value?.is_admin === true)

/**
 * Одна неудачная попытка на старте (сеть моргнула, сервер перезапускался)
 * оставляла приложение без данных пользователя до перезагрузки страницы: у
 * админа из меню пропадали «Админка» и «Релизы». Поэтому повтор через 3 с.
 */
export async function loadMe(retry = true): Promise<void> {
  try {
    me.value = await api.get<Me>('/me')
  } catch {
    if (retry) setTimeout(() => void loadMe(false), 3000)
    // вне Telegram / нет сети — остаёмся без данных пользователя
  }
}
