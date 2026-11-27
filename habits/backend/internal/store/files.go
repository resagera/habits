package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// FileRoot — папка, доступная через файлового агента.
type FileRoot struct {
	Path string `json:"path"`
	Mode string `json:"mode"` // ro | rw
}

// FileMachine — домашняя машина с файловым агентом.
type FileMachine struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Token      string     `json:"token"`
	Roots      []FileRoot `json:"roots"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func scanFileMachine(row pgx.Row) (FileMachine, error) {
	var m FileMachine
	var roots []byte
	if err := row.Scan(&m.ID, &m.Name, &m.Token, &roots, &m.LastSeenAt, &m.CreatedAt); err != nil {
		return m, err
	}
	m.Roots = []FileRoot{}
	_ = json.Unmarshal(roots, &m.Roots)
	return m, nil
}

const fileMachineCols = `id, name, token, roots, last_seen_at, created_at`

func (s *Store) ListFileMachines(ctx context.Context, userID int64) ([]FileMachine, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+fileMachineCols+` FROM file_machines
		WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []FileMachine
	for rows.Next() {
		m, err := scanFileMachine(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

func (s *Store) CreateFileMachine(ctx context.Context, userID int64, name, token string) (FileMachine, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO file_machines (user_id, name, token) VALUES ($1, $2, $3)
		RETURNING `+fileMachineCols, userID, name, token)
	return scanFileMachine(row)
}

func (s *Store) RenameFileMachine(ctx context.Context, userID, id int64, name string) (FileMachine, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE file_machines SET name = $3 WHERE id = $2 AND user_id = $1
		RETURNING `+fileMachineCols, userID, id, name)
	m, err := scanFileMachine(row)
	if err == pgx.ErrNoRows {
		return m, ErrNotFound
	}
	return m, err
}

func (s *Store) DeleteFileMachine(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM file_machines WHERE id = $2 AND user_id = $1`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// FileMachineByToken — авторизация агента по его токену.
func (s *Store) FileMachineByToken(ctx context.Context, token string) (FileMachine, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+fileMachineCols+` FROM file_machines WHERE token = $1`, token)
	m, err := scanFileMachine(row)
	if err == pgx.ErrNoRows {
		return m, ErrNotFound
	}
	return m, err
}

// FileMachineOwned — машина с проверкой владения (для операций из UI).
func (s *Store) FileMachineOwned(ctx context.Context, userID, id int64) (FileMachine, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+fileMachineCols+` FROM file_machines WHERE id = $2 AND user_id = $1`, userID, id)
	m, err := scanFileMachine(row)
	if err == pgx.ErrNoRows {
		return m, ErrNotFound
	}
	return m, err
}

// TouchFileMachine сохраняет присланные агентом корни и время подключения.
func (s *Store) TouchFileMachine(ctx context.Context, id int64, roots []FileRoot) error {
	data, err := json.Marshal(roots)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE file_machines SET roots = $2, last_seen_at = now() WHERE id = $1`, id, data)
	return err
}
