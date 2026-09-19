package store

import (
	"context"
	"encoding/json"
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
	// свой цвет фона приложения ('' — цвет темы по умолчанию)
	BgDark  string
	BgLight string
	// карточки-«стекло»: непрозрачность 0-100 (100 — сплошной) и размытие 0-30
	CardOpacity int32
	CardBlur    int32
}

type BackgroundImage struct {
	ID       int64
	Filename string
	FolderID *int64
	Thumb    string
}

func (s *Store) GetBackground(ctx context.Context, userID int64) (BackgroundSettings, []BackgroundImage, error) {
	bg := BackgroundSettings{Kind: "none", Position: "cover", CardOpacity: 100}
	err := s.pool.QueryRow(ctx, `
		SELECT bg_kind, bg_value, bg_position, bg_blur, bg_dim, text_color_dark, text_color_light, card_opacity, card_blur, bg_color_dark, bg_color_light
		FROM user_settings WHERE user_id = $1`,
		userID).Scan(&bg.Kind, &bg.Value, &bg.Position, &bg.Blur, &bg.Dim, &bg.TextDark, &bg.TextLight, &bg.CardOpacity, &bg.CardBlur, &bg.BgDark, &bg.BgLight)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return bg, nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, filename, folder_id, thumb FROM user_backgrounds
		WHERE user_id = $1 ORDER BY id DESC`, userID)
	if err != nil {
		return bg, nil, err
	}
	images, err := pgx.CollectRows(rows, pgx.RowToStructByPos[BackgroundImage])
	return bg, images, err
}

// SetBackground пишет ТОЛЬКО поля картинки. Цвета и «стекло» карточек с v2.67
// живут в теме (user_settings.appearance), а старые колонки text_color_*,
// bg_color_* и card_* остались как след миграции 0078 — трогать их отсюда
// нельзя: сохранение фона затирало бы перенесённые значения.
func (s *Store) SetBackground(ctx context.Context, userID int64, bg BackgroundSettings) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, bg_kind, bg_value, bg_position, bg_blur, bg_dim)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id) DO UPDATE
		SET bg_kind = EXCLUDED.bg_kind,
		    bg_value = EXCLUDED.bg_value,
		    bg_position = EXCLUDED.bg_position,
		    bg_blur = EXCLUDED.bg_blur,
		    bg_dim = EXCLUDED.bg_dim,
		    updated_at = now()`,
		userID, bg.Kind, bg.Value, bg.Position, bg.Blur, bg.Dim)
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

// GetCheckerTrashDays — срок хранения корзины Checker в днях (по умолчанию 30).
func (s *Store) GetCheckerTrashDays(ctx context.Context, userID int64) (int, error) {
	var v int
	err := s.pool.QueryRow(ctx, `
		SELECT checker_trash_days FROM user_settings WHERE user_id = $1`, userID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 30, nil
	}
	return v, err
}

func (s *Store) SetCheckerTrashDays(ctx context.Context, userID int64, days int) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, checker_trash_days) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET checker_trash_days = EXCLUDED.checker_trash_days, updated_at = now()`,
		userID, days)
	return err
}

// GetTestsPassStreak — сколько верных ответов подряд нужно, чтобы вопрос
// считался пройденным и ушёл из пула (по умолчанию 1).
func (s *Store) GetTestsPassStreak(ctx context.Context, userID int64) (int, error) {
	var v int
	err := s.pool.QueryRow(ctx, `
		SELECT tests_pass_streak FROM user_settings WHERE user_id = $1`, userID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 1, nil
	}
	return v, err
}

func (s *Store) SetTestsPassStreak(ctx context.Context, userID int64, n int) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, tests_pass_streak) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET tests_pass_streak = EXCLUDED.tests_pass_streak, updated_at = now()`,
		userID, n)
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

// --- оформление: тема (с сервера, чтобы совпадала везде) ---

// Appearance — тема интерфейса. Theme выбирается в Telegram и считается
// основной; WebTheme переопределяет её только для входа по токену (веб,
// расширение), '' = как в Telegram. WebBgOff убирает фоновую картинку там же.
type Appearance struct {
	Theme    string `json:"theme"`
	WebTheme string `json:"web_theme"`
	WebBgOff bool   `json:"web_bg_off"`
}

func (s *Store) GetAppearance(ctx context.Context, userID int64) (Appearance, error) {
	a := Appearance{Theme: "auto"}
	err := s.pool.QueryRow(ctx, `
		SELECT theme, web_theme, web_bg_off FROM user_settings WHERE user_id = $1`,
		userID).Scan(&a.Theme, &a.WebTheme, &a.WebBgOff)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, nil // строки настроек ещё нет — значения по умолчанию
	}
	return a, err
}

// SetTelegramTheme — основная тема (меняется только из Telegram).
func (s *Store) SetTelegramTheme(ctx context.Context, userID int64, theme string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, theme) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET theme = $2`, userID, theme)
	return err
}

// --- закреплённые заголовки страниц ---

// HeaderSettings — «закрепить заголовок при прокрутке». PinAll закрепляет
// сразу везде, Pages — точечно по именам роутов. Хранится на сервере (а не в
// localStorage, где терялось): настройка одна на аккаунт и во всех режимах
// входа — Telegram, веб, расширение.
type HeaderSettings struct {
	PinAll bool     `json:"pin_all"`
	Pages  []string `json:"pages"`
}

func (s *Store) GetHeaderSettings(ctx context.Context, userID int64) (HeaderSettings, error) {
	h := HeaderSettings{Pages: []string{}}
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT pin_all_headers, pinned_pages FROM user_settings WHERE user_id = $1`,
		userID).Scan(&h.PinAll, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return h, nil // строки настроек ещё нет — значения по умолчанию
	}
	if err != nil {
		return h, err
	}
	if err := json.Unmarshal(raw, &h.Pages); err != nil || h.Pages == nil {
		h.Pages = []string{}
	}
	return h, nil
}

func (s *Store) SetHeaderSettings(ctx context.Context, userID int64, h HeaderSettings) error {
	if h.Pages == nil {
		h.Pages = []string{}
	}
	raw, err := json.Marshal(h.Pages)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, pin_all_headers, pinned_pages) VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET
		    pin_all_headers = EXCLUDED.pin_all_headers,
		    pinned_pages = EXCLUDED.pinned_pages,
		    updated_at = now()`, userID, h.PinAll, raw)
	return err
}

// SetWebAppearance — оформление для входа по токену; на мини-приложение
// не влияет (отдельные колонки).
func (s *Store) SetWebAppearance(ctx context.Context, userID int64, webTheme string, bgOff bool) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, web_theme, web_bg_off) VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET web_theme = $2, web_bg_off = $3`,
		userID, webTheme, bgOff)
	return err
}
