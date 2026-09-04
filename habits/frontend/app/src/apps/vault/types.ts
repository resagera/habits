export interface VaultFolder {
  id: number
  parent_id: number | null
  name: string
  hint: string
  thumbs: boolean
  /** косметика: не показывать подпапки, пока папка не открыта паролем */
  hide_children: boolean
  /** 0 — файлы живут вечно; иначе загруженным ставится срок */
  auto_delete_days: number
  kdf_salt: string
  kdf_iter: number
  wrapped_key: string
  wrap_iv: string
  position: number
  owner_id: number
  mine: boolean
  shared: boolean
  owner_name?: string
}

export interface VaultFile {
  id: number
  folder_id: number
  size_bytes: number
  plain_size: number
  key_env: string
  meta_env: string
  chunk_size: number
  has_thumb: boolean
  created_at: string
  expires_at: string | null
  owner_id: number
  mine: boolean
  shared: boolean
}

/** Расшифрованные метаданные файла: на сервере лежат внутри meta_env. */
export interface FileMeta {
  name: string
  type: string
  size: number
  /** заметка владельца: лежит внутри meta_env, то есть тоже зашифрована */
  note?: string
}

/** Временная ссылка на один файл (токен виден только при создании). */
export interface VaultLink {
  id: number
  file_id: number
  kdf_salt: string
  kdf_iter: number
  key_env: string
  meta_env: string
  expires_at: string
  max_views: number
  views: number
  created_at: string
}

export interface AccessEntry {
  user_id: number | null
  user_name: string
  via: 'share' | 'link'
  at: string
}

export interface VaultQuota {
  used: number
  total_limit: number
  file_limit: number
}

export interface ShareUser {
  id: number
  username: string
  first_name: string
}
