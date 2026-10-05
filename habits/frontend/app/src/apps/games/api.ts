import { api } from '../../shared/api/client'
import type { GameCode, GameResult, GameScore, GamesState, SlotCode } from './types'

export function loadGames(): Promise<GamesState> {
  return api.get<GamesState>('/games')
}

export function sendResult(game: string, result: GameResult): Promise<{ score: GameScore }> {
  return api.post<{ score: GameScore }>(`/games/${game}/result`, result)
}

export function saveBackgrounds(
  game: GameCode, slot: SlotCode, imageIds: number[],
): Promise<GamesState> {
  return api.put<GamesState>(`/games/${game}/backgrounds/${slot}`, { image_ids: imageIds })
}
