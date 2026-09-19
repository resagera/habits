// Экспорт/импорт групп Checker простым текстом и JSON. Поддерживается
// произвольная вложенность подгрупп: в тексте глубина задаётся числом «#»
// (# — подгруппа 1-го уровня, ## — 2-го, и т.д.), в JSON — вложенными
// массивами subgroups.
import type { CheckGroup } from './types'

export interface ExportItem {
  name: string
  done: boolean
}
export interface ExportSubgroup {
  name: string
  items: ExportItem[]
  subgroups: ExportSubgroup[]
}
export interface ExportGroup {
  name: string
  items: ExportItem[]
  subgroups: ExportSubgroup[]
}

/** Собирает дерево для экспорта из группы и всех её потомков (по плоскому списку). */
export function buildExport(group: CheckGroup, allGroups: CheckGroup[]): ExportGroup {
  const itemsOf = (g: CheckGroup): ExportItem[] => g.items.map((i) => ({ name: i.name, done: i.done }))
  const subsOf = (parentId: number): ExportSubgroup[] =>
    allGroups
      .filter((g) => g.parent_id === parentId)
      .map((g) => ({ name: g.name, items: itemsOf(g), subgroups: subsOf(g.id) }))
  return { name: group.name, items: itemsOf(group), subgroups: subsOf(group.id) }
}

export function toJson(g: ExportGroup): string {
  return JSON.stringify(g, null, 2)
}

/** Простой текст: имя, пункты «- …», подгруппы «#…» (глубина = число «#»). */
export function toText(g: ExportGroup): string {
  const lines: string[] = [g.name]
  for (const it of g.items) lines.push('- ' + it.name)
  const walk = (subs: ExportSubgroup[], depth: number) => {
    for (const sub of subs) {
      lines.push('#'.repeat(depth) + ' ' + sub.name)
      for (const it of sub.items) lines.push('- ' + it.name)
      walk(sub.subgroups, depth + 1)
    }
  }
  walk(g.subgroups, 1)
  return lines.join('\n')
}

function stripItem(line: string): ExportItem {
  const s = line.replace(/^\s*[-*•–]\s+/, '').trim()
  const m = s.match(/^\[([ xXхХ])\]\s*(.*)$/)
  if (m) {
    return { name: m[2].trim(), done: /[xXхХ]/.test(m[1]) }
  }
  return { name: s, done: false }
}

/** Разбирает простой текст в дерево с учётом глубины заголовков «#…». */
export function parseText(text: string): ExportGroup {
  const raw = text.split('\n')
  const out: ExportGroup = { name: '', items: [], subgroups: [] }
  // nodes[d] — текущий узел на глубине d (nodes[0] — корневая группа)
  const nodes: ExportGroup[] = [out]
  let cur: ExportItem[] = out.items
  let nameSet = false
  for (const rawLine of raw) {
    const line = rawLine.trim()
    if (!line) continue
    const heading = line.match(/^(#+)\s*(.*)$/)
    if (!nameSet && !heading && !/^[-*•–]\s/.test(line)) {
      out.name = line
      nameSet = true
      continue
    }
    if (heading) {
      // глубина = число «#»; родитель — на глубине depth-1 (с клампом, если
      // уровни пропущены — крепим к самому глубокому доступному узлу)
      const depth = heading[1].length
      const parentDepth = Math.min(depth - 1, nodes.length - 1)
      const sub: ExportSubgroup = { name: heading[2].trim(), items: [], subgroups: [] }
      nodes[parentDepth].subgroups.push(sub)
      nodes[parentDepth + 1] = sub
      nodes.length = parentDepth + 2 // отбрасываем более глубокие узлы
      cur = sub.items
      continue
    }
    const it = stripItem(line)
    if (it.name) cur.push(it)
  }
  return out
}

/** Автоопределение формата: JSON (начинается с «{») или простой текст. */
export function parseAny(text: string): ExportGroup {
  const t = text.trim()
  if (t.startsWith('{')) {
    const raw = JSON.parse(t) as Partial<ExportGroup>
    const items = (arr: unknown): ExportItem[] =>
      Array.isArray(arr)
        ? arr
            .map((x) =>
              typeof x === 'string'
                ? { name: x, done: false }
                : { name: String((x as ExportItem).name ?? '').trim(), done: !!(x as ExportItem).done },
            )
            .filter((i) => i.name)
        : []
    const subs = (arr: unknown): ExportSubgroup[] =>
      Array.isArray(arr)
        ? arr
            .map((s) => {
              const sub = s as Partial<ExportSubgroup>
              return { name: String(sub.name ?? '').trim(), items: items(sub.items), subgroups: subs(sub.subgroups) }
            })
            .filter((s) => s.name)
        : []
    return {
      name: String(raw.name ?? '').trim(),
      items: items(raw.items),
      subgroups: subs(raw.subgroups),
    }
  }
  return parseText(t)
}
