import { api, apiBase } from '../../shared/api/client'
import type { TerminalMachine } from './types'

export function fetchMachines() {
  return api.get<{ machines: TerminalMachine[] }>('/terminal/machines')
}

export function createMachine(name: string) {
  return api.post<{ machine: TerminalMachine }>('/terminal/machines', { name })
}

export function renameMachine(id: number, name: string) {
  return api.patch<{ machine: TerminalMachine }>(`/terminal/machines/${id}`, { name })
}

export function deleteMachine(id: number) {
  return api.delete<void>(`/terminal/machines/${id}`)
}

export function openSession(id: number) {
  return api.post<{ ticket: string }>(`/terminal/machines/${id}/session`)
}

/** WebSocket-URL консоли по одноразовому пропуску (ws/wss под тем же хостом). */
export function streamWsUrl(ticket: string): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${location.host}${apiBase()}/terminal/stream/${ticket}`
}
