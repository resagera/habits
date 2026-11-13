// Свайпы для игр: одна точка касания, порог в пикселях, направление по
// большей оси. Вынесено из игр — жест нужен и в 2048, и в пятнашках.
export type SwipeDir = 'up' | 'down' | 'left' | 'right'

export function onSwipe(el: HTMLElement, cb: (dir: SwipeDir) => void, min = 24): () => void {
  let x = 0
  let y = 0

  const start = (e: TouchEvent) => {
    x = e.changedTouches[0].clientX
    y = e.changedTouches[0].clientY
  }
  const end = (e: TouchEvent) => {
    const dx = e.changedTouches[0].clientX - x
    const dy = e.changedTouches[0].clientY - y
    if (Math.max(Math.abs(dx), Math.abs(dy)) < min) return
    cb(Math.abs(dx) > Math.abs(dy) ? (dx > 0 ? 'right' : 'left') : dy > 0 ? 'down' : 'up')
  }

  el.addEventListener('touchstart', start, { passive: true })
  el.addEventListener('touchend', end, { passive: true })
  return () => {
    el.removeEventListener('touchstart', start)
    el.removeEventListener('touchend', end)
  }
}
