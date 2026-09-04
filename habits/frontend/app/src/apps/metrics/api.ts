import { api } from '../../shared/api/client'
import type { ChartType, ItemConfig, MetricCategory, MetricItem, MetricValue } from './types'

export function fetchChartTypes() {
  return api.get<{ chart_types: ChartType[] }>('/metrics/chart-types')
}

export function fetchTree() {
  return api.get<{ categories: MetricCategory[] }>('/metrics/tree')
}

export function createCategory(name: string) {
  return api.post<{ category: MetricCategory }>('/metrics/categories', { name })
}

export function renameCategory(id: number, name: string) {
  return api.patch<{ category: MetricCategory }>(`/metrics/categories/${id}`, { name })
}

export function deleteCategory(id: number) {
  return api.delete<void>(`/metrics/categories/${id}`)
}

export function createItem(categoryId: number, name: string, chartType: string, config: ItemConfig) {
  return api.post<{ item: MetricItem }>(`/metrics/categories/${categoryId}/items`, {
    name,
    chart_type: chartType,
    config,
  })
}

export function updateItem(id: number, patch: { name?: string; chart_type?: string; config?: ItemConfig }) {
  return api.patch<{ item: MetricItem }>(`/metrics/items/${id}`, patch)
}

export function deleteItem(id: number) {
  return api.delete<void>(`/metrics/items/${id}`)
}

export function fetchValues(itemId: number, limit = 500) {
  return api.get<{ values: MetricValue[] }>(`/metrics/items/${itemId}/values?limit=${limit}`)
}

export function addValues(itemId: number, at: string | null, values: Record<string, number>) {
  return api.post<{ values: MetricValue[] }>(`/metrics/items/${itemId}/values`, {
    ...(at ? { at } : {}),
    values,
  })
}

export function updateValue(id: number, patch: { at?: string; value?: number }) {
  return api.patch<{ value: MetricValue }>(`/metrics/values/${id}`, patch)
}

export function deleteValue(id: number) {
  return api.delete<void>(`/metrics/values/${id}`)
}
