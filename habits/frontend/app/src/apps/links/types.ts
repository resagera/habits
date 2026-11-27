export interface LinkFolder {
  id: number
  parent_id: number | null
  name: string
  position: number
}

export interface LinkItem {
  id: number
  folder_id: number | null
  name: string
  url: string
  tags: string[]
  pinned: boolean
  position: number
  clicks: number
  /** помечена мёртвой проверкой битых ссылок (опция links.dead_check) */
  dead?: boolean
}

export interface LinksData {
  folders: LinkFolder[]
  links: LinkItem[]
}

export interface NewLink {
  name: string
  url: string
  tags: string[]
  pinned: boolean
  folder_id: number | null
}

export interface LinkPatch {
  name?: string
  url?: string
  tags?: string[]
  pinned?: boolean
  folder_id?: number | null
}

export interface FolderPatch {
  name?: string
  parent_id?: number | null
}
