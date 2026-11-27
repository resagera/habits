package store

import "context"

// SaveHelpRequest сохраняет обращение и возвращает его id.
func (s *Store) SaveHelpRequest(ctx context.Context, userID int64, text string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO help_requests (user_id, text) VALUES ($1, $2) RETURNING id`,
		userID, text).Scan(&id)
	return id, err
}
