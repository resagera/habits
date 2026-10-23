// Глобальная кнопка «копировать в буфер» у текстовых полей.
// Одна плавающая кнопка-иконка на всё приложение: появляется справа внутри
// сфокусированного input/textarea (если поле непустое), по нажатию копирует
// значение и показывает тост. Работает для всех страниц без правки компонентов;
// отключить для конкретного поля можно атрибутом data-no-copy.
import { showToast } from './toast'

type CopyTarget = HTMLInputElement | HTMLTextAreaElement

// Текстоподобные типы input; password не включаем — на странице Passwords
// свои кнопки копирования, а маскированное поле копировать неожиданно.
const TEXT_TYPES = new Set(['text', 'search', 'url', 'email', 'tel', 'number'])

const BTN_SIZE = 26

let btn: HTMLButtonElement | null = null
let target: CopyTarget | null = null

function isCopyable(el: EventTarget | null): el is CopyTarget {
  if (!(el instanceof HTMLElement) || el.dataset.noCopy !== undefined) return false
  if (el instanceof HTMLTextAreaElement) return !el.disabled
  if (el instanceof HTMLInputElement) return !el.disabled && TEXT_TYPES.has(el.type || 'text')
  return false
}

function place(): void {
  if (!btn || !target) return
  if (!target.value) {
    btn.style.display = 'none'
    return
  }
  const r = target.getBoundingClientRect()
  if (r.width < BTN_SIZE * 2.5 || r.bottom < 0 || r.top > window.innerHeight) {
    btn.style.display = 'none'
    return
  }
  const top =
    target instanceof HTMLTextAreaElement && r.height > 44
      ? r.top + 5
      : r.top + (r.height - BTN_SIZE) / 2
  btn.style.display = 'flex'
  btn.style.left = `${r.right - BTN_SIZE - 5}px`
  btn.style.top = `${top}px`
}

async function copyValue(): Promise<void> {
  if (!target) return
  const text = target.value
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    // фолбэк для webview без clipboard API: выделить поле и execCommand
    try {
      const el = target
      const s = el.selectionStart
      const e = el.selectionEnd
      el.select()
      document.execCommand('copy')
      if (s !== null && e !== null) el.setSelectionRange(s, e)
    } catch {
      showToast('Не удалось скопировать')
      return
    }
  }
  showToast('Скопировано')
}

function ensureButton(): HTMLButtonElement {
  if (btn) return btn
  btn = document.createElement('button')
  btn.type = 'button'
  btn.className = 'copy-field-btn'
  btn.setAttribute('aria-label', 'Скопировать в буфер')
  btn.innerHTML =
    '<svg width="14" height="14" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">' +
    '<path fill="currentColor" d="M16 1H4a2 2 0 0 0-2 2v13h2V3h12V1Zm3 4H8a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2Zm0 16H8V7h11v14Z"/>' +
    '</svg>'
  // pointerdown + preventDefault: не отдаём фокус из поля, копируем сразу
  btn.addEventListener('pointerdown', (e) => {
    e.preventDefault()
    void copyValue()
  })
  document.body.appendChild(btn)
  return btn
}

function hide(): void {
  if (btn) btn.style.display = 'none'
  target = null
}

export function installCopyField(): void {
  ensureButton()
  document.addEventListener('focusin', (e) => {
    if (isCopyable(e.target)) {
      target = e.target
      place()
    } else {
      hide()
    }
  })
  document.addEventListener('focusout', (e) => {
    if (e.target === target) hide()
  })
  document.addEventListener('input', (e) => {
    if (e.target === target) place()
  })
  // capture: ловим и прокрутку внутренних контейнеров (модалки и т.п.)
  document.addEventListener('scroll', place, { capture: true, passive: true })
  window.addEventListener('resize', place)
  window.visualViewport?.addEventListener('resize', place)
}
