package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// AccessToken — метаданные токена доступа. Самого токена здесь нет: в БД
// лежит только sha256-хэш, наружу отдаётся prefix для опознания.
type AccessToken struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	LastDevice string     `json:"last_device"`
	CreatedAt  time.Time  `json:"created_at"`
}

// maxAccessTokens — предел активных токенов на пользователя.
const maxAccessTokens = 10

const accessTokenCols = `id, name, prefix, expires_at, last_used_at, last_device, created_at`

// ListAccessTokens — активные (не отозванные) токены пользователя.
func (s *Store) ListAccessTokens(ctx context.Context, userID int64) ([]AccessToken, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+accessTokenCols+` FROM access_tokens
		WHERE user_id = $1 AND revoked_at IS NULL
		ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[AccessToken])
}

// ErrTooManyTokens — превышен предел активных токенов.
var ErrTooManyTokens = errors.New("too many access tokens")

// CreateAccessToken сохраняет хэш нового токена (сам токен генерирует и
// показывает пользователю обработчик — второй раз его увидеть нельзя).
func (s *Store) CreateAccessToken(ctx context.Context, userID int64, name, hash, prefix string, expiresAt *time.Time) (AccessToken, error) {
	var n int64
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM access_tokens
		WHERE user_id = $1 AND revoked_at IS NULL`, userID).Scan(&n); err != nil {
		return AccessToken{}, err
	}
	if n >= maxAccessTokens {
		return AccessToken{}, ErrTooManyTokens
	}
	rows, err := s.pool.Query(ctx, `
		INSERT INTO access_tokens (user_id, name, token_hash, prefix, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+accessTokenCols,
		userID, name, hash, prefix, expiresAt)
	if err != nil {
		return AccessToken{}, err
	}
	return pgx.CollectOneRow(rows, pgx.RowToStructByPos[AccessToken])
}

// RevokeAccessToken помечает токен отозванным (строку храним для истории).
func (s *Store) RevokeAccessToken(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE access_tokens SET revoked_at = now()
		WHERE id = $2 AND user_id = $1 AND revoked_at IS NULL`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AccessTokenOwner ищет владельца по хэшу токена: только активные и не
// истёкшие. Возвращает данные пользователя, чтобы TouchUser не затёр
// username/first_name пустыми значениями. Сигнатура на примитивах —
// интерфейс auth.TokenStore реализуется без импорта store.
func (s *Store) AccessTokenOwner(ctx context.Context, hash string) (userID, tokenID int64, username, firstName string, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT t.user_id, t.id, COALESCE(u.username, ''), COALESCE(u.first_name, '')
		FROM access_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = $1 AND t.revoked_at IS NULL
		  AND (t.expires_at IS NULL OR t.expires_at > now())`, hash).
		Scan(&userID, &tokenID, &username, &firstName)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, "", "", ErrNotFound
	}
	return userID, tokenID, username, firstName, err
}

// TouchAccessToken отмечает факт использования токена (для списка в UI).
func (s *Store) TouchAccessToken(ctx context.Context, tokenID int64, device string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE access_tokens SET last_used_at = now(), last_device = $2 WHERE id = $1`,
		tokenID, device)
	return err
}
