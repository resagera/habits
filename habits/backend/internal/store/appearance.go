package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// AppearanceState — выбор темы и черновик «своей темы» для одного режима
// входа. Хранится одним JSONB: раньше каждая новая настройка означала новую
// колонку, и их успело набраться восемь при двух темах.
//
// Draft — сырые токены, сервер их не интерпретирует: набор токенов знает
// фронтенд, и добавление нового цвета не должно требовать миграции.
type AppearanceState struct {
	Mode      string          `json:"mode"`       // auto | fixed
	ThemeID   string          `json:"theme_id"`   // id встроенной темы, saved:<id> или draft
	AutoLight string          `json:"auto_light"` // тема для светлого режима системы
	AutoDark  string          `json:"auto_dark"`
	Draft     json.RawMessage `json:"draft,omitempty"`
	// не показывать фоновую картинку — настройка режима входа: в браузере
	// фон часто мешает, в мини-приложении остаётся
	BgOff bool `json:"bg_off"`
}

// UserTheme — сохранённая тема пользователя вместе со снимком фона.
type UserTheme struct {
	ID       int64           `json:"id"`
	Name     string          `json:"name"`
	Kind     string          `json:"kind"`
	Tokens   json.RawMessage `json:"tokens"`
	Bg       json.RawMessage `json:"bg"`
	Position int             `json:"position"`
}

func defaultAppearance() AppearanceState {
	return AppearanceState{Mode: "auto", ThemeID: "night", AutoLight: "day", AutoDark: "night"}
}

// GetAppearanceState возвращает оформление для нужного режима входа.
// token=true — веб/расширение (своё оформление, на Telegram не влияет).
func (s *Store) GetAppearanceState(ctx context.Context, userID int64, token bool) (AppearanceState, error) {
	col := "appearance"
	if token {
		col = "web_appearance"
	}
	var raw []byte
	err := s.pool.QueryRow(ctx,
		`SELECT `+col+` FROM user_settings WHERE user_id = $1`, userID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return defaultAppearance(), nil
	}
	if err != nil {
		return defaultAppearance(), err
	}
	st := defaultAppearance()
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &st); err != nil {
			return defaultAppearance(), nil // битое значение не должно ронять страницу
		}
	}
	if st.Mode == "" {
		st.Mode = "auto"
	}
	if st.ThemeID == "" {
		st.ThemeID = "night"
	}
	if st.AutoLight == "" {
		st.AutoLight = "day"
	}
	if st.AutoDark == "" {
		st.AutoDark = "night"
	}
	return st, nil
}

func (s *Store) SetAppearanceState(ctx context.Context, userID int64, token bool, st AppearanceState) error {
	col := "appearance"
	if token {
		col = "web_appearance"
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, `+col+`) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET `+col+` = EXCLUDED.`+col+`, updated_at = now()`,
		userID, raw)
	return err
}

// --- сохранённые темы ---

func (s *Store) ListUserThemes(ctx context.Context, userID int64) ([]UserTheme, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, kind, tokens, bg, position
		FROM user_themes WHERE user_id = $1 ORDER BY position, id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[UserTheme])
}

// SaveUserTheme создаёт или обновляет тему. Имя уникально в пределах
// пользователя: сохранение под тем же именем перезаписывает — так «Сохранить»
// после правки не плодит копии.
func (s *Store) SaveUserTheme(ctx context.Context, userID int64, t UserTheme) (UserTheme, error) {
	var out UserTheme
	if t.ID > 0 {
		rows, err := s.pool.Query(ctx, `
			UPDATE user_themes SET name = $3, kind = $4, tokens = $5, bg = $6, updated_at = now()
			WHERE user_id = $1 AND id = $2
			RETURNING id, name, kind, tokens, bg, position`,
			userID, t.ID, t.Name, t.Kind, t.Tokens, t.Bg)
		if err != nil {
			return out, err
		}
		out, err = pgx.CollectOneRow(rows, pgx.RowToStructByPos[UserTheme])
		if errors.Is(err, pgx.ErrNoRows) {
			return out, ErrNotFound
		}
		return out, err
	}
	rows, err := s.pool.Query(ctx, `
		INSERT INTO user_themes (user_id, name, kind, tokens, bg, position)
		VALUES ($1, $2, $3, $4, $5,
		        (SELECT COALESCE(MAX(position) + 1, 0) FROM user_themes WHERE user_id = $1))
		ON CONFLICT (user_id, name) DO UPDATE SET
			kind = EXCLUDED.kind, tokens = EXCLUDED.tokens, bg = EXCLUDED.bg, updated_at = now()
		RETURNING id, name, kind, tokens, bg, position`,
		userID, t.Name, t.Kind, t.Tokens, t.Bg)
	if err != nil {
		return out, err
	}
	return pgx.CollectOneRow(rows, pgx.RowToStructByPos[UserTheme])
}

func (s *Store) DeleteUserTheme(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM user_themes WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- папки фонов ---

type BackgroundFolder struct {
	ID        int64  `json:"id"`
	ParentID  *int64 `json:"parent_id"`
	Name      string `json:"name"`
	Position  int    `json:"position"`
	Collapsed bool   `json:"collapsed"`
}

func (s *Store) ListBackgroundFolders(ctx context.Context, userID int64) ([]BackgroundFolder, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, parent_id, name, position, collapsed
		FROM background_folders WHERE user_id = $1 ORDER BY position, id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[BackgroundFolder])
}

func (s *Store) CreateBackgroundFolder(ctx context.Context, userID int64, name string, parent *int64) (BackgroundFolder, error) {
	var f BackgroundFolder
	rows, err := s.pool.Query(ctx, `
		INSERT INTO background_folders (user_id, parent_id, name, position)
		VALUES ($1, $2, $3,
		        (SELECT COALESCE(MAX(position) + 1, 0) FROM background_folders
		         WHERE user_id = $1 AND parent_id IS NOT DISTINCT FROM $2))
		RETURNING id, parent_id, name, position, collapsed`, userID, parent, name)
	if err != nil {
		return f, err
	}
	return pgx.CollectOneRow(rows, pgx.RowToStructByPos[BackgroundFolder])
}

// UpdateBackgroundFolder меняет имя, родителя и свёрнутость (любое из полей).
func (s *Store) UpdateBackgroundFolder(ctx context.Context, userID, id int64, name *string, collapsed *bool, parent *int64, setParent bool) error {
	// защита от петли: папку нельзя положить внутрь самой себя
	if setParent && parent != nil {
		if loop, err := s.folderIsDescendant(ctx, userID, *parent, id); err != nil {
			return err
		} else if loop || *parent == id {
			return ErrConflict
		}
	}
	q := `UPDATE background_folders SET
			name = COALESCE($3, name),
			collapsed = COALESCE($4, collapsed)`
	args := []any{userID, id, name, collapsed}
	if setParent {
		q += `, parent_id = $5`
		args = append(args, parent)
	}
	q += ` WHERE user_id = $1 AND id = $2`
	tag, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// folderIsDescendant — является ли candidate потомком root (обход вверх).
func (s *Store) folderIsDescendant(ctx context.Context, userID, candidate, root int64) (bool, error) {
	var found bool
	err := s.pool.QueryRow(ctx, `
		WITH RECURSIVE up AS (
			SELECT id, parent_id FROM background_folders WHERE user_id = $1 AND id = $2
			UNION ALL
			SELECT f.id, f.parent_id FROM background_folders f
			JOIN up ON f.id = up.parent_id WHERE f.user_id = $1
		)
		SELECT EXISTS (SELECT 1 FROM up WHERE id = $3)`, userID, candidate, root).Scan(&found)
	return found, err
}

// DeleteBackgroundFolder удаляет папку; вложенные уходят каскадом, а картинки
// не пропадают — у них folder_id обнуляется (ON DELETE SET NULL).
func (s *Store) DeleteBackgroundFolder(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM background_folders WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) MoveBackgroundImage(ctx context.Context, userID, imageID int64, folder *int64) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE user_backgrounds SET folder_id = $3 WHERE user_id = $1 AND id = $2`,
		userID, imageID, folder)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- общая галерея (наполняет админ, видна всем) ---

type GalleryCategory struct {
	ID       int64  `json:"id"`
	ParentID *int64 `json:"parent_id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

type GalleryImage struct {
	ID         int64  `json:"id"`
	CategoryID *int64 `json:"category_id"`
	Filename   string `json:"filename"`
	Thumb      string `json:"thumb"`
	Title      string `json:"title"`
	Position   int    `json:"position"`
}

func (s *Store) ListGalleryCategories(ctx context.Context) ([]GalleryCategory, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, parent_id, name, position FROM gallery_categories
		ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[GalleryCategory])
}

func (s *Store) ListGalleryImages(ctx context.Context) ([]GalleryImage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, category_id, filename, thumb, title, position FROM gallery_images
		ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[GalleryImage])
}

func (s *Store) CreateGalleryCategory(ctx context.Context, name string, parent *int64) (GalleryCategory, error) {
	var c GalleryCategory
	rows, err := s.pool.Query(ctx, `
		INSERT INTO gallery_categories (parent_id, name, position)
		VALUES ($1, $2, (SELECT COALESCE(MAX(position) + 1, 0) FROM gallery_categories
		                 WHERE parent_id IS NOT DISTINCT FROM $1))
		RETURNING id, parent_id, name, position`, parent, name)
	if err != nil {
		return c, err
	}
	return pgx.CollectOneRow(rows, pgx.RowToStructByPos[GalleryCategory])
}

func (s *Store) RenameGalleryCategory(ctx context.Context, id int64, name string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE gallery_categories SET name = $2 WHERE id = $1`, id, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteGalleryCategory(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM gallery_categories WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AddGalleryImage(ctx context.Context, adminID int64, img GalleryImage) (GalleryImage, error) {
	var out GalleryImage
	rows, err := s.pool.Query(ctx, `
		INSERT INTO gallery_images (category_id, filename, thumb, title, position, uploaded_by)
		VALUES ($1, $2, $3, $4,
		        (SELECT COALESCE(MAX(position) + 1, 0) FROM gallery_images
		         WHERE category_id IS NOT DISTINCT FROM $1), $5)
		RETURNING id, category_id, filename, thumb, title, position`,
		img.CategoryID, img.Filename, img.Thumb, img.Title, adminID)
	if err != nil {
		return out, err
	}
	return pgx.CollectOneRow(rows, pgx.RowToStructByPos[GalleryImage])
}

func (s *Store) UpdateGalleryImage(ctx context.Context, id int64, title *string, category *int64, setCategory bool) error {
	q := `UPDATE gallery_images SET title = COALESCE($2, title)`
	args := []any{id, title}
	if setCategory {
		q += `, category_id = $3`
		args = append(args, category)
	}
	q += ` WHERE id = $1`
	tag, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteGalleryImage возвращает имена файлов, чтобы удалить их с диска.
func (s *Store) DeleteGalleryImage(ctx context.Context, id int64) (filename, thumb string, err error) {
	err = s.pool.QueryRow(ctx,
		`DELETE FROM gallery_images WHERE id = $1 RETURNING filename, thumb`, id).
		Scan(&filename, &thumb)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return filename, thumb, err
}

// --- расширенные настройки фона ---

// BackgroundPlacement — масштаб, смещение и точка фокуса картинки.
type BackgroundPlacement struct {
	Scale   int `json:"scale"`
	OffsetX int `json:"offset_x"`
	OffsetY int `json:"offset_y"`
	FocalX  int `json:"focal_x"`
	FocalY  int `json:"focal_y"`
}

func (s *Store) GetBackgroundPlacement(ctx context.Context, userID int64) (BackgroundPlacement, error) {
	p := BackgroundPlacement{Scale: 100, FocalX: 50, FocalY: 50}
	err := s.pool.QueryRow(ctx, `
		SELECT bg_scale, bg_offset_x, bg_offset_y, bg_focal_x, bg_focal_y
		FROM user_settings WHERE user_id = $1`, userID).Scan(
		&p.Scale, &p.OffsetX, &p.OffsetY, &p.FocalX, &p.FocalY)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, nil
	}
	return p, err
}

func (s *Store) SetBackgroundPlacement(ctx context.Context, userID int64, p BackgroundPlacement) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, bg_scale, bg_offset_x, bg_offset_y, bg_focal_x, bg_focal_y)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id) DO UPDATE SET
			bg_scale = EXCLUDED.bg_scale, bg_offset_x = EXCLUDED.bg_offset_x,
			bg_offset_y = EXCLUDED.bg_offset_y, bg_focal_x = EXCLUDED.bg_focal_x,
			bg_focal_y = EXCLUDED.bg_focal_y, updated_at = now()`,
		userID, p.Scale, p.OffsetX, p.OffsetY, p.FocalX, p.FocalY)
	return err
}

// SetBackgroundImageThumb — привязка уменьшенной копии к загруженной картинке.
func (s *Store) SetBackgroundImageThumb(ctx context.Context, userID, id int64, thumb string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE user_backgrounds SET thumb = $3 WHERE user_id = $1 AND id = $2`,
		userID, id, thumb)
	return err
}

