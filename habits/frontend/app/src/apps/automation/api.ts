import { api } from '../../shared/api/client'
import type { AgentInfo, Automation, AutomationRun, RunStep } from './types'

export interface AutomationPayload {
  title?: string
  enabled?: boolean
  interval_days?: number
  next_run_at?: string // RFC3339 или '' — только вручную
  login?: string
  password?: string
  quantity?: number
  tare_mode?: string
  tare_qty?: number
  time_slot?: string
  payment?: string
  comment?: string
}

export function fetchAutomations() {
  return api.get<{ automations: Automation[] }>('/automation')
}

export function createAutomation(p: AutomationPayload) {
  return api.post<{ automation: Automation }>('/automation', p)
}

export function updateAutomation(id: number, p: AutomationPayload) {
  return api.patch<{ automation: Automation }>(`/automation/${id}`, p)
}

export function deleteAutomation(id: number) {
  return api.delete<void>(`/automation/${id}`)
}

export function fetchRuns(id: number) {
  return api.get<{ runs: AutomationRun[] }>(`/automation/${id}/runs`)
}

// Запуск (dry=true — пробный прогон без оформления). Выполняется синхронно.
export function runAutomation(id: number, dry: boolean) {
  return api.post<{ ok: boolean; steps: RunStep[]; error?: string }>(
    `/automation/${id}/run${dry ? '?dry=true' : ''}`,
  )
}

export function fetchAgentInfo() {
  return api.get<AgentInfo>('/automation/agent-info')
}

export function regenAgentToken() {
  return api.post<{ token: string }>('/automation/agent-token/regenerate')
}
