package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// AIMachine — домашняя машина с coding-агентом (habits-ai-agent).
type AIMachine struct {
	ID         int64           `json:"id"`
	Name       string          `json:"name"`
	Token      string          `json:"token"`
	Dirs       []string        `json:"dirs"`
	Tools      json.RawMessage `json:"tools"`
	LastSeenAt *time.Time      `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

const aiMachineCols = `id, name, token, dirs, tools, last_seen_at, created_at`

func scanAIMachine(row pgx.Row) (AIMachine, error) {
	var m AIMachine
	var dirs []byte
	if err := row.Scan(&m.ID, &m.Name, &m.Token, &dirs, &m.Tools, &m.LastSeenAt, &m.CreatedAt); err != nil {
		return m, err
	}
	m.Dirs = []string{}
	_ = json.Unmarshal(dirs, &m.Dirs)
	if len(m.Tools) == 0 {
		m.Tools = json.RawMessage(`{}`)
	}
	return m, nil
}

func (s *Store) ListAIMachines(ctx context.Context, userID int64) ([]AIMachine, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+aiMachineCols+` FROM ai_machines
		WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []AIMachine
	for rows.Next() {
		m, err := scanAIMachine(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

func (s *Store) CreateAIMachine(ctx context.Context, userID int64, name, token string) (AIMachine, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO ai_machines (user_id, name, token) VALUES ($1, $2, $3)
		RETURNING `+aiMachineCols, userID, name, token)
	return scanAIMachine(row)
}

func (s *Store) RenameAIMachine(ctx context.Context, userID, id int64, name string) (AIMachine, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE ai_machines SET name = $3 WHERE id = $2 AND user_id = $1
		RETURNING `+aiMachineCols, userID, id, name)
	m, err := scanAIMachine(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

func (s *Store) DeleteAIMachine(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM ai_machines WHERE id = $2 AND user_id = $1`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AIMachineOwned(ctx context.Context, userID, id int64) (AIMachine, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+aiMachineCols+` FROM ai_machines WHERE id = $2 AND user_id = $1`, userID, id)
	m, err := scanAIMachine(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// AIMachineByToken — авторизация агента по его токену.
func (s *Store) AIMachineByToken(ctx context.Context, token string) (AIMachine, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+aiMachineCols+` FROM ai_machines WHERE token = $1`, token)
	m, err := scanAIMachine(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// TouchAIMachine — hello агента: список разрешённых папок + last_seen.
func (s *Store) TouchAIMachine(ctx context.Context, id int64, dirs []string) error {
	if dirs == nil {
		dirs = []string{}
	}
	data, err := json.Marshal(dirs)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE ai_machines SET dirs = $2, last_seen_at = now() WHERE id = $1`, id, data)
	return err
}

// SetAIMachineTool сохраняет результат проверки инструмента (claude/codex).
func (s *Store) SetAIMachineTool(ctx context.Context, id int64, tool string, result json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE ai_machines SET tools = jsonb_set(tools, ARRAY[$2], $3::jsonb, true)
		WHERE id = $1`, id, tool, result)
	return err
}

// --- задачи и прогоны ---

type AITask struct {
	ID        int64     `json:"id"`
	MachineID int64     `json:"machine_id"`
	Tool      string    `json:"tool"`
	Workdir   string    `json:"workdir"`
	Model     string    `json:"model"`
	Params    string    `json:"params"`
	Title     string    `json:"title"`
	SessionID string    `json:"session_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// сводка по прогонам (заполняется в ListAITasks)
	RunCount   int64  `json:"run_count"`
	LastStatus string `json:"last_status"`
}

type AIRun struct {
	ID         int64           `json:"id"`
	TaskID     int64           `json:"task_id"`
	Prompt     string          `json:"prompt"`
	Status     string          `json:"status"`
	Output     string          `json:"output"`
	Error      string          `json:"error"`
	Meta       json.RawMessage `json:"meta"`
	Mode       string          `json:"mode"` // '' | 'plan'
	Log        string          `json:"log"`  // живой ход выполнения
	StartedAt  *time.Time      `json:"started_at,omitempty"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

const aiTaskCols = `id, machine_id, tool, workdir, model, params, title, session_id, created_at, updated_at`

// CreateAITask создаёт задачу; sessionID не пуст, если задача продолжает уже
// существующую сессию CLI на машине (первый прогон уйдёт с --resume).
func (s *Store) CreateAITask(ctx context.Context, userID, machineID int64, tool, workdir, model, params, title, sessionID string) (AITask, error) {
	var t AITask
	err := s.pool.QueryRow(ctx, `
		INSERT INTO ai_tasks (user_id, machine_id, tool, workdir, model, params, title, session_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+aiTaskCols,
		userID, machineID, tool, workdir, model, params, title, sessionID).Scan(
		&t.ID, &t.MachineID, &t.Tool, &t.Workdir, &t.Model, &t.Params, &t.Title, &t.SessionID, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

// ListAITasks — задачи пользователя (по машине, если machineID > 0) со сводкой
// прогонов: количество и статус последнего.
func (s *Store) ListAITasks(ctx context.Context, userID, machineID int64) ([]AITask, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.machine_id, t.tool, t.workdir, t.model, t.params, t.title, t.session_id,
		       t.created_at, t.updated_at,
		       count(r.id),
		       COALESCE((SELECT r2.status FROM ai_runs r2 WHERE r2.task_id = t.id ORDER BY r2.id DESC LIMIT 1), '')
		FROM ai_tasks t
		LEFT JOIN ai_runs r ON r.task_id = t.id
		WHERE t.user_id = $1 AND ($2 = 0 OR t.machine_id = $2)
		GROUP BY t.id
		ORDER BY t.updated_at DESC, t.id DESC
		LIMIT 200`, userID, machineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []AITask
	for rows.Next() {
		var t AITask
		if err := rows.Scan(&t.ID, &t.MachineID, &t.Tool, &t.Workdir, &t.Model, &t.Params, &t.Title,
			&t.SessionID, &t.CreatedAt, &t.UpdatedAt, &t.RunCount, &t.LastStatus); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

func (s *Store) AITaskOwned(ctx context.Context, userID, id int64) (AITask, error) {
	var t AITask
	err := s.pool.QueryRow(ctx, `
		SELECT `+aiTaskCols+` FROM ai_tasks WHERE id = $2 AND user_id = $1`, userID, id).Scan(
		&t.ID, &t.MachineID, &t.Tool, &t.Workdir, &t.Model, &t.Params, &t.Title, &t.SessionID, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

func (s *Store) DeleteAITask(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM ai_tasks WHERE id = $2 AND user_id = $1`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const aiRunCols = `id, task_id, prompt, status, output, error, meta, mode, log, started_at, finished_at, created_at`

func scanAIRun(row pgx.Row) (AIRun, error) {
	var r AIRun
	err := row.Scan(&r.ID, &r.TaskID, &r.Prompt, &r.Status, &r.Output, &r.Error, &r.Meta,
		&r.Mode, &r.Log, &r.StartedAt, &r.FinishedAt, &r.CreatedAt)
	if len(r.Meta) == 0 {
		r.Meta = json.RawMessage(`{}`)
	}
	return r, err
}

func (s *Store) CreateAIRun(ctx context.Context, taskID int64, prompt, mode string) (AIRun, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO ai_runs (task_id, prompt, mode) VALUES ($1, $2, $3)
		RETURNING `+aiRunCols, taskID, prompt, mode)
	if _, err := s.pool.Exec(ctx, `UPDATE ai_tasks SET updated_at = now() WHERE id = $1`, taskID); err != nil {
		return AIRun{}, err
	}
	return scanAIRun(row)
}

// AppendAIRunLog — дозапись строки живого лога (кап 256КБ, только активные).
func (s *Store) AppendAIRunLog(ctx context.Context, runID int64, line string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE ai_runs SET log = log || $2
		WHERE id = $1 AND status IN ('queued','running') AND length(log) < 262144`,
		runID, line+"\n")
	return err
}

func (s *Store) ListAIRuns(ctx context.Context, taskID int64) ([]AIRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+aiRunCols+` FROM ai_runs WHERE task_id = $1 ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []AIRun
	for rows.Next() {
		r, err := scanAIRun(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// HasActiveAIRun — есть ли у задачи прогон в очереди или выполнении.
func (s *Store) HasActiveAIRun(ctx context.Context, taskID int64) (bool, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM ai_runs WHERE task_id = $1 AND status IN ('queued','running')`, taskID).Scan(&n)
	return n > 0, err
}

func (s *Store) MarkAIRunRunning(ctx context.Context, runID int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE ai_runs SET status = 'running', started_at = COALESCE(started_at, now())
		WHERE id = $1 AND status = 'queued'`, runID)
	return err
}

// FinishAIRun сохраняет результат прогона и session_id задачи (для --resume).
// Повторная доставка результата (уже done/error/canceled) — no-op с
// updated=false, чтобы не дублировать уведомления.
func (s *Store) FinishAIRun(ctx context.Context, runID int64, ok bool, output, errText string, meta json.RawMessage, sessionID string) (bool, error) {
	status := "done"
	if !ok {
		status = "error"
	}
	if len(meta) == 0 {
		meta = json.RawMessage(`{}`)
	}
	var taskID int64
	err := s.pool.QueryRow(ctx, `
		UPDATE ai_runs SET status = $2, output = $3, error = $4, meta = $5, finished_at = now()
		WHERE id = $1 AND status IN ('queued','running')
		RETURNING task_id`, runID, status, output, errText, meta).Scan(&taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // повторная доставка — уже сохранено
	}
	if err != nil {
		return false, err
	}
	if sessionID != "" {
		_, err = s.pool.Exec(ctx, `
			UPDATE ai_tasks SET session_id = $2, updated_at = now() WHERE id = $1`, taskID, sessionID)
	} else {
		_, err = s.pool.Exec(ctx, `UPDATE ai_tasks SET updated_at = now() WHERE id = $1`, taskID)
	}
	return true, err
}

// AIRunNotifyInfo — данные для уведомления о завершении прогона.
type AIRunNotifyInfo struct {
	UserID    int64
	TaskID    int64
	TaskTitle string
	Tool      string
}

func (s *Store) AIRunNotify(ctx context.Context, runID int64) (AIRunNotifyInfo, error) {
	var n AIRunNotifyInfo
	err := s.pool.QueryRow(ctx, `
		SELECT t.user_id, t.id, t.title, t.tool
		FROM ai_runs r JOIN ai_tasks t ON t.id = r.task_id
		WHERE r.id = $1`, runID).Scan(&n.UserID, &n.TaskID, &n.TaskTitle, &n.Tool)
	if errors.Is(err, pgx.ErrNoRows) {
		return n, ErrNotFound
	}
	return n, err
}

// QueuedAIRun — прогон в очереди с данными задачи для отправки агенту.
type QueuedAIRun struct {
	RunID     int64
	Prompt    string
	Mode      string
	Tool      string
	Workdir   string
	Model     string
	Params    string
	SessionID string
}

// QueuedAIRunsForMachine — очередь машины (агент был офлайн или перезапущен).
func (s *Store) QueuedAIRunsForMachine(ctx context.Context, machineID int64) ([]QueuedAIRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.prompt, r.mode, t.tool, t.workdir, t.model, t.params, t.session_id
		FROM ai_runs r JOIN ai_tasks t ON t.id = r.task_id
		WHERE t.machine_id = $1 AND r.status = 'queued'
		ORDER BY r.id`, machineID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[QueuedAIRun])
}

// ActiveAIRunCounts — занятость машин пользователя (queued+running прогоны).
func (s *Store) ActiveAIRunCounts(ctx context.Context, userID int64) (map[int64]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.machine_id, count(*)
		FROM ai_runs r JOIN ai_tasks t ON t.id = r.task_id
		WHERE t.user_id = $1 AND r.status IN ('queued','running')
		GROUP BY t.machine_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := map[int64]int64{}
	for rows.Next() {
		var id, n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		res[id] = n
	}
	return res, rows.Err()
}

// AIRunOwned — прогон с проверкой владения (через задачу); возвращает также
// machine_id для отправки cancel агенту.
func (s *Store) AIRunOwned(ctx context.Context, userID, runID int64) (status string, machineID int64, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT r.status, t.machine_id
		FROM ai_runs r JOIN ai_tasks t ON t.id = r.task_id
		WHERE r.id = $1 AND t.user_id = $2`, runID, userID).Scan(&status, &machineID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, ErrNotFound
	}
	return
}

// FailStaleAIRuns — прогоны, зависшие в running дольше max: агент умер
// посреди выполнения и результат не придёт. Вызывается при листинге.
// queued не трогаем — это очередь до подключения агента (отменяется вручную).
func (s *Store) FailStaleAIRuns(ctx context.Context, maxAge time.Duration) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE ai_runs SET status = 'error', error = 'timeout: агент не вернул результат', finished_at = now()
		WHERE status = 'running' AND started_at < now() - $1::interval`,
		maxAge.String())
	return err
}

// FailOrphanedAIRuns закрывает прогоны машины, числящиеся running, которых
// нет среди живых у подключившегося агента: процесс агента перезапустился или
// упал вместе с дочерним CLI, результат уже не придёт. Возвращает id закрытых
// (для уведомления владельца).
func (s *Store) FailOrphanedAIRuns(ctx context.Context, machineID int64, active []int64) ([]int64, error) {
	if active == nil {
		active = []int64{}
	}
	rows, err := s.pool.Query(ctx, `
		UPDATE ai_runs r
		SET status = 'error',
		    error = 'прогон прерван: агент был перезапущен или остановлен',
		    finished_at = now()
		FROM ai_tasks t
		WHERE t.id = r.task_id AND t.machine_id = $1
		  AND r.status = 'running' AND NOT (r.id = ANY($2::bigint[]))
		RETURNING r.id`, machineID, active)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// CancelQueuedAIRun — отмена прогона, который ещё в очереди (агент офлайн);
// running отменяет агент (kill) и присылает результат сам.
func (s *Store) CancelQueuedAIRun(ctx context.Context, runID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE ai_runs SET status = 'error', error = 'остановлено пользователем (из очереди)', finished_at = now()
		WHERE id = $1 AND status = 'queued'`, runID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// AIRunResult — статус и вывод прогона владельца. Нужен разбору чеков: он
// просит агента предложить группы товаров и потом читает ответ.
func (s *Store) AIRunResult(ctx context.Context, userID, runID int64) (AIRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.task_id, r.prompt, r.status, r.output, r.error, r.meta,
		       r.mode, r.log, r.started_at, r.finished_at, r.created_at
		FROM ai_runs r
		JOIN ai_tasks t ON t.id = r.task_id
		WHERE t.user_id = $1 AND r.id = $2`, userID, runID)
	if err != nil {
		return AIRun{}, err
	}
	run, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[AIRun])
	if errors.Is(err, pgx.ErrNoRows) {
		return run, ErrNotFound
	}
	return run, err
}
