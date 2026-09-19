/**
 * Чтение текста из буфера обмена.
 *
 * Сначала стандартный Clipboard API (браузер, расширение), потом метод
 * Telegram WebApp: в вебвью Telegram `navigator.clipboard.readText` запрещён.
 * Метод Telegram тоже работает не во всех режимах запуска и может молча не
 * вызвать колбэк — поэтому таймаут. Пустая строка значит «прочитать не вышло»:
 * вызывающий предлагает вставить вручную (событие paste прав не требует).
 */
export async function readClipboardText(): Promise<string> {
  try {
    const text = (await navigator.clipboard.readText()).trim()
    if (text) return text
  } catch {
    // в Telegram-вебвью readText запрещён — идём дальше
  }
  return (await tgReadClipboard()).trim()
}

function tgReadClipboard(): Promise<string> {
  return new Promise((resolve) => {
    const tg = (window as { Telegram?: { WebApp?: { readTextFromClipboard?: (cb: (t: string | null) => void) => void } } })
      .Telegram?.WebApp
    if (!tg?.readTextFromClipboard) {
      resolve('')
      return
    }
    const timer = setTimeout(() => resolve(''), 1200)
    try {
      tg.readTextFromClipboard((t) => {
        clearTimeout(timer)
        resolve(t ?? '')
      })
    } catch {
      clearTimeout(timer)
      resolve('')
    }
  })
}
