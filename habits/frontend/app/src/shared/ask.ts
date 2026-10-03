// Ввод строки своим окном вместо window.prompt().
//
// Нативный prompt в Telegram выглядит инородно (а в некоторых клиентах его
// вовсе нет и он молча возвращает null — так «не создавались» папки фонов).
// Состояние живёт здесь, окно рисует AskModal в App.vue — как с тостами.
import { ref } from 'vue'

export interface AskRequest {
  title: string
  value: string
  placeholder: string
  okLabel: string
}

export const askState = ref<AskRequest | null>(null)

let resolver: ((value: string | null) => void) | null = null

/** Спросить строку. Пустой ответ и отмена — null, строка приходит обрезанной. */
export function askText(opts: {
  title: string
  value?: string
  placeholder?: string
  okLabel?: string
}): Promise<string | null> {
  askState.value = {
    title: opts.title,
    value: opts.value ?? '',
    placeholder: opts.placeholder ?? '',
    okLabel: opts.okLabel ?? 'Готово',
  }
  // Предыдущий вопрос, если он почему-то остался открытым, закрываем ответом
  // null: висящий промис хуже отменённого действия.
  resolver?.(null)
  return new Promise((resolve) => {
    resolver = resolve
  })
}

export function answerAsk(value: string | null): void {
  const trimmed = value?.trim() ?? ''
  askState.value = null
  const done = resolver
  resolver = null
  done?.(trimmed ? trimmed : null)
}
