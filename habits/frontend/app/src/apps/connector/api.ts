import { api } from '../../shared/api/client'
import type {
  ConnectorOverview, ConnectorRule, ConnectorRun, RunResult, TestResult,
} from './types'

export function fetchConnector(): Promise<ConnectorOverview> {
  return api.get('/connector/overview')
}

export function fetchRuns(limit = 50): Promise<{ runs: ConnectorRun[] }> {
  return api.get(`/connector/runs?limit=${limit}`)
}

export type RuleBody = Pick<ConnectorRule,
  'name' | 'enabled' | 'sources' | 'match' | 'actions' | 'tz_offset_minutes'>

export function createRule(body: RuleBody): Promise<{ rule: ConnectorRule }> {
  return api.post('/connector/rules', body)
}

export function updateRule(id: number, body: RuleBody): Promise<{ rule: ConnectorRule }> {
  return api.put(`/connector/rules/${id}`, body)
}

export function setRuleEnabled(id: number, enabled: boolean): Promise<{ enabled: boolean }> {
  return api.post(`/connector/rules/${id}/enabled`, { enabled })
}

export function deleteRule(id: number): Promise<void> {
  return api.delete(`/connector/rules/${id}`)
}

/**
 * Проверка без действий. Правило уходит телом, а не по id: проверять надо и
 * несохранённое — иначе правило не настроить, не насорив в трекере.
 */
export function testRule(body: RuleBody, messageID?: number): Promise<TestResult> {
  return api.post('/connector/test', { rule: body, message_id: messageID ?? 0 })
}

/** Применить правило к письму по-настоящему (для писем до создания правила). */
export function applyRule(id: number, messageID: number): Promise<{ result: RunResult }> {
  return api.post(`/connector/rules/${id}/apply`, { message_id: messageID })
}
