import { ref } from 'vue'

export const toastMessage = ref('')
let timer: ReturnType<typeof setTimeout> | undefined

export function showToast(message: string): void {
  toastMessage.value = message
  clearTimeout(timer)
  timer = setTimeout(() => (toastMessage.value = ''), 2500)
}
