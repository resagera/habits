// Поиск по группам Checker: по именам групп и по пунктам, с учётом произвольной
// вложенности подгрупп. Фильтр — нормализованная (lowercase, trim) строка;
// пустая строка означает «фильтр неактивен».
import type { CheckGroup, CheckItem } from './types'

export function normQuery(q: string): string {
  return q.trim().toLowerCase()
}

/**
 * Группа релевантна фильтру, если совпало её имя, любой её пункт или любой
 * потомок (подгруппа на любой глубине).
 */
export function groupRelevant(group: CheckGroup, groups: CheckGroup[], f: string): boolean {
  if (!f) return true
  if (group.name.toLowerCase().includes(f)) return true
  if (group.items.some((i) => i.name.toLowerCase().includes(f))) return true
  return groups.some((g) => g.parent_id === group.id && groupRelevant(g, groups, f))
}

/**
 * Видимые пункты группы при фильтре. Если совпало имя самой группы — показываем
 * все её пункты, иначе только совпавшие.
 */
export function visibleItems(group: CheckGroup, f: string): CheckItem[] {
  if (!f) return group.items
  if (group.name.toLowerCase().includes(f)) return group.items
  return group.items.filter((i) => i.name.toLowerCase().includes(f))
}

/**
 * Разбивает текст на куски для подсветки совпадений с фильтром (без учёта
 * регистра). Возвращает сегменты {text, hit}; hit=true — совпавший фрагмент.
 * Рендерить через <span>, не через v-html (текст пользовательский).
 */
export function highlightParts(text: string, f: string): { text: string; hit: boolean }[] {
  if (!f) return [{ text, hit: false }]
  const parts: { text: string; hit: boolean }[] = []
  const lower = text.toLowerCase()
  let i = 0
  while (i < text.length) {
    const idx = lower.indexOf(f, i)
    if (idx === -1) {
      parts.push({ text: text.slice(i), hit: false })
      break
    }
    if (idx > i) parts.push({ text: text.slice(i, idx), hit: false })
    parts.push({ text: text.slice(idx, idx + f.length), hit: true })
    i = idx + f.length
  }
  return parts
}
