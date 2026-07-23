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
