import { api } from '../../shared/api/client'
import type { DiaryEntry } from './types'

export interface DiaryListParams {
  q?: string
  from?: string // YYYY-MM-DD
  to?: string // YYYY-MM-DD
  limit?: number
}

export function fetchEntries(params: DiaryListParams = {}) {
  const qs = new URLSearchParams()
  if (params.q) qs.set('q', params.q)
  if (params.from) qs.set('from', params.from)
  if (params.to) qs.set('to', params.to)
  if (params.limit) qs.set('limit', String(params.limit))
  const suffix = qs.size ? `?${qs}` : ''
  return api.get<{ entries: DiaryEntry[] }>(`/diary/entries${suffix}`)
}

export function createEntry(at: string, text: string) {
  return api.post<{ entry: DiaryEntry }>('/diary/entries', { at, text })
}

export function updateEntry(id: number, patch: { at?: string; text?: string }) {
  return api.patch<{ entry: DiaryEntry }>(`/diary/entries/${id}`, patch)
}

export function deleteEntry(id: number) {
  return api.delete<void>(`/diary/entries/${id}`)
}
