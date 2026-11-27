// Построчный diff «как git»: только изменения с небольшим контекстом.
// Общие префикс/суффикс отбрасываются, середина сравнивается через LCS;
// для очень больших фрагментов середина показывается как замена целиком.

export interface DiffLine {
  type: 'ctx' | 'add' | 'del' | 'skip'
  text: string
}

const CONTEXT = 2
const MAX_LCS = 500 // строк в середине, дальше LCS не считаем (O(n·m))

/** diffLines(old, new): хунки изменений (skip = разрыв между хунками). */
export function diffLines(oldText: string, newText: string): DiffLine[] {
  const a = oldText.split('\n')
  const b = newText.split('\n')

  // общий префикс и суффикс
  let start = 0
  while (start < a.length && start < b.length && a[start] === b[start]) start++
  let endA = a.length
  let endB = b.length
  while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) {
    endA--
    endB--
  }

  const midA = a.slice(start, endA)
  const midB = b.slice(start, endB)
  if (midA.length === 0 && midB.length === 0) return []

  // полный список строк середины с типами
  let ops: DiffLine[]
  if (midA.length > MAX_LCS || midB.length > MAX_LCS) {
    ops = [
      ...midA.map((text) => ({ type: 'del' as const, text })),
      ...midB.map((text) => ({ type: 'add' as const, text })),
    ]
  } else {
    ops = lcsDiff(midA, midB)
  }

  // добавляем контекст по краям и внутри, сжимая длинные общие участки
  const full: DiffLine[] = [
    ...a.slice(Math.max(0, start - CONTEXT), start).map((text) => ({ type: 'ctx' as const, text })),
    ...ops,
    ...a.slice(endA, Math.min(a.length, endA + CONTEXT)).map((text) => ({ type: 'ctx' as const, text })),
  ]
  return squeeze(full)
}

function lcsDiff(a: string[], b: string[]): DiffLine[] {
  const n = a.length
  const m = b.length
  // таблица длин LCS
  const dp: Int32Array[] = []
  for (let i = 0; i <= n; i++) dp.push(new Int32Array(m + 1))
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1])
    }
  }
  const out: DiffLine[] = []
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push({ type: 'ctx', text: a[i] })
      i++
      j++
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      out.push({ type: 'del', text: a[i] })
      i++
    } else {
      out.push({ type: 'add', text: b[j] })
      j++
    }
  }
  while (i < n) out.push({ type: 'del', text: a[i++] })
  while (j < m) out.push({ type: 'add', text: b[j++] })
  return out
}

/** Длинные общие участки внутри сжимаются в «⋯» с CONTEXT строк по краям. */
function squeeze(lines: DiffLine[]): DiffLine[] {
  const out: DiffLine[] = []
  let run: DiffLine[] = []
  const flush = (isEdge: boolean) => {
    if (run.length <= CONTEXT * 2 + 1 || isEdge) {
      out.push(...run)
    } else {
      out.push(...run.slice(0, CONTEXT))
      out.push({ type: 'skip', text: `⋯ ${run.length - CONTEXT * 2} строк без изменений` })
      out.push(...run.slice(-CONTEXT))
    }
    run = []
  }
  for (const l of lines) {
    if (l.type === 'ctx') {
      run.push(l)
    } else {
      flush(out.length === 0)
      out.push(l)
    }
  }
  // хвостовой общий участок обрезаем до CONTEXT
  if (run.length > CONTEXT) {
    out.push(...run.slice(0, CONTEXT))
    if (run.length > CONTEXT + 1) out.push({ type: 'skip', text: `⋯` })
  } else {
    out.push(...run)
  }
  return out
}
