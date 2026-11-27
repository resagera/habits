export interface TerminalMachine {
  id: number
  name: string
  token: string
  last_seen_at?: string
  created_at: string
  online: boolean
}
