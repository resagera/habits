import { api, apiAuthHeader, apiBase } from '../../shared/api/client'
import type { Contact, ContactPhoto, IncomingShare, Suggestion } from './types'

export function fetchContacts() {
  return api.get<{ contacts: Contact[]; suggestions: Suggestion[] }>('/contacts')
}

export function addContact(to: string) {
  return api.post<{ contact: Contact }>('/contacts', { to })
}

export function updateContact(id: number, patch: { note?: string; auto_accept?: boolean }) {
  return api.patch<{ contact: Contact }>(`/contacts/${id}`, patch)
}

export function deleteContact(id: number) {
  return api.delete<void>(`/contacts/${id}`)
}

/** Загрузка фото в галерею (multipart) — api-клиент шлёт JSON, поэтому raw fetch. */
export async function uploadPhoto(id: number, file: File): Promise<ContactPhoto> {
  const form = new FormData()
  form.append('file', file)
  const res = await fetch(`${apiBase()}/contacts/${id}/photos`, {
    method: 'POST',
    headers: { Authorization: apiAuthHeader() },
    body: form,
  })
  if (!res.ok) throw new Error(`upload failed: ${res.status}`)
  return ((await res.json()) as { photo: ContactPhoto }).photo
}

export function deletePhoto(id: number, photoId: number) {
  return api.delete<void>(`/contacts/${id}/photos/${photoId}`)
}

export function fetchIncoming() {
  return api.get<{ shares: IncomingShare[] }>('/contacts/incoming')
}

export function acceptShare(id: number) {
  return api.post<{ name: string; kind: string }>(`/contacts/incoming/${id}/accept`)
}

export function declineShare(id: number) {
  return api.post<void>(`/contacts/incoming/${id}/decline`)
}
