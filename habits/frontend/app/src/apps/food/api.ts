import { api } from '../../shared/api/client'
import type {
  FoodDayStat,
  FoodDiary,
  FoodGoal,
  FoodItem,
  FoodMeal,
  FoodProduct,
  FoodProfile,
  FoodRecipe,
  FoodShareUser,
  FoodTargets,
  FoodTemplate,
} from './types'

// --- профиль и цели ---

export function fetchProfile() {
  return api.get<{ profile: FoodProfile | null; goal: FoodGoal | null }>('/food/profile')
}

export function saveProfile(p: FoodProfile) {
  return api.put<{ profile: FoodProfile }>('/food/profile', p)
}

export function calculateTargets(p: FoodProfile) {
  return api.post<{ targets: FoodTargets }>('/food/profile/calculate', p)
}

export function fetchGoals() {
  return api.get<{ goals: FoodGoal[] }>('/food/goals')
}

export function createGoal(g: Partial<FoodGoal>) {
  return api.post<{ goal: FoodGoal }>('/food/goals', g)
}

// --- продукты ---

export function fetchProducts(q = '', archived = false, limit = 50, offset = 0) {
  const p = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  if (q) p.set('q', q)
  if (archived) p.set('archived', 'true')
  return api.get<{ products: FoodProduct[] }>(`/food/products?${p}`)
}

export function createProduct(p: Partial<FoodProduct>) {
  return api.post<{ product: FoodProduct }>('/food/products', p)
}

export function updateProduct(id: number, p: Partial<FoodProduct>) {
  return api.put<{ product: FoodProduct }>(`/food/products/${id}`, p)
}

export function deleteProduct(id: number) {
  return api.delete<{ deleted: boolean; archived: boolean }>(`/food/products/${id}`)
}

// --- дневник ---

export function fetchDiary(date: string) {
  return api.get<FoodDiary>(`/food/diary?date=${date}`)
}

export interface MealPayload {
  day?: string
  time?: string
  meal_type?: string
  name?: string
  description?: string
  photo?: string
  items?: FoodItem[]
  calories?: number
  protein?: number
  fat?: number
  carbs?: number
}

export function createMeal(m: MealPayload) {
  return api.post<{ meal: FoodMeal }>('/food/meals', m)
}

export function updateMeal(id: number, m: MealPayload) {
  return api.put<{ meal: FoodMeal }>(`/food/meals/${id}`, m)
}

export function deleteMeal(id: number) {
  return api.delete<void>(`/food/meals/${id}`)
}

export function duplicateMeal(id: number, day?: string) {
  return api.post<{ meal: FoodMeal }>(`/food/meals/${id}/duplicate`, { day })
}

export function mealToTemplate(id: number, name?: string) {
  return api.post<{ template: FoodTemplate }>(`/food/meals/${id}/save-as-template`, { name })
}

// --- шаблоны ---

export function fetchTemplates() {
  return api.get<{ templates: FoodTemplate[] }>('/food/templates')
}

export function createTemplate(t: Partial<FoodTemplate>) {
  return api.post<{ template: FoodTemplate }>('/food/templates', t)
}

export function updateTemplate(id: number, t: Partial<FoodTemplate>) {
  return api.put<{ template: FoodTemplate }>(`/food/templates/${id}`, t)
}

export function deleteTemplate(id: number) {
  return api.delete<void>(`/food/templates/${id}`)
}

export function templateToMeal(id: number, day: string, mealType?: string, time?: string) {
  return api.post<{ meal: FoodMeal }>(`/food/templates/${id}/create-meal`, {
    day,
    meal_type: mealType,
    time,
  })
}

// --- рецепты ---

export interface RecipeResponse {
  recipe: FoodRecipe
  per100?: { calories: number; protein: number; fat: number; carbs: number }
  per_portion?: { calories: number; protein: number; fat: number; carbs: number }
}

export function fetchRecipes() {
  return api.get<{ recipes: FoodRecipe[] }>('/food/recipes')
}

export function createRecipe(r: Partial<FoodRecipe>) {
  return api.post<RecipeResponse>('/food/recipes', r)
}

export function updateRecipe(id: number, r: Partial<FoodRecipe>) {
  return api.put<RecipeResponse>(`/food/recipes/${id}`, r)
}

export function deleteRecipe(id: number) {
  return api.delete<void>(`/food/recipes/${id}`)
}

export function recipeToMeal(
  id: number,
  day: string,
  opts: { grams?: number; portions?: number; meal_type?: string; time?: string },
) {
  return api.post<{ meal: FoodMeal }>(`/food/recipes/${id}/create-meal`, { day, ...opts })
}

// --- шаринг ---

export function shareDiary(to: string) {
  return api.post<{ queued: boolean }>('/food/shares', { to })
}

export function fetchShares() {
  return api.get<{ users: FoodShareUser[] }>('/food/shares')
}

export function updateShare(userId: number, flags: Partial<FoodShareUser>) {
  return api.patch<{ flags: unknown }>(`/food/shares/${userId}`, flags)
}

export function revokeShare(userId: number) {
  return api.delete<void>(`/food/shares/${userId}`)
}

export function fetchSharedWithMe() {
  return api.get<{ owners: FoodShareUser[] }>('/food/shared')
}

export function leaveShared(ownerId: number) {
  return api.delete<void>(`/food/shared/${ownerId}`)
}

export function fetchSharedDiary(ownerId: number, date: string) {
  return api.get<FoodDiary>(`/food/shared/${ownerId}/diary?date=${date}`)
}

// --- статистика ---

export function fetchStats(from: string, to: string) {
  return api.get<{ days: FoodDayStat[]; goals: FoodGoal[] }>(`/food/stats?from=${from}&to=${to}`)
}

// --- фото ---

/** Загрузка фото (multipart) — api-клиент шлёт JSON, поэтому raw fetch. */
export async function uploadPhoto(file: File): Promise<{ url: string }> {
  const { apiBase, apiAuthHeader } = await import('../../shared/api/client')
  const fd = new FormData()
  fd.append('file', file)
  const res = await fetch(`${apiBase()}/food/upload`, {
    method: 'POST',
    headers: { Authorization: apiAuthHeader() },
    body: fd,
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new Error(data?.error?.message ?? 'Не удалось загрузить фото')
  return data as { url: string }
}
