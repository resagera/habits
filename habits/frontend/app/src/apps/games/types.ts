// Общие типы страницы «Игры».

/** Код игры. Он же ключ рекорда на сервере и ключ набора картинок. */
export type GameCode = '2048' | 'fifteen' | 'pairs' | 'maze' | 'reaction' | 'puzzle'

export interface GameScore {
  /** код игры; с вариантом через двоеточие — «maze:huge», «puzzle:4» */
  game: string
  /** очки (2048), секунды (время) или миллисекунды (реакция) */
  best: number
  detail?: { moves?: number; tile?: number; avg?: number; cells?: number; shortest?: number }
  played: number
}

export interface GameImage {
  id: number
  url: string
  thumb: string
}

/** Назначение картинки: фон всей игры есть у каждой, рубашка карт — у «пары». */
export type SlotCode = 'game' | 'cards' | 'picture'

export interface GamesState {
  scores: GameScore[]
  backgrounds: Partial<Record<GameCode, Partial<Record<SlotCode, GameImage[]>>>>
}

/** Результат партии: что отправить на сервер. */
export interface GameResult {
  best: number
  detail?: Record<string, number>
  /** вариант игры (размер лабиринта, сетка пазла): у него свой рекорд */
  variant?: string
  /** другой код игры: режим с другой меркой (очки вместо миллисекунд) */
  game?: string
}

export interface SlotInfo {
  code: SlotCode
  title: string
  /** что именно изменится — пишем рядом с набором */
  hint: string
}

export interface GameInfo {
  code: GameCode
  title: string
  icon: string
  about: string
  /** меньше — лучше (время), иначе больше — лучше (очки) */
  lower: boolean
  /** наборы картинок этой игры */
  slots: SlotInfo[]
  /** можно позвать второго игрока — у таких игр на плитке человечек */
  invite?: boolean
}

const GAME_SLOT: SlotInfo = {
  code: 'game',
  title: 'Фон игры',
  hint: 'картинка под всем полем; выбирается случайно при каждом запуске',
}

export const GAMES: GameInfo[] = [
  {
    code: '2048',
    title: '2048',
    icon: '🔢',
    about: 'Двигайте плитки, одинаковые складываются. Цель — плитка 2048.',
    lower: false,
    slots: [GAME_SLOT],
  },
  {
    code: 'fifteen',
    title: 'Пятнашки',
    icon: '🧩',
    about: 'Соберите порядок, двигая костяшки в пустую клетку.',
    lower: true,
    slots: [GAME_SLOT],
  },
  {
    code: 'maze',
    title: 'Лабиринт',
    icon: '🌀',
    about: 'Доведите точку до выхода. Три размера — от экрана до огромного.',
    lower: true,
    slots: [GAME_SLOT],
  },
  {
    code: 'puzzle',
    title: 'Пазл',
    icon: '🖼',
    about: 'Соберите свою картинку: нажали на два кусочка — они поменялись.',
    lower: true,
    slots: [
      GAME_SLOT,
      { code: 'picture', title: 'Картинка пазла', hint: 'её и будете собирать; без набора рисуется своя' },
    ],
  },
  {
    code: 'reaction',
    title: 'Реакция',
    icon: '⚡',
    about: 'Пять попыток: нажать, как только поле станет зелёным.',
    lower: true,
    slots: [GAME_SLOT],
  },
  {
    code: 'pairs',
    title: 'Найди пару',
    icon: '🃏',
    about: 'Откройте все пары за меньшее время.',
    lower: true,
    slots: [
      GAME_SLOT,
      { code: 'cards', title: 'Рубашка карт', hint: 'картинка на закрытых картах' },
    ],
  },
]

/** «2:05» — время партии человеческим текстом. */
export function fmtTime(seconds: number): string {
  if (!seconds) return '—'
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

/**
 * Рекорд для плитки. У лабиринта рекорды раздельные по размерам, поэтому
 * показываем лучший из них — подробности видно при выборе размера.
 */
export function bestScore(info: GameInfo, scores: GameScore[]): GameScore | undefined {
  const own = scores.filter((s) => s.game === info.code || s.game.startsWith(`${info.code}:`))
  if (!own.length) return undefined
  return own.reduce((a, b) => {
    if (!a.best) return b
    if (!b.best) return a
    return info.lower ? (b.best < a.best ? b : a) : (b.best > a.best ? b : a)
  })
}

/** Рекорд одной строкой: у очков и у времени разные подписи. */
export function fmtBest(info: GameInfo, score?: GameScore): string {
  if (!score || !score.best) return 'ещё не играли'
  if (!info.lower) {
    const tile = score.detail?.tile
    return `рекорд ${score.best}${tile ? ` · плитка ${tile}` : ''}`
  }
  if (info.code === 'reaction') {
    const avg = score.detail?.avg
    return `лучшая ${score.best} мс${avg ? ` · средняя ${avg}` : ''}`
  }
  const moves = score.detail?.moves
  return `лучшее время ${fmtTime(score.best)}${moves ? ` · ходов ${moves}` : ''}`
}
