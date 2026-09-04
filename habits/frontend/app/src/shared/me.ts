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

export async function loadMe(): Promise<void> {
  try {
    me.value = await api.get<Me>('/me')
  } catch {
    // вне Telegram / нет сети — остаёмся без данных пользователя
  }
}
