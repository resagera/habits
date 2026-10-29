// Генератор таблиц Ишихары.
//
// Картинок в проекте нет: оригинальные таблицы — чужая интеллектуальная
// собственность, да и фиксированный набор заучивается наизусть. Поэтому
// рисуем свои по тем же правилам: круги разного размера без зазоров, цифра
// набрана цветом с линии спутывания, светлота у фигуры и фона одинаковая,
// плюс лёгкий шум яркости — иначе фигуру видно по пятну.
//
// Мы знаем про каждый кружок, фигура это или фон, поэтому перекрасить таблицу
// любым способом (коррекция, симуляция, серый, подсветка ответа) — это просто
// другая функция цвета при отрисовке, а не обработка картинки.
import {
  boost, clamp, confusionPair, css, daltonize, distance, simulate, toGray,
  type CvdType, type Rgb,
} from './colors'

export type PlateKind = CvdType | 'demo'

export interface Dot {
  x: number
  y: number
  r: number
  figure: boolean
  /** собственный оттенок кружка: шум яркости поверх базового цвета */
  jitter: number
}

export interface Plate {
  /** что набрано цифрами: ответ */
  answer: string
  kind: PlateKind
  dots: Dot[]
  size: number
  bg: Rgb
  fg: Rgb
}

export type PaletteMode = 'normal' | 'boost' | 'fix' | 'gray' | 'sim' | 'reveal'

export const PALETTES: { code: PaletteMode; title: string; hint: string }[] = [
  { code: 'normal', title: 'Как есть', hint: 'исходные цвета таблицы' },
  { code: 'boost', title: 'Усилить', hint: 'разница между цветами увеличена' },
  { code: 'fix', title: 'Коррекция', hint: 'потерянный оттенок перенесён в различимые цвета' },
  { code: 'gray', title: 'Светлота', hint: 'только яркость: фигуры не видно — так и задумано' },
  { code: 'sim', title: 'Как видит дальтоник', hint: 'симуляция этого типа' },
  { code: 'reveal', title: 'Показать ответ', hint: 'кружки фигуры выделены' },
]

/**
 * Базовые цвета. Приглушённые и средней светлоты намеренно: из насыщенных
 * пара на линии спутывания вылезает за пределы экрана, обрезается — и
 * дальтоник начинает видеть фигуру, то есть таблица перестаёт работать.
 */
const BASE: Record<PlateKind, Rgb> = {
  protan: { r: 168, g: 148, b: 118 },
  deutan: { r: 168, g: 148, b: 118 },
  tritan: { r: 150, g: 152, b: 140 },
  demo: { r: 190, g: 150, b: 120 },
}

/**
 * Цифры таблицы. Берём двузначные: односложную «1» можно угадать по случайному
 * пятну, а две цифры случайно не складываются.
 */
function randomAnswer(): string {
  const a = 1 + Math.floor(Math.random() * 9)
  const b = Math.floor(Math.random() * 10)
  return `${a}${b}`
}

/** Маска фигуры: цифры, набранные в середине круга. */
function figureMask(size: number, text: string): Uint8Array {
  const c = document.createElement('canvas')
  c.width = size
  c.height = size
  const g = c.getContext('2d')!
  g.fillStyle = '#000'
  g.fillRect(0, 0, size, size)
  g.fillStyle = '#fff'
  g.strokeStyle = '#fff'
  g.textAlign = 'center'
  g.textBaseline = 'middle'
  g.font = `bold ${Math.round(size * 0.6)}px Inter, Arial, sans-serif`
  // цифру ещё и обводим: кружок попадает в фигуру по своему центру, и тонкие
  // штрихи иначе рассыпаются на отдельные точки
  g.lineWidth = size * 0.055
  g.lineJoin = 'round'
  g.strokeText(text, size / 2, size / 2 + size * 0.02)
  g.fillText(text, size / 2, size / 2 + size * 0.02)
  const data = g.getImageData(0, 0, size, size).data
  const mask = new Uint8Array(size * size)
  for (let i = 0; i < mask.length; i++) mask[i] = data[i * 4] > 128 ? 1 : 0
  return mask
}

/**
 * Укладка кругов. Радиус каждой попытки случайный со смещением в сторону
 * мелких (квадрат равномерного) — так крупные не занимают всё поле первыми, а
 * мелкие заполняют промежутки, и получается характерная «россыпь» таблицы.
 * Пересечения отбрасываем: кружки не должны сливаться в пятна, иначе фигуру
 * видно по контуру, а не по цвету.
 *
 * Соседей ищем по сетке, а не перебором всех: при восьми сотнях кружков
 * полный перебор на каждую из десяти тысяч попыток заметно тормозит телефон.
 */
function packDots(size: number, mask: Uint8Array): Dot[] {
  const dots: Dot[] = []
  const center = size / 2
  const radius = center - 2
  // кружки мельче, чем кажется нужным: на крупных штрихи цифры рассыпаются
  const rMin = Math.max(1.8, size / 170)
  const rMax = Math.max(rMin + 1.4, size / 54)

  const cell = rMax * 2 + 2
  const cols = Math.ceil(size / cell)
  const grid = new Map<number, Dot[]>()

  const at = (px: number, py: number) => {
    const ix = Math.min(size - 1, Math.max(0, Math.round(px)))
    const iy = Math.min(size - 1, Math.max(0, Math.round(py)))
    return mask[iy * size + ix] === 1
  }
  /** Кружок целиком внутри цифры: ни один край не вылезает за штрих. */
  const insideFigure = (x: number, y: number, r: number) =>
    at(x, y) && at(x - r, y) && at(x + r, y) && at(x, y - r) && at(x, y + r)

  const free = (x: number, y: number, r: number) => {
    const cx = Math.floor(x / cell)
    const cy = Math.floor(y / cell)
    for (let gy = cy - 1; gy <= cy + 1; gy++) {
      for (let gx = cx - 1; gx <= cx + 1; gx++) {
        for (const o of grid.get(gy * cols + gx) ?? []) {
          if ((o.x - x) ** 2 + (o.y - y) ** 2 < (o.r + r + 1.1) ** 2) return false
        }
      }
    }
    return true
  }

  const place = (x: number, y: number, r: number, figure: boolean) => {
    const dot: Dot = { x, y, r, figure, jitter: 0.78 + Math.random() * 0.42 }
    dots.push(dot)
    const key = Math.floor(y / cell) * cols + Math.floor(x / cell)
    const bucket = grid.get(key)
    if (bucket) bucket.push(dot)
    else grid.set(key, [dot])
  }

  // Сначала заполняем саму цифру, и только потом фон вокруг. В обратном
  // порядке крупные фоновые кружки залезали на штрихи, цифра выходила рваной
  // и читалась через раз.
  const tries = Math.round(size * 110)
  for (let i = 0; i < tries; i++) {
    const r = rMin + (rMax - rMin) * Math.random() ** 2
    const a = Math.random() * Math.PI * 2
    const d = Math.sqrt(Math.random()) * (radius - r)
    const x = center + Math.cos(a) * d
    const y = center + Math.sin(a) * d
    if (!insideFigure(x, y, r) || !free(x, y, r)) continue
    place(x, y, r, true)
  }
  for (let i = 0; i < tries; i++) {
    const r = rMin + (rMax - rMin) * Math.random() ** 2
    const a = Math.random() * Math.PI * 2
    const d = Math.sqrt(Math.random()) * (radius - r)
    const x = center + Math.cos(a) * d
    const y = center + Math.sin(a) * d
    if (at(x, y) || !free(x, y, r)) continue
    place(x, y, r, false)
  }
  return dots
}

/**
 * Готовая таблица. Для demo-таблицы фигура отличается и светлотой — её видят
 * все, и по ней понятно, что человек вообще понял задание.
 */
export function makePlate(kind: PlateKind, size = 320): Plate {
  const answer = randomAnswer()
  const mask = figureMask(size, answer)
  const dots = packDots(size, mask)
  const base = BASE[kind]

  if (kind === 'demo') {
    return {
      answer, kind, dots, size,
      bg: { r: 170, g: 170, b: 170 },
      fg: { r: 200, g: 80, b: 70 },
    }
  }
  const [bg, fg] = confusionPair(base, kind)
  return { answer, kind, dots, size, bg, fg }
}

/** Цвет кружка в выбранном режиме палитры. */
function dotColor(plate: Plate, dot: Dot, mode: PaletteMode): Rgb {
  const base = dot.figure ? plate.fg : plate.bg
  // шум яркости: именно он не даёт собрать фигуру по светлоте
  const jittered = clamp({
    r: base.r * dot.jitter, g: base.g * dot.jitter, b: base.b * dot.jitter,
  })
  const type: CvdType = plate.kind === 'demo' ? 'deutan' : plate.kind
  switch (mode) {
    case 'boost': {
      const mid = {
        r: (plate.bg.r + plate.fg.r) / 2,
        g: (plate.bg.g + plate.fg.g) / 2,
        b: (plate.bg.b + plate.fg.b) / 2,
      }
      return boost(jittered, mid)
    }
    case 'fix':
      return daltonize(jittered, type)
    case 'gray':
      return toGray(jittered)
    case 'sim':
      return simulate(jittered, type)
    case 'reveal':
      return dot.figure ? { r: 30, g: 30, b: 34 } : jittered
    default:
      return jittered
  }
}

export function drawPlate(
  canvas: HTMLCanvasElement, plate: Plate, mode: PaletteMode,
): void {
  const dpr = Math.min(window.devicePixelRatio || 1, 2)
  canvas.width = Math.round(plate.size * dpr)
  canvas.height = Math.round(plate.size * dpr)
  canvas.style.width = `${plate.size}px`
  canvas.style.height = `${plate.size}px`
  const g = canvas.getContext('2d')
  if (!g) return
  g.setTransform(dpr, 0, 0, dpr, 0, 0)
  g.clearRect(0, 0, plate.size, plate.size)

  // подложка круга — чуть темнее фона, чтобы между кружками не светился лист
  g.fillStyle = mode === 'gray' ? '#9a9a9a' : css({
    r: plate.bg.r * 0.55, g: plate.bg.g * 0.55, b: plate.bg.b * 0.55,
  })
  g.beginPath()
  g.arc(plate.size / 2, plate.size / 2, plate.size / 2 - 1, 0, Math.PI * 2)
  g.fill()

  for (const dot of plate.dots) {
    g.fillStyle = css(dotColor(plate, dot, mode))
    g.beginPath()
    g.arc(dot.x, dot.y, dot.r, 0, Math.PI * 2)
    g.fill()
  }
}

/**
 * Насколько пара цветов таблицы «правильная»: для нормального зрения разница
 * должна быть заметной, для своего типа дальтонизма — почти нулевой. Нужна
 * проверке, а не игроку.
 */
export function plateContrast(plate: Plate): { normal: number; blind: number } {
  const type: CvdType = plate.kind === 'demo' ? 'deutan' : plate.kind
  return {
    normal: distance(plate.bg, plate.fg),
    blind: distance(simulate(plate.bg, type), simulate(plate.fg, type)),
  }
}
