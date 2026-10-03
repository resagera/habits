// Управление лабиринтом: круглый джойстик в правом нижнем углу и крестик
// кнопок для хода ровно на одну клетку.
//
// Раньше точка шла туда, куда зажали в любом месте поля. На телефоне это
// подвисало: касания по полю перехватывались и после пары быстрых нажатий
// точка «залипала». Джойстик ловит жест только на себе, а отпускание
// обрабатывается тремя событиями сразу — pointerup, pointercancel и
// pointerleave, чтобы не остаться с зажатым направлением.

export interface Stick {
  /** вектор от центра джойстика к пальцу (пусто — не держат) */
  vec: { x: number; y: number }
  active: boolean
}

export function emptyStick(): Stick {
  return { vec: { x: 0, y: 0 }, active: false }
}

/** Нормализованное смещение ручки джойстика для отрисовки (−1…1 по каждой оси). */
export function knobOffset(stick: Stick, radius: number): { x: number; y: number } {
  if (!stick.active) return { x: 0, y: 0 }
  const len = Math.hypot(stick.vec.x, stick.vec.y)
  if (len < 1) return { x: 0, y: 0 }
  const k = Math.min(1, len / radius)
  return { x: (stick.vec.x / len) * k, y: (stick.vec.y / len) * k }
}
