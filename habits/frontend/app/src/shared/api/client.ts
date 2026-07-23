import { getInitData } from '../telegram'

// BASE_URL = '/' в dev, '/app/habits/' в проде — API живёт под тем же префиксом
const BASE = import.meta.env.BASE_URL + 'api/v1'

export class ApiError extends Error {
  status: number
  code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

function authHeader(): string {
  const initData = getInitData()
  if (initData) return `tma ${initData}`
  // Local development outside Telegram: backend must run with DEV_AUTH_BYPASS=true.
  if (import.meta.env.DEV) return 'tma dev'
  return ''
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(BASE + path, {
    method,
    headers: {
      Authorization: authHeader(),
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    const err = data?.error
    throw new ApiError(res.status, err?.code ?? 'unknown', err?.message ?? res.statusText)
  }
  return data as T
}

async function upload<T>(path: string, form: FormData): Promise<T> {
  const res = await fetch(BASE + path, {
    method: 'POST',
    headers: { Authorization: authHeader() },
    body: form,
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    const err = data?.error
    throw new ApiError(res.status, err?.code ?? 'unknown', err?.message ?? res.statusText)
  }
  return data as T
}

/** Базовый URL API ('/app/habits/api/v1' в проде) — для прямых fetch/стриминга. */
export function apiBase(): string {
  return BASE
}

/** Значение заголовка Authorization для ручных запросов (стрим, upload). */
export function apiAuthHeader(): string {
  return authHeader()
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
  patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body),
  delete: <T>(path: string) => request<T>('DELETE', path),
  upload,
}
