import { api } from '../../shared/api/client'

export type UserType = 'regular' | 'vip' | 'payed1' | 'payed2'

export const USER_TYPE_LABELS: Record<UserType, string> = {
  regular: 'Обычный',
  vip: 'VIP',
  payed1: 'Payed 1',
  payed2: 'Payed 2',
}

export interface AdminUser {
  id: number
  username: string
  first_name: string
  created_at: string
  last_seen_at: string
  banned: boolean
  user_type: UserType
  last_ip: string
  last_device: string
}

export interface UserDevice {
  ip: string
  device: string
  created_at: string
}

export interface AdminUserDetail {
  user: AdminUser
  devices: UserDevice[]
  data: Record<string, number>
  is_admin: boolean
}

export function fetchUsers(limit = 200) {
  return api.get<{ users: AdminUser[]; total: number }>(`/admin/users?limit=${limit}`)
}

export function fetchUser(id: number) {
  return api.get<AdminUserDetail>(`/admin/users/${id}`)
}

export function setUserBanned(id: number, banned: boolean) {
  return api.post<{ banned: boolean }>(`/admin/users/${id}/ban`, { banned })
}

export function setUserType(id: number, type: UserType) {
  return api.post<{ type: UserType }>(`/admin/users/${id}/type`, { type })
}

// --- лимиты типов (страница Projects) ---

export interface TypeLimits {
  type: UserType
  max_blocks: number
  max_images: number
  max_files: number
  max_image_mb: number
  max_file_mb: number
}

export function fetchLimits() {
  return api.get<{ limits: TypeLimits[] }>('/admin/limits')
}

export function updateLimits(l: TypeLimits) {
  return api.put<{ limits: TypeLimits }>(`/admin/limits/${l.type}`, l)
}
