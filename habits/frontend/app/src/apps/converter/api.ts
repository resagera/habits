import { api } from '../../shared/api/client'

export function fetchCurrencies() {
  return api.get<{ currencies: string[] }>('/converter/currencies')
}

export function addCurrency(code: string) {
  return api.post<{ code: string }>('/converter/currencies', { code })
}

export function removeCurrency(code: string) {
  return api.delete<void>(`/converter/currencies/${code}`)
}

export function fetchRates(base: string, targets: string[]) {
  return api.get<{ base: string; date: string; rates: Record<string, number> }>(
    `/converter/rates?base=${base}&targets=${targets.join(',')}`,
  )
}

export interface AvailableCurrency {
  code: string
  name: string
  crypto: boolean
}

/** Справочник источника: фиат и криптовалюты. */
export function fetchAvailable() {
  return api.get<{ currencies: AvailableCurrency[] }>('/converter/available')
}

export interface RateSeries {
  code: string
  days: string[]
  rates: number[]
}

/** Курс за период. Недостающие дни сервер докачивает сам. */
export function fetchHistory(base: string, targets: string[], days = 30) {
  return api.get<{ base: string; series: RateSeries[] }>(
    `/converter/history?base=${base}&targets=${targets.join(',')}&days=${days}`,
  )
}
