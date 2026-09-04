/**
 * Textarea, которая растёт под текст.
 *
 * Сначала обязательно `height: auto`: без сброса `scrollHeight` не умеет
 * уменьшаться, и поле, один раз выросшее, назад уже не сжимается.
 *
 * Верхняя граница нужна в модалках: без неё длинная заметка выталкивает
 * кнопки «Сохранить» и «Удалить» за экран. Дальше поле прокручивается внутри.
 */
export function autoGrow(el: HTMLTextAreaElement | null | undefined, max = 320): void {
  if (!el) return
  el.style.height = 'auto'
  const h = Math.min(el.scrollHeight, max)
  el.style.height = h + 'px'
  el.style.overflowY = el.scrollHeight > max ? 'auto' : 'hidden'
}
