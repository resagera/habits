// Food: типы и справочники

export type MealType = 'breakfast' | 'lunch' | 'dinner' | 'snack' | 'none'
export type FoodUnit = 'g' | 'ml' | 'piece' | 'portion'

export const MEAL_TYPE_LABELS: Record<MealType, string> = {
  breakfast: '🌅 Завтрак',
  lunch: '🍲 Обед',
  dinner: '🌇 Ужин',
  snack: '🍎 Перекусы',
  none: '📦 Без категории',
}
export const MEAL_TYPES: MealType[] = ['breakfast', 'lunch', 'dinner', 'snack', 'none']

export const UNIT_LABELS: Record<FoodUnit, string> = {
  g: 'г',
  ml: 'мл',
  piece: 'шт',
  portion: 'порц.',
}

export const ACTIVITY_LABELS: Record<string, string> = {
  min: 'Минимальная (сидячий образ)',
  low: 'Низкая (1–3 тренировки/нед)',
  medium: 'Средняя (3–5 тренировок/нед)',
  high: 'Высокая (6–7 тренировок/нед)',
  max: 'Очень высокая (физ. работа)',
}

export const GOAL_LABELS: Record<string, string> = {
  lose: '📉 Похудение',
  maintain: '⚖️ Поддержание',
  gain: '📈 Набор веса',
}

export const PROTEIN_BASE_LABELS: Record<string, string> = {
  current: 'От текущего веса',
  target: 'От целевого веса',
  lean: 'От безжировой массы',
  manual: 'Вручную (кг)',
}

// подсказки коэффициента белка (г/кг)
export const PROTEIN_COEF_PRESETS = [
  { v: 1.0, label: '1.0 — минимальная активность' },
  { v: 1.2, label: '1.2 — обычная активность' },
  { v: 1.6, label: '1.6 — регулярные тренировки / набор массы' },
  { v: 1.8, label: '1.8 — похудение с тренировками' },
]

export interface FoodItem {
  id?: number
  product_id: number | null
  name: string
  amount: number
  unit: FoodUnit
  grams: number
  base_type: 'g' | 'ml'
  calories_per: number
  protein_per: number
  fat_per: number
  carbs_per: number
  calories: number
  protein: number
  fat: number
  carbs: number
}

export interface FoodMeal {
  id: number
  day: string
  time: string
  meal_type: MealType
  name: string
  description: string
  photo: string
  source_type: '' | 'template' | 'recipe'
  source_id: number | null
  calories: number
  protein: number
  fat: number
  carbs: number
  items: FoodItem[]
}

export interface FoodProduct {
  id: number
  name: string
  alt_name: string
  brand: string
  category: string
  photo: string
  base_type: 'g' | 'ml'
  calories: number
  protein: number
  fat: number
  carbs: number
  piece_grams: number
  portion_grams: number
  archived: boolean
  used_count: number
  recent: boolean
}

export interface FoodTemplate {
  id: number
  name: string
  description: string
  photo: string
  meal_type: MealType
  archived: boolean
  calories: number
  protein: number
  fat: number
  carbs: number
  items: FoodItem[]
}

export interface FoodRecipe {
  id: number
  name: string
  description: string
  steps: string
  photo: string
  final_weight: number
  portions: number
  archived: boolean
  calories: number
  protein: number
  fat: number
  carbs: number
  items: FoodItem[]
}

export interface FoodProfile {
  sex: '' | 'male' | 'female'
  birth_date: string
  height_cm: number
  weight_kg: number
  target_weight_kg: number
  body_fat_percent: number
  activity_level: string
  goal_type: string
  rate_kcal: number
  protein_base: string
  protein_base_kg: number
  protein_coef: number
}

export interface FoodTargets {
  bmr: number
  tdee: number
  calories: number
  protein: number
  fat: number
  carbs: number
  details: string
}

export interface FoodGoal {
  id: number
  date_from: string
  date_to: string
  goal_type: string
  calories: number
  protein: number
  fat: number
  carbs: number
  source: string
  details: string
}

export interface FoodSummary {
  calories: number
  protein: number
  fat: number
  carbs: number
}

export interface FoodDiary {
  date: string
  meals: FoodMeal[]
  summary: FoodSummary
  goal: FoodGoal | null
  weight_kg?: number
}

export interface FoodShareUser {
  id: number
  username: string
  first_name: string
  show_weight: boolean
  show_goals: boolean
  show_photos: boolean
  show_notes: boolean
}

export interface FoodDayStat {
  day: string
  calories: number
  protein: number
  fat: number
  carbs: number
  meals: number
  breakfast: number
  lunch: number
  dinner: number
  snack: number
  other: number
}

// --- утилиты ---

export function assetUrl(u: string): string {
  return import.meta.env.BASE_URL + u
}

/** Сегодня в часовом поясе пользователя (YYYY-MM-DD). */
export function todayStr(): string {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

export function addDays(day: string, n: number): string {
  const d = new Date(day + 'T12:00:00')
  d.setDate(d.getDate() + n)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

/** «21 июля, вт» */
export function fmtDay(day: string): string {
  const d = new Date(day + 'T12:00:00')
  return d.toLocaleDateString('ru-RU', { day: 'numeric', month: 'long', weekday: 'short' })
}

/** Округление только для отображения. */
export function r0(v: number): string {
  return String(Math.round(v))
}

export function r1(v: number): string {
  return (Math.round(v * 10) / 10).toString()
}

export function userLabel(u: { first_name: string; username: string; id: number }): string {
  return u.first_name || (u.username ? '@' + u.username : `#${u.id}`)
}
