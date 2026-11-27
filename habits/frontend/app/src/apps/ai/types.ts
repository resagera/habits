export interface ToolStatus {
  installed: boolean
  authorized: boolean
  version: string
  path?: string // где на машине нашли бинарник
  error?: string
  checked_at: number
}

/** Папка, разрешённая машине ИЗ ПРИЛОЖЕНИЯ (dirs — те, что в конфиге агента). */
export interface AIMachineDir {
  id: number
  machine_id: number
  path: string
  created_at: string
}

export interface AIMachine {
  id: number
  name: string
  token: string
  /** белый список из конфига агента (AI_AGENT_DIRS, приходит в hello) */
  dirs: string[]
  /** папки, разрешённые на этой странице (сервер присылает их агенту) */
  extra_dirs: AIMachineDir[]
  /** версия агента из последнего hello ('' — ещё не подключался) */
  agent_version: string
  tools: Partial<Record<'claude' | 'codex', ToolStatus>>
  last_seen_at?: string
  created_at: string
  online: boolean
  /** активные прогоны (queued + running) — занятость машины */
  busy: number
}

export interface AISchedule {
  id: number
  machine_id: number
  tool: string
  workdir: string
  model: string
  params: string
  prompt: string
  period: 'daily' | 'weekly' | 'hours'
  at_minute: number
  dow: number
  every_hours: number
  tz_off: number
  enabled: boolean
  next_run_at?: string
  last_task_id?: number
  created_at: string
}

export interface AITask {
  id: number
  machine_id: number
  tool: string
  workdir: string
  model: string
  params: string
  title: string
  /** сессия, в которой идёт задача (у форка — НОВАЯ, пришедшая с прогоном) */
  session_id: string
  /** сессия, от которой задача отделилась («форк»); '' — обычная задача */
  forked_from: string
  created_at: string
  updated_at: string
  run_count: number
  last_status: string
}

export interface AIRunMeta {
  cost_usd?: number
  num_turns?: number
  elapsed_sec?: number
  branch?: string
  diffstat?: string
  // codex: токены вместо стоимости (подписка)
  tokens_in?: number
  tokens_out?: number
  /** codex-форк с --worktree: где он на самом деле работал */
  worktree_dir?: string
}

export interface AIRun {
  id: number
  task_id: number
  prompt: string
  status: 'queued' | 'running' | 'done' | 'error'
  output: string
  error: string
  meta: AIRunMeta
  /** '' — обычный прогон, 'plan' — только план (без правок) */
  mode: string
  /** прогон отделил новую сессию от старой (--fork-session / exec fork) */
  fork: boolean
  /** codex выполнял форк в отдельном git-worktree */
  worktree: boolean
  /** живой ход выполнения (строки-события) */
  log: string
  started_at?: string
  finished_at?: string
  created_at: string
}

export interface UsageWindow {
  label: string
  percent: number
  resets_at?: string
}

export interface UsageInfo {
  plan?: string
  windows: UsageWindow[]
  note?: string
  error?: string
  fetched_at: number
}

/** Прошлая сессия CLI-инструмента на машине (из индекса агента). */
export interface CLISession {
  id: string
  tool: string
  title: string
  workdir: string
  branch?: string
  started_at?: string
  updated_at?: string
  /** сообщений пользователя */
  msgs: number
  /** размер файла сессии */
  bytes: number
}

/** Сессия, добавленная вручную по id (живёт на сервере, не в индексе агента). */
export interface AISessionRef {
  id: number
  machine_id: number
  tool: string
  session_id: string
  workdir: string
  title: string
  created_at: string
}

/** Ответ поиска сессии по id: карточка + разрешена ли её папка. */
export interface FoundSession {
  found: boolean
  allowed: boolean
  meta: CLISession
  error?: string
}

export interface CLISessionMsg {
  role: 'user' | 'assistant' | 'tool'
  text: string
  ts?: string
}

export interface CLISessionDetail {
  meta: CLISession
  messages: CLISessionMsg[]
  /** начало обрезано — показан хвост сессии */
  truncated: boolean
  error?: string
}

export const RUN_STATUS_LABELS: Record<string, string> = {
  queued: 'в очереди',
  running: 'выполняется',
  done: 'готово',
  error: 'ошибка',
}
