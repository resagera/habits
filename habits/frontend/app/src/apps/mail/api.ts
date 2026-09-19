import { api, apiAuthHeader, apiBase } from '../../shared/api/client'
import type {
  MailAddress, MailAttachment, MailIPStat, MailMessage, MailOverview, MailReceipt,
  MailReceiptItem,
} from './types'

export interface MailQuery {
  box?: string
  address_id?: number
  q?: string
  limit?: number
  offset?: number
}

export function fetchMail(q: MailQuery = {}): Promise<MailOverview> {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(q)) {
    if (v !== undefined && v !== '' && v !== 0) p.set(k, String(v))
  }
  const s = p.toString()
  return api.get(`/mail/overview${s ? `?${s}` : ''}`)
}

export function fetchMessage(
  id: number,
): Promise<{ message: MailMessage; attachments: MailAttachment[] }> {
  return api.get(`/mail/messages/${id}`)
}

export function setFlag(id: number, flag: string, value: boolean): Promise<{ ok: boolean }> {
  return api.post(`/mail/messages/${id}/flag`, { flag, value })
}

export function archiveMessage(id: number, archived: boolean): Promise<{ archived: boolean }> {
  return api.post(`/mail/messages/${id}/archive`, { archived })
}

export function deleteMessage(id: number): Promise<void> {
  return api.delete(`/mail/messages/${id}`)
}

export function createAddress(body: Partial<MailAddress>): Promise<{ address: MailAddress }> {
  return api.post('/mail/addresses', body)
}

export function updateAddress(
  id: number, body: Partial<MailAddress>,
): Promise<{ address: MailAddress }> {
  return api.put(`/mail/addresses/${id}`, body)
}

export function deleteAddress(id: number): Promise<void> {
  return api.delete(`/mail/addresses/${id}`)
}

/** Какой разборщик применять к письмам на адрес и куда писать трату. */
export function setAddressParser(
  id: number, body: { parser: string; category_id?: number | null; account_id?: number | null },
): Promise<{ ok: boolean }> {
  return api.put(`/mail/addresses/${id}/parser`, body)
}

/**
 * Разобрать письмо руками: для писем, пришедших до настройки разборщика.
 * refresh перечитывает уже разобранный чек — нужен, когда разбор поумнел
 * (например, научился понимать старый формат даты).
 */
export function parseMessage(
  id: number, opts: { parser?: string; refresh?: boolean } = {},
): Promise<{ receipt: MailReceipt; receipt_items: MailReceiptItem[]; refreshed?: boolean }> {
  return api.post(`/mail/messages/${id}/parse`, opts)
}

export function fetchReceiptItems(id: number): Promise<{ items: MailReceiptItem[] }> {
  return api.get(`/mail/receipts/${id}/items`)
}

export function deleteReceipt(id: number): Promise<void> {
  return api.delete(`/mail/receipts/${id}`)
}

export function fetchGuard(): Promise<{ ips: MailIPStat[] }> {
  return api.get('/mail/guard?limit=100')
}

export function unblockIP(ip: string): Promise<{ ok: boolean }> {
  return api.post(`/mail/guard/${encodeURIComponent(ip)}/unblock`, {})
}

export function saveMailSettings(body: { notify?: boolean }): Promise<{ ok: boolean }> {
  return api.put('/mail/settings', body)
}

/**
 * Вложение отдаётся API с проверкой владельца, поэтому просто ссылкой его не
 * скачать — нужен заголовок авторизации. Тянем в память и отдаём как blob.
 */
export async function downloadAttachment(a: MailAttachment): Promise<void> {
  const res = await fetch(`${apiBase()}/mail/attachments/${a.id}`, {
    headers: { Authorization: apiAuthHeader() },
  })
  if (!res.ok) throw new Error('не удалось скачать')
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = a.filename || 'attachment'
  link.click()
  URL.revokeObjectURL(url)
}
