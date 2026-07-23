package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type BackgroundSettings struct {
	Kind     string // none | file | url
	Value    string // имя файла или URL
	Position string // cover | repeat | center
	Blur     int32  // px, 0-30
	Dim      int32  // -70 (темнее) .. 70 (светлее)
	// свой цвет текста интерфейса ('' — цвет темы по умолчанию)
	TextDark  string
	TextLight string
	// карточки-«стекло»: непрозрачность 0-100 (100 — сплошной) и размытие 0-30
	CardOpacity int32
	CardBlur    int32
}

type BackgroundImage struct {
	ID       int64
	Filename string
}

func (s *Store) GetBackground(ctx context.Context, userID int64) (BackgroundSettings, []BackgroundImage, error) {
	bg := BackgroundSettings{Kind: "none", Position: "cover", CardOpacity: 100}
	err := s.pool.QueryRow(ctx, `
		SELECT bg_kind, bg_value, bg_position, bg_blur, bg_dim, text_color_dark, text_color_light, card_opacity, card_blur
		FROM user_settings WHERE user_id = $1`,
		userID).Scan(&bg.Kind, &bg.Value, &bg.Position, &bg.Blur, &bg.Dim, &bg.TextDark, &bg.TextLight, &bg.CardOpacity, &bg.CardBlur)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return bg, nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, filename FROM user_backgrounds WHERE user_id = $1 ORDER BY id DESC`, userID)
	if err != nil {
		return bg, nil, err
	}
	images, err := pgx.CollectRows(rows, pgx.RowToStructByPos[BackgroundImage])
	return bg, images, err
}

func (s *Store) SetBackground(ctx context.Context, userID int64, bg BackgroundSettings) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, bg_kind, bg_value, bg_position, bg_blur, bg_dim, text_color_dark, text_color_light, card_opacity, card_blur)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (user_id) DO UPDATE
		SET bg_kind = EXCLUDED.bg_kind,
		    bg_value = EXCLUDED.bg_value,
		    bg_position = EXCLUDED.bg_position,
		    bg_blur = EXCLUDED.bg_blur,
		    bg_dim = EXCLUDED.bg_dim,
		    text_color_dark = EXCLUDED.text_color_dark,
		    text_color_light = EXCLUDED.text_color_light,
		    card_opacity = EXCLUDED.card_opacity,
		    card_blur = EXCLUDED.card_blur,
		    updated_at = now()`,
		userID, bg.Kind, bg.Value, bg.Position, bg.Blur, bg.Dim, bg.TextDark, bg.TextLight, bg.CardOpacity, bg.CardBlur)
	return err
}

func (s *Store) AddBackgroundImage(ctx context.Context, userID int64, filename string) (BackgroundImage, error) {
	img := BackgroundImage{Filename: filename}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO user_backgrounds (user_id, filename)
		VALUES ($1, $2) RETURNING id`, userID, filename).Scan(&img.ID)
	return img, err
}

// BackgroundImageFilename возвращает имя файла картинки пользователя.
func (s *Store) BackgroundImageFilename(ctx context.Context, userID, id int64) (string, error) {
	var filename string
	err := s.pool.QueryRow(ctx, `
		SELECT filename FROM user_backgrounds WHERE id = $1 AND user_id = $2`,
		id, userID).Scan(&filename)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return filename, err
}

// DeleteBackgroundImage удаляет запись; если картинка была текущим фоном,
// сбрасывает фон. Возвращает имя файла для удаления с диска.
func (s *Store) DeleteBackgroundImage(ctx context.Context, userID, id int64) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var filename string
	err = tx.QueryRow(ctx, `
		DELETE FROM user_backgrounds WHERE id = $1 AND user_id = $2 RETURNING filename`,
		id, userID).Scan(&filename)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE user_settings SET bg_kind = 'none', bg_value = '', updated_at = now()
		WHERE user_id = $1 AND bg_kind = 'file' AND bg_value = $2`,
		userID, filename); err != nil {
		return "", err
	}
	return filename, tx.Commit(ctx)
}

// GetLinksStorage возвращает выбранное хранилище Links ('' — не выбрано).
func (s *Store) GetLinksStorage(ctx context.Context, userID int64) (string, error) {
	var v string
	err := s.pool.QueryRow(ctx, `
		SELECT links_storage FROM user_settings WHERE user_id = $1`, userID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Store) SetLinksStorage(ctx context.Context, userID int64, v string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, links_storage) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET links_storage = EXCLUDED.links_storage, updated_at = now()`,
		userID, v)
	return err
}

// GetCollapsed возвращает JSON свёрнутых групп: {"checker":[ids],"tracker":[ids]}.
func (s *Store) GetCollapsed(ctx context.Context, userID int64) ([]byte, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT ui_collapsed FROM user_settings WHERE user_id = $1`, userID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return []byte(`{}`), nil
	}
	return raw, err
}

// SetCollapsedApp заменяет список свёрнутых id для одного приложения.
func (s *Store) SetCollapsedApp(ctx context.Context, userID int64, app string, ids []int64) error {
	if ids == nil {
		ids = []int64{}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, ui_collapsed)
		VALUES ($1, jsonb_build_object($2::text, to_jsonb($3::bigint[])))
		ON CONFLICT (user_id) DO UPDATE
		SET ui_collapsed = user_settings.ui_collapsed || jsonb_build_object($2::text, to_jsonb($3::bigint[])),
		    updated_at = now()`,
		userID, app, ids)
	return err
}
