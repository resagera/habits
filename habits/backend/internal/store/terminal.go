package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// TerminalMachine — домашняя машина с shell-агентом (страница Terminal).
type TerminalMachine struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Token      string     `json:"token"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

const terminalMachineCols = `id, name, token, last_seen_at, created_at`

func scanTerminalMachine(row pgx.Row) (TerminalMachine, error) {
	var m TerminalMachine
	err := row.Scan(&m.ID, &m.Name, &m.Token, &m.LastSeenAt, &m.CreatedAt)
	return m, err
}

func (s *Store) ListTerminalMachines(ctx context.Context, userID int64) ([]TerminalMachine, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+terminalMachineCols+` FROM terminal_machines
		WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []TerminalMachine
	for rows.Next() {
		m, err := scanTerminalMachine(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

func (s *Store) CreateTerminalMachine(ctx context.Context, userID int64, name, token string) (TerminalMachine, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO terminal_machines (user_id, name, token) VALUES ($1, $2, $3)
		RETURNING `+terminalMachineCols, userID, name, token)
	return scanTerminalMachine(row)
}

func (s *Store) RenameTerminalMachine(ctx context.Context, userID, id int64, name string) (TerminalMachine, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE terminal_machines SET name = $3 WHERE id = $2 AND user_id = $1
		RETURNING `+terminalMachineCols, userID, id, name)
	m, err := scanTerminalMachine(row)
	if err == pgx.ErrNoRows {
		return m, ErrNotFound
	}
	return m, err
}

func (s *Store) DeleteTerminalMachine(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM terminal_machines WHERE id = $2 AND user_id = $1`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TerminalMachineByToken — авторизация агента по токену.
func (s *Store) TerminalMachineByToken(ctx context.Context, token string) (TerminalMachine, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+terminalMachineCols+` FROM terminal_machines WHERE token = $1`, token)
	m, err := scanTerminalMachine(row)
	if err == pgx.ErrNoRows {
		return m, ErrNotFound
	}
	return m, err
}

// TerminalMachineOwned — машина с проверкой владельца (для открытия сессии).
func (s *Store) TerminalMachineOwned(ctx context.Context, userID, id int64) (TerminalMachine, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+terminalMachineCols+` FROM terminal_machines WHERE id = $2 AND user_id = $1`, userID, id)
	m, err := scanTerminalMachine(row)
	if err == pgx.ErrNoRows {
		return m, ErrNotFound
	}
	return m, err
}

func (s *Store) TouchTerminalMachine(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE terminal_machines SET last_seen_at = now() WHERE id = $1`, id)
	return err
}
