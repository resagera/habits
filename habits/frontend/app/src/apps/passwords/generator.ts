// Генератор паролей на crypto.getRandomValues без modulo bias
// (rejection sampling) и с гарантией хотя бы одного символа из каждого
// выбранного набора.

export interface GeneratorOptions {
  length: number
  lower: boolean
  upper: boolean
  digits: boolean
  symbols: boolean
}

export const DEFAULT_OPTIONS: GeneratorOptions = {
  length: 16,
  lower: true,
  upper: true,
  digits: true,
  symbols: false,
}

const SETS = {
  lower: 'abcdefghijklmnopqrstuvwxyz',
  upper: 'ABCDEFGHIJKLMNOPQRSTUVWXYZ',
  digits: '0123456789',
  symbols: '!@#$%^&*()-_=+[]{};:,.?/',
} as const

function randomInt(maxExclusive: number): number {
  const limit = Math.floor(0x100000000 / maxExclusive) * maxExclusive
  const buf = new Uint32Array(1)
  do {
    crypto.getRandomValues(buf)
  } while (buf[0] >= limit)
  return buf[0] % maxExclusive
}

function pick(alphabet: string): string {
  return alphabet[randomInt(alphabet.length)]
}

export function generatePassword(opts: GeneratorOptions): string {
  const chosen = (Object.keys(SETS) as (keyof typeof SETS)[]).filter((k) => opts[k])
  if (chosen.length === 0) chosen.push('lower')
  const length = Math.min(128, Math.max(chosen.length, Math.round(opts.length) || 16))
  const all = chosen.map((k) => SETS[k]).join('')

  // по одному символу из каждого набора, остальное — из общего алфавита
  const chars = chosen.map((k) => pick(SETS[k]))
  while (chars.length < length) chars.push(pick(all))

  // тасование Фишера–Йетса, чтобы гарантированные символы не липли к началу
  for (let i = chars.length - 1; i > 0; i--) {
    const j = randomInt(i + 1)
    ;[chars[i], chars[j]] = [chars[j], chars[i]]
  }
  return chars.join('')
}
