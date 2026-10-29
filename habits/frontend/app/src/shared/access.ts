// Доступы к страницам и опциям: /me/pages отдаёт карту видимости для
// текущего пользователя (админ видит всё). До загрузки считаем, что
// доступно всё, чтобы не мигать интерфейсом; после — фильтруем.
import { ref } from 'vue'
import { api } from './api/client'

export const accessLoaded = ref(false)
export const allowedPages = ref<Record<string, boolean>>({})
export const grantedFeatures = ref<Record<string, boolean>>({})

export async function loadAccess(): Promise<void> {
  try {
    const data = await api.get<{ pages: Record<string, boolean>; features: Record<string, boolean> }>('/me/pages')
    allowedPages.value = data.pages
    grantedFeatures.value = data.features
    accessLoaded.value = true
  } catch {
    // не смогли загрузить (оффлайн/ошибка) — не прячем ничего
  }
}

/** Доступна ли страница (код = имя роута). Неуправляемые страницы — всегда. */
export function pageAllowed(code: string): boolean {
  if (!accessLoaded.value) return true
  return allowedPages.value[code] !== false
}

export function featureGranted(code: string): boolean {
  return grantedFeatures.value[code] === true
}
