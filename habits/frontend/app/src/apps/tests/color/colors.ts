// Цветовая математика теста на цветовосприятие.
//
// Таблицы Ишихары держатся на «линиях спутывания»: два цвета, которые для
// человека с нормальным зрением разные, а для дальтоника сливаются. В
// пространстве LMS (отклик трёх типов колбочек) такая пара строится просто —
// меняем ровно тот канал, который у этого типа дальтонизма не работает.
//
// Матрицы — те же, что в классическом daltonize: перевод RGB↔LMS и проекции
// Вьено для симуляции дихромазии. Считаем по гамма-кодированным значениям,
// как делают все расхожие реализации: для экранного теста этого достаточно.

export type CvdType = 'protan' | 'deutan' | 'tritan'

export interface Rgb {
  r: number
  g: number
  b: number
}

const RGB2LMS = [
  [17.8824, 43.5161, 4.11935],
  [3.45565, 27.1554, 3.86714],
  [0.0299566, 0.184309, 1.46709],
]

const LMS2RGB = [
  [0.0809444479, -0.130504409, 0.116721066],
  [-0.0102485335, 0.0540193266, -0.113614708],
  [-0.000365296938, -0.00412161469, 0.693511405],
]

type Lms = [number, number, number]

export function toLms({ r, g, b }: Rgb): Lms {
  return [
    RGB2LMS[0][0] * r + RGB2LMS[0][1] * g + RGB2LMS[0][2] * b,
    RGB2LMS[1][0] * r + RGB2LMS[1][1] * g + RGB2LMS[1][2] * b,
    RGB2LMS[2][0] * r + RGB2LMS[2][1] * g + RGB2LMS[2][2] * b,
  ]
}

export function fromLms(lms: Lms): Rgb {
  return clamp(fromLmsRaw(lms))
}

/** Без обрезки по диапазону: нужно, чтобы понять, вылез ли цвет за пределы экрана. */
function fromLmsRaw(lms: Lms): Rgb {
  return {
    r: LMS2RGB[0][0] * lms[0] + LMS2RGB[0][1] * lms[1] + LMS2RGB[0][2] * lms[2],
    g: LMS2RGB[1][0] * lms[0] + LMS2RGB[1][1] * lms[1] + LMS2RGB[1][2] * lms[2],
    b: LMS2RGB[2][0] * lms[0] + LMS2RGB[2][1] * lms[1] + LMS2RGB[2][2] * lms[2],
  }
}

const inGamut = ({ r, g, b }: Rgb) =>
  r >= 6 && g >= 6 && b >= 6 && r <= 249 && g <= 249 && b <= 249

export function clamp(c: Rgb): Rgb {
  return {
    r: Math.max(0, Math.min(255, c.r)),
    g: Math.max(0, Math.min(255, c.g)),
    b: Math.max(0, Math.min(255, c.b)),
  }
}

export const css = (c: Rgb) =>
  `rgb(${Math.round(c.r)}, ${Math.round(c.g)}, ${Math.round(c.b)})`

/** Воспринимаемая светлота (та же формула, что у яркости sRGB). */
export const luma = ({ r, g, b }: Rgb) => 0.2126 * r + 0.7152 * g + 0.0722 * b

/** Как этот цвет видит дальтоник: проекция LMS на плоскость дихромата (Вьено). */
export function simulate(c: Rgb, type: CvdType): Rgb {
  const [l, m, s] = toLms(c)
  switch (type) {
    case 'protan':
      return fromLms([2.02344 * m - 2.52581 * s, m, s])
    case 'deutan':
      return fromLms([l, 0.494207 * l + 1.24827 * s, s])
    default:
      return fromLms([l, m, -0.395913 * l + 0.801109 * m])
  }
}

/**
 * Коррекция (дальтонизация): то, что человек не различает, переносится в
 * каналы, которые он видит. Разница между оригиналом и симуляцией —
 * потерянная информация; её и подмешиваем.
 */
export function daltonize(c: Rgb, type: CvdType, strength = 1): Rgb {
  const sim = simulate(c, type)
  const dr = c.r - sim.r
  const dg = c.g - sim.g
  const db = c.b - sim.b
  // классическая матрица переноса ошибки: красно-зелёное уходит в синий и наоборот
  const shift = {
    r: 0,
    g: 0.7 * dr + dg,
    b: 0.7 * dr + db,
  }
  return clamp({
    r: c.r + strength * shift.r,
    g: c.g + strength * shift.g,
    b: c.b + strength * shift.b,
  })
}

/** Серый по светлоте: проверка, что фигура не выдаёт себя яркостью. */
export function toGray(c: Rgb): Rgb {
  const y = luma(c)
  return { r: y, g: y, b: y }
}

/** Усиление разницы между цветом и средним цветом таблицы. */
export function boost(c: Rgb, mid: Rgb, k = 2.4): Rgb {
  return clamp({
    r: mid.r + (c.r - mid.r) * k,
    g: mid.g + (c.g - mid.g) * k,
    b: mid.b + (c.b - mid.b) * k,
  })
}

/** Подогнать светлоту цвета под образец, не меняя оттенок. */
export function matchLuma(c: Rgb, target: number): Rgb {
  const y = luma(c)
  if (y <= 1) return c
  const k = target / y
  // чистое умножение уводит в потолок, поэтому ярче 255 тянем к белому
  const scaled = { r: c.r * k, g: c.g * k, b: c.b * k }
  const over = Math.max(scaled.r, scaled.g, scaled.b)
  if (over <= 255) return clamp(scaled)
  const t = (over - 255) / over
  return clamp({
    r: scaled.r * (1 - t) + 255 * t,
    g: scaled.g * (1 - t) + 255 * t,
    b: scaled.b * (1 - t) + 255 * t,
  })
}

/**
 * Пара «цвет фона — цвет фигуры» на линии спутывания: меняем только тот канал
 * колбочек, который у этого типа дальтонизма не работает.
 *
 * Разницу подбираем, а не берём на глаз: слишком большая вылезает за пределы
 * экрана, обрезается — и тогда дальтоник начинает различать цвета, то есть
 * таблица перестаёт работать.
 *
 * Светлоту пары намеренно НЕ выравниваем. Любая правка яркости двигает все три
 * канала колбочек, в том числе «рабочие», и цвета перестают сливаться — в
 * замерах расхождение после выравнивания было вдесятеро больше. В оригинальных
 * таблицах эту задачу решает не равенство яркостей, а шум: кружки случайно
 * светлее и темнее, и по яркости фигуру всё равно не собрать. Поэтому здесь
 * только ограничиваем разницу светлот, а маскирует её шум при отрисовке.
 */
export function confusionPair(base: Rgb, type: CvdType, delta = 0.26): [Rgb, Rgb] {
  const lms = toLms(base)
  const axis = type === 'protan' ? 0 : type === 'deutan' ? 1 : 2
  let fallback: [Rgb, Rgb] = [clamp(base), clamp(base)]

  for (let d = delta; d > 0.02; d *= 0.85) {
    const a: Lms = [...lms] as Lms
    const b: Lms = [...lms] as Lms
    a[axis] *= 1 - d
    b[axis] *= 1 + d
    const first = fromLmsRaw(a)
    const second = fromLmsRaw(b)
    if (!inGamut(first) || !inGamut(second)) continue

    const pair: [Rgb, Rgb] = [clamp(first), clamp(second)]
    fallback = pair
    const normal = distance(pair[0], pair[1])
    const lumaGap = Math.abs(luma(pair[0]) - luma(pair[1]))
    // разницу по цвету берём максимально возможную, но по яркости — умеренную:
    // слишком светлую фигуру шум уже не спрячет
    if (normal > 26 && lumaGap < 20) return pair
  }
  return fallback
}

/** Насколько два цвета различимы (грубая метрика, без перехода в Lab). */
export function distance(a: Rgb, b: Rgb): number {
  return Math.hypot(a.r - b.r, a.g - b.g, a.b - b.b)
}
