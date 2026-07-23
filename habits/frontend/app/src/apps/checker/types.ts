export interface CheckItem {
  id: number
  name: string
  done: boolean
  position: number
}

export interface CheckGroup {
  id: number
  parent_id: number | null
  name: string
  position: number
  hide_done: boolean
  items: CheckItem[]
}
