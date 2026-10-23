import { api } from '../../shared/api/client'
import type {
  AIMachine,
  AIRun,
  AISchedule,
  AITask,
  CLISession,
  CLISessionDetail,
  ToolStatus,
  UsageInfo,
} from './types'

export function fetchMachines(): Promise<{ machines: AIMachine[] }> {
  return api.get('/ai/machines')
}

export function createMachine(name: string): Promise<{ machine: AIMachine }> {
  return api.post('/ai/machines', { name })
}

export function renameMachine(id: number, name: string): Promise<{ machine: AIMachine }> {
  return api.patch(`/ai/machines/${id}`, { name })
}

export function deleteMachine(id: number): Promise<void> {
  return api.delete(`/ai/machines/${id}`)
}

/** Проверка инструмента на машине (существует + авторизован). Долгая (~30с). */
export function checkTool(machineID: number, tool: 'claude' | 'codex'): Promise<{ tool: ToolStatus }> {
  return api.post(`/ai/machines/${machineID}/check`, { tool })
}

/** Оставшиеся лимиты инструмента (/usage у Claude Code, /status у Codex). */
export function fetchUsage(machineID: number, tool: 'claude' | 'codex'): Promise<{ usage: UsageInfo }> {
  return api.post(`/ai/machines/${machineID}/usage`, { tool })
}

/** Прошлые сессии инструмента на машине — только заголовки (индекс агента). */
export function fetchSessions(
  machineID: number,
  tool: 'claude' | 'codex',
): Promise<{ sessions: { sessions: CLISession[]; error?: string } }> {
  return api.post(`/ai/machines/${machineID}/sessions`, { tool })
}

/** Содержимое одной сессии — грузится по клику, файлы бывают большими. */
export function fetchSession(
  machineID: number,
  tool: 'claude' | 'codex',
  sessionID: string,
): Promise<{ session: CLISessionDetail }> {
  return api.post(`/ai/machines/${machineID}/session`, { tool, session_id: sessionID })
}

export function fetchTasks(machineID: number): Promise<{ tasks: AITask[] }> {
  return api.get(`/ai/tasks?machine_id=${machineID}`)
}

export function createTask(body: {
  machine_id: number
  tool: string
  workdir: string
  model: string
  params: string
  prompt: string
  mode?: string
  /** продолжить существующую сессию CLI на машине (--resume) */
  session_id?: string
}): Promise<{ task: AITask; run: AIRun; queued_offline: boolean }> {
  return api.post('/ai/tasks', body)
}

export function fetchTask(id: number): Promise<{ task: AITask; runs: AIRun[] }> {
  return api.get(`/ai/tasks/${id}`)
}

/** Новый прогон в той же задаче — с контекстом предыдущих (--resume). */
export function continueTask(
  id: number,
  prompt: string,
  mode = '',
): Promise<{ task: AITask; run: AIRun; queued_offline: boolean }> {
  return api.post(`/ai/tasks/${id}/runs`, { prompt, mode })
}

export function deleteTask(id: number): Promise<void> {
  return api.delete(`/ai/tasks/${id}`)
}

/** Остановить прогон: queued отменяется сразу, running — kill на агенте. */
export function cancelRun(id: number): Promise<void> {
  return api.post(`/ai/runs/${id}/cancel`)
}

// --- расписания ---

export type ScheduleBody = {
  machine_id: number
  tool: string
  workdir: string
  model: string
  params: string
  prompt: string
  period: 'daily' | 'weekly' | 'hours'
  at_minute: number
  dow: number
  every_hours: number
  tz_off: number
  enabled?: boolean
}

export function fetchSchedules(): Promise<{ schedules: AISchedule[] }> {
  return api.get('/ai/schedules')
}

export function createSchedule(body: ScheduleBody): Promise<{ schedule: AISchedule }> {
  return api.post('/ai/schedules', body)
}

export function updateSchedule(id: number, body: ScheduleBody): Promise<{ schedule: AISchedule }> {
  return api.patch(`/ai/schedules/${id}`, body)
}

export function deleteSchedule(id: number): Promise<void> {
  return api.delete(`/ai/schedules/${id}`)
}
