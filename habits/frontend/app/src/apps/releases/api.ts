import { api } from '../../shared/api/client'
import type { Release } from './types'

export function fetchReleases(): Promise<{ releases: Release[] }> {
  return api.get('/releases')
}

/** Правка комментария/статуса релиза (только админ). */
export function updateRelease(
  id: number,
  body: { comment?: string; status?: string },
): Promise<{ release: Release }> {
  return api.patch(`/releases/${id}`, body)
}
