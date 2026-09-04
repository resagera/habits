export interface CheckItem {
  id: number
  name: string
  done: boolean
  position: number
  note: string
  label: string
  /** взят в работу — необязательная отметка между несделанным и сделанным */
  in_progress: boolean
  remind_at: string | null
}

export interface CheckGroup {
  id: number
  parent_id: number | null
  name: string
  position: number
  hide_done: boolean
  /** у пунктов группы есть промежуточный статус «в работе» (клик в два такта) */
  progress_mode: boolean
  mine: boolean
  shared: boolean
  owner_name?: string
  reset_period: string // none/daily/weekly/monthly
  reset_minute: number
  reset_dow: number
  reset_dom: number
  reset_tz_off: number
  remind_at: string | null
  items: CheckItem[]
}
