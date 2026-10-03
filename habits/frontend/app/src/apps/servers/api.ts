import { api } from '../../shared/api/client'
import type { ServerAlerts, ServerEntry, ServerSample } from './types'

export function fetchServers() {
  return api.get<{ servers: ServerEntry[] }>('/servers')
}

export function createServer(kind: 'pull' | 'push', name: string, url: string, token: string) {
  return api.post<{ server: ServerEntry }>('/servers', { kind, name, url, token })
}

export function updateServer(
  id: number,
  kind: 'pull' | 'push',
  name: string,
  url: string,
  token: string,
  alerts?: ServerAlerts,
) {
  return api.put<{ server: ServerEntry }>(`/servers/${id}`, { kind, name, url, token, alerts })
}

export function deleteServer(id: number) {
  return api.delete<void>(`/servers/${id}`)
}

export function fetchHistory(id: number, hours = 24) {
  return api.get<{ samples: ServerSample[] }>(`/servers/${id}/history?hours=${hours}`)
}

export function refreshServer(id: number) {
  return api.post<{ server: ServerEntry }>(`/servers/${id}/refresh`)
}
