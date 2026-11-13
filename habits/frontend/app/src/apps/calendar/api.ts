import { api } from '../../shared/api/client'
import type { CalendarPayload, CalPrefs } from './types'

export function fetchCalendar(from: string, to: string): Promise<CalendarPayload> {
  const tzOff = -new Date().getTimezoneOffset()
  return api.get(`/calendar?from=${from}&to=${to}&tz_off=${tzOff}`)
}

export function fetchPrefs(): Promise<{ prefs: Partial<CalPrefs> }> {
  return api.get('/calendar/prefs')
}

export function savePrefs(prefs: CalPrefs): Promise<void> {
  return api.put('/calendar/prefs', prefs)
}
