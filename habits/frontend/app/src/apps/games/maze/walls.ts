// Стили стенок лабиринта. Каждый — маленький холст-плитка, который canvas
// размножает по всей стенке (createPattern): одна заливка на все стены вместо
// рисования каждого кирпичика, и никаких картинок грузить не нужно.
//
// Стиль выбирается случайно при запуске партии. Позже сюда добавится набор
// текстур из админки — тогда рядом появится вариант «картинка».

export interface WallStyle {
  code: string
  title: string
  /** рисует одну плитку размером size×size */
  draw(c: CanvasRenderingContext2D, size: number): void
}

export const WALL_STYLES: WallStyle[] = [
  {
    code: 'brick',
    title: 'Кирпич',
    draw(c, s) {
      c.fillStyle = '#b4652a'
      c.fillRect(0, 0, s, s)
      c.strokeStyle = 'rgba(0, 0, 0, 0.35)'
      c.lineWidth = 2
      const h = s / 2
      for (let y = 0; y <= s; y += h) {
        c.beginPath()
        c.moveTo(0, y)
        c.lineTo(s, y)
        c.stroke()
      }
      // перевязка: вертикальные швы в соседних рядах смещены
      c.beginPath()
      c.moveTo(s / 2, 0)
      c.lineTo(s / 2, h)
      c.moveTo(0, h)
      c.lineTo(0, s)
      c.moveTo(s, h)
      c.lineTo(s, s)
      c.stroke()
      c.fillStyle = 'rgba(255, 255, 255, 0.12)'
      c.fillRect(0, 2, s, 2)
    },
  },
  {
    code: 'stone',
    title: 'Камень',
    draw(c, s) {
      c.fillStyle = '#6b7280'
      c.fillRect(0, 0, s, s)
      c.fillStyle = 'rgba(255, 255, 255, 0.12)'
      for (let i = 0; i < 14; i++) {
        const x = Math.random() * s
        const y = Math.random() * s
        c.fillRect(x, y, 3 + Math.random() * 5, 2 + Math.random() * 4)
      }
      c.fillStyle = 'rgba(0, 0, 0, 0.22)'
      for (let i = 0; i < 10; i++) {
        c.fillRect(Math.random() * s, Math.random() * s, 2 + Math.random() * 4, 2)
      }
    },
  },
  {
    code: 'wood',
    title: 'Дерево',
    draw(c, s) {
      c.fillStyle = '#8b5a2b'
      c.fillRect(0, 0, s, s)
      c.strokeStyle = 'rgba(0, 0, 0, 0.25)'
      c.lineWidth = 1.5
      for (let y = 3; y < s; y += 6) {
        c.beginPath()
        c.moveTo(0, y)
        c.bezierCurveTo(s / 3, y + 2, (2 * s) / 3, y - 2, s, y)
        c.stroke()
      }
    },
  },
  {
    code: 'neon',
    title: 'Неон',
    draw(c, s) {
      c.fillStyle = '#111827'
      c.fillRect(0, 0, s, s)
      c.strokeStyle = '#22d3ee'
      c.lineWidth = 2
      c.strokeRect(2, 2, s - 4, s - 4)
      c.fillStyle = 'rgba(34, 211, 238, 0.18)'
      c.fillRect(4, 4, s - 8, s - 8)
    },
  },
  {
    code: 'hedge',
    title: 'Живая изгородь',
    draw(c, s) {
      c.fillStyle = '#2f6b3a'
      c.fillRect(0, 0, s, s)
      for (let i = 0; i < 18; i++) {
        c.fillStyle = Math.random() > 0.5 ? 'rgba(255,255,255,0.10)' : 'rgba(0,0,0,0.18)'
        const x = Math.random() * s
        const y = Math.random() * s
        c.beginPath()
        c.arc(x, y, 2 + Math.random() * 3, 0, Math.PI * 2)
        c.fill()
      }
    },
  },
  {
    code: 'ice',
    title: 'Лёд',
    draw(c, s) {
      c.fillStyle = '#7dd3fc'
      c.fillRect(0, 0, s, s)
      c.strokeStyle = 'rgba(255, 255, 255, 0.7)'
      c.lineWidth = 1.5
      for (let i = 0; i < 4; i++) {
        c.beginPath()
        c.moveTo(Math.random() * s, 0)
        c.lineTo(Math.random() * s, s)
        c.stroke()
      }
      c.fillStyle = 'rgba(255, 255, 255, 0.25)'
      c.fillRect(0, 0, s, 3)
    },
  },
]

/**
 * Паттерн из загруженной админом картинки. Плитка размножается по стене, так
 * что картинка должна быть бесшовной — проверить это может только человек.
 */
export function imagePattern(
  ctx: CanvasRenderingContext2D, url: string,
): Promise<CanvasPattern | null> {
  return new Promise((resolve) => {
    const img = new Image()
    img.onload = () => resolve(ctx.createPattern(img, 'repeat'))
    img.onerror = () => resolve(null)
    img.src = url
  })
}

/** Готовый паттерн для заливки стен. */
export function wallPattern(ctx: CanvasRenderingContext2D, code: string): CanvasPattern | null {
  const style = WALL_STYLES.find((s) => s.code === code) ?? WALL_STYLES[0]
  const size = 24
  const tile = document.createElement('canvas')
  tile.width = size
  tile.height = size
  const tc = tile.getContext('2d')
  if (!tc) return null
  style.draw(tc, size)
  return ctx.createPattern(tile, 'repeat')
}
