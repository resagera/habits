import { reactive, ref } from 'vue'
import { ApiError } from '../../../shared/api/client'
import { showToast } from '../../../shared/toast'
import * as trackerApi from '../api'
import { addDays, lastWeekStarts, toISODate, WEEKS_TO_SHOW } from '../dates'
import type { Category, CategoryMarks, CategoryPatch, MarkInfo } from '../types'

const ACTIVE_KEY = 'tracker_active_v1'
const RECENT_COLORS_KEY = 'tracker_recent_colors'
const RECENT_EMOJI_KEY = 'tracker_recent_emoji'
const RECENT_MAX = 10

function readJSON<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key)
    return raw ? (JSON.parse(raw) as T) : fallback
  } catch {
    return fallback
  }
}

export type Tracker = ReturnType<typeof useTracker>

export function useTracker() {
  const categories = ref<Category[]>([])
  // category id -> day YYYY-MM-DD -> отметка
  const marks = reactive(new Map<number, Map<string, MarkInfo>>())
  const loading = ref(true)

  // активный цвет/эмодзи мультитрекеров (запоминаем на устройстве)
  const active = reactive(
    readJSON<Record<number, { color?: string; emoji?: string }>>(ACTIVE_KEY, {}),
  )
  const recentColors = ref(readJSON<string[]>(RECENT_COLORS_KEY, []))
  const recentEmoji = ref(readJSON<string[]>(RECENT_EMOJI_KEY, []))

  function saveActive() {
    localStorage.setItem(ACTIVE_KEY, JSON.stringify(active))
  }

  function pushRecent(list: { value: string[] }, key: string, v: string) {
    list.value = [v, ...list.value.filter((x) => x !== v)].slice(0, RECENT_MAX)
    localStorage.setItem(key, JSON.stringify(list.value))
  }

  function activeColor(cat: Category): string {
    return active[cat.id]?.color ?? cat.color
  }

  function setActiveColor(cat: Category, color: string) {
    active[cat.id] = { ...active[cat.id], color }
    saveActive()
    pushRecent(recentColors, RECENT_COLORS_KEY, color)
  }

  function activeEmoji(cat: Category): string {
    return active[cat.id]?.emoji ?? cat.emoji
  }

  function setActiveEmoji(cat: Category, emoji: string) {
    active[cat.id] = { ...active[cat.id], emoji }
    saveActive()
    pushRecent(recentEmoji, RECENT_EMOJI_KEY, emoji)
  }

  function catMap(categoryId: number): Map<string, MarkInfo> {
    let m = marks.get(categoryId)
    if (!m) {
      m = new Map()
      marks.set(categoryId, m)
    }
    return m
  }

  /** Слить отметки диапазона в кэш: старые записи диапазона заменяются свежими. */
  function mergeRange(catMarks: CategoryMarks[], from: string, to: string, categoryId?: number) {
    const ids = categoryId ? [categoryId] : categories.value.map((c) => c.id)
    for (const id of ids) {
      const m = marks.get(id)
      if (!m) continue
      for (const day of m.keys()) {
        if (day >= from && day <= to) m.delete(day)
      }
    }
    for (const cm of catMarks) {
      const m = catMap(cm.category_id)
      for (const d of cm.days) {
        m.set(d.day, { color: d.color ?? null, emoji: d.emoji ?? null, count: d.count })
      }
    }
  }

  async function load() {
    const weekStarts = lastWeekStarts(WEEKS_TO_SHOW)
    const from = toISODate(weekStarts[0])
    const to = toISODate(addDays(weekStarts[weekStarts.length - 1], 6))
    try {
      const [cats, mks] = await Promise.all([
        trackerApi.fetchCategories(),
        trackerApi.fetchMarks(from, to),
      ])
      categories.value = cats.categories
      marks.clear()
      mergeRange(mks.marks, from, to)
    } catch (e) {
      showToast(errorText(e))
    } finally {
      loading.value = false
    }
  }

  function markInfo(categoryId: number, day: string): MarkInfo | undefined {
    return marks.get(categoryId)?.get(day)
  }

  /**
   * Клик по дню обычного трекера. Для мультицветных/мульти-эмодзи клик
   * по отметке другого цвета перекрашивает её в активный, того же — снимает.
   * Оптимистично, с откатом при ошибке.
   */
  async function toggle(category: Category, day: string): Promise<void> {
    const m = catMap(category.id)
    const prev = m.get(day)

    let color: string | undefined
    let emoji: string | undefined
    if (category.multi) {
      if (category.style === 'emoji') emoji = activeEmoji(category)
      else color = activeColor(category)
    }

    // ожидаемое состояние (та же логика, что на сервере)
    const repaint =
      prev && ((color && prev.color !== color) || (emoji && prev.emoji !== emoji))
    if (!prev || repaint) {
      m.set(day, { color: color ?? null, emoji: emoji ?? null, count: 1 })
    } else {
      m.delete(day)
    }

    try {
      const { marked } = await trackerApi.toggleMark(category.id, day, color, emoji)
      // сервер — источник истины (на случай гонки)
      if (marked) m.set(day, { color: color ?? null, emoji: emoji ?? null, count: 1 })
      else m.delete(day)
    } catch (e) {
      if (prev) m.set(day, prev)
      else m.delete(day)
      showToast(errorText(e))
    }
  }

  /** Счётчик: клик +1, долгое нажатие −1. Оптимистично, с откатом. */
  async function increment(category: Category, day: string, delta: 1 | -1): Promise<void> {
    const m = catMap(category.id)
    const prev = m.get(day)
    const next = Math.max(0, (prev?.count ?? 0) + delta)
    if (next === (prev?.count ?? 0)) return
    if (next === 0) m.delete(day)
    else m.set(day, { color: null, emoji: null, count: next })

    try {
      const { count } = await trackerApi.incrementMark(category.id, day, delta)
      if (count === 0) m.delete(day)
      else m.set(day, { color: null, emoji: null, count })
    } catch (e) {
      if (prev) m.set(day, prev)
      else m.delete(day)
      showToast(errorText(e))
    }
  }

  async function addCategory(name: string) {
    try {
      const { category } = await trackerApi.createCategory(name)
      categories.value.push(category)
      return true
    } catch (e) {
      showToast(errorText(e))
      return false
    }
  }

  async function patchCategory(id: number, patch: CategoryPatch) {
    try {
      const { category } = await trackerApi.updateCategory(id, patch)
      const i = categories.value.findIndex((c) => c.id === id)
      if (i >= 0) categories.value[i] = category
      return true
    } catch (e) {
      showToast(errorText(e))
      return false
    }
  }

  async function removeCategory(id: number) {
    try {
      await trackerApi.deleteCategory(id)
      categories.value = categories.value.filter((c) => c.id !== id)
      marks.delete(id)
      return true
    } catch (e) {
      showToast(errorText(e))
      return false
    }
  }

  /** Участник покидает чужой трекер. */
  async function leaveCategory(cat: Category, myId: number) {
    try {
      await trackerApi.revokeShare(cat.id, myId)
      categories.value = categories.value.filter((c) => c.id !== cat.id)
      marks.delete(cat.id)
      return true
    } catch (e) {
      showToast(errorText(e))
      return false
    }
  }

  return {
    categories,
    marks,
    loading,
    load,
    mergeRange,
    markInfo,
    toggle,
    increment,
    addCategory,
    patchCategory,
    removeCategory,
    leaveCategory,
    activeColor,
    setActiveColor,
    activeEmoji,
    setActiveEmoji,
    recentColors,
    recentEmoji,
  }
}

export function errorText(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'conflict') return 'Категория с таким именем уже есть'
    return e.message
  }
  return 'Ошибка сети'
}
