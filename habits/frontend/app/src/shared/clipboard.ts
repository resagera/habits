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

/**
 * Запись текста в буфер обмена.
 *
 * Clipboard API, а если он недоступен (старый вебвью, страница без secure
 * context) — фолбэк через скрытую textarea и execCommand('copy'): он требует
 * настоящего элемента в документе, поэтому копировать произвольный текст
 * (не значение поля, как в copyField.ts) иначе не выходит.
 * false значит «скопировать не вышло» — вызывающий сам показывает тост.
 */
export async function writeClipboardText(text: string): Promise<boolean> {
  if (!text) return false
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    // ниже фолбэк
  }
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    // вне экрана, но в документе: execCommand копирует только видимое дереву
    ta.style.cssText = 'position:fixed;top:-1000px;left:0;opacity:0'
    document.body.appendChild(ta)
    ta.select()
    ta.setSelectionRange(0, text.length) // iOS игнорирует select()
    const ok = document.execCommand('copy')
    ta.remove()
    return ok
  } catch {
    return false
  }
}
