// Перестановка групп-соседей перетаскиванием за заголовок (pointer-события —
// работает и мышью, и на тач). Живая перестановка идёт через мутацию `position`
// у объектов групп (соседние карточки пересортировываются вычислимым свойством),
// на отпускание порядок сохраняется на сервере.
import { ref } from 'vue'
import * as checkerApi from './api'
import { showToast } from '../../shared/toast'
import type { CheckGroup } from './types'

export function useGroupReorder(
  getGroup: () => CheckGroup,
  getAll: () => CheckGroup[],
  getRootEl: () => HTMLElement | null,
) {
  const dragging = ref(false)
  const suppressClick = ref(false)
  let started = false
  let startY = 0
  let container: HTMLElement | null = null

  // соседи — группы того же родителя, отсортированные по position
  const siblings = () =>
    getAll()
      .filter((g) => g.parent_id === getGroup().parent_id)
      .sort((a, b) => a.position - b.position || a.id - b.id)

  function onPointerDown(e: PointerEvent) {
    if (e.button && e.button !== 0) return
    // одиночная группа переставлять некуда
    if (siblings().length < 2) return
    started = false
    startY = e.clientY
    container = getRootEl()?.parentElement ?? null
    if (!container) return

    const move = (ev: PointerEvent) => {
      if (!started) {
        if (Math.abs(ev.clientY - startY) < 8) return
        started = true
        dragging.value = true
      }
      reorderToPointer(ev.clientY)
    }
    const up = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      window.removeEventListener('pointercancel', up)
      if (started) {
        suppressClick.value = true // не переключать сворачивание после перетаскивания
        checkerApi
          .reorderGroups(getGroup().parent_id, siblings().map((g) => g.id))
          .catch(() => showToast('Не удалось переставить'))
      }
      dragging.value = false
      started = false
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
    window.addEventListener('pointercancel', up)
  }

  function reorderToPointer(y: number) {
    if (!container) return
    const cards = Array.from(container.children).filter((c) =>
      (c as HTMLElement).classList.contains('check-group'),
    ) as HTMLElement[]
    const sibs = siblings()
    if (cards.length !== sibs.length) return // фильтр поиска и т.п. — не переставляем
    let target = sibs.length - 1
    for (let i = 0; i < cards.length; i++) {
      const r = cards[i].getBoundingClientRect()
      if (y < r.top + r.height / 2) {
        target = i
        break
      }
    }
    const cur = sibs.findIndex((g) => g.id === getGroup().id)
    if (cur === -1 || cur === target) return
    const arr = sibs.slice()
    const [moved] = arr.splice(cur, 1)
    arr.splice(target, 0, moved)
    arr.forEach((g, idx) => (g.position = idx))
  }

  // вызвать из @click заголовка: true — клик подавлён (был drag)
  function consumeClick(): boolean {
    if (suppressClick.value) {
      suppressClick.value = false
      return true
    }
    return false
  }

  return { dragging, onPointerDown, consumeClick }
}
