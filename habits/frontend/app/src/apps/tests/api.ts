import { api } from '../../shared/api/client'
import type {
  AnswerResult, ReviewItem, TestDeck, TestGroup, TestQuestion, TestScope, TestSession,
} from './types'

export function fetchDecks(): Promise<{ decks: TestDeck[]; active: Record<string, TestSession> }> {
  return api.get('/tests/decks')
}

export function fetchGroups(deckId: number): Promise<{ groups: TestGroup[] }> {
  return api.get(`/tests/decks/${deckId}/groups`)
}

export function startSession(body: {
  deck_id: number
  mode?: 'study' | 'exam'
  scope?: TestScope
  group_id?: number
  limit?: number
}): Promise<{ session: TestSession }> {
  return api.post('/tests/sessions', body)
}

export function fetchSession(
  id: number,
): Promise<{ session: TestSession; question?: TestQuestion; position?: number }> {
  return api.get(`/tests/sessions/${id}`)
}

export function sendAnswer(
  sessionId: number, questionId: number, chosen: number,
): Promise<AnswerResult> {
  return api.post(`/tests/sessions/${sessionId}/answer`, {
    question_id: questionId, chosen,
  })
}

export function finishSession(id: number): Promise<{ session: TestSession }> {
  return api.post(`/tests/sessions/${id}/finish`, {})
}

export function fetchReview(id: number): Promise<{ items: ReviewItem[] }> {
  return api.get(`/tests/sessions/${id}/review`)
}

export function resetDeck(deckId: number, hard = false): Promise<{ reset: number }> {
  return api.post(`/tests/decks/${deckId}/reset`, { hard })
}

export function fetchTestsSettings(): Promise<{ pass_streak: number }> {
  return api.get('/tests/settings')
}

export function saveTestsSettings(passStreak: number): Promise<{ pass_streak: number }> {
  return api.put('/tests/settings', { pass_streak: passStreak })
}
