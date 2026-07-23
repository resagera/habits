package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// PageInfo — страница приложения в реестре доступов.
type PageInfo struct {
	Code              string `json:"code"`
	Title             string `json:"title"`
	Icon              string `json:"icon"`
	DefaultVisibility string `json:"-"` // all | personal
}

// FeatureInfo — отдельная опция страницы с собственным доступом.
type FeatureInfo struct {
	Code  string `json:"code"`
	Page  string `json:"page"`
	Title string `json:"title"`
}

// PagesRegistry — все управляемые страницы. Видимость по умолчанию
// переопределяется строкой в page_settings.
var PagesRegistry = []PageInfo{
	{Code: "tracker", Title: "Tracker", Icon: "📊", DefaultVisibility: "all"},
	{Code: "checker", Title: "Checker", Icon: "✅", DefaultVisibility: "all"},
	{Code: "tasks", Title: "Tasks", Icon: "🗂", DefaultVisibility: "all"},
	{Code: "diary", Title: "Diary", Icon: "📔", DefaultVisibility: "all"},
	{Code: "metrics", Title: "Metrics", Icon: "📈", DefaultVisibility: "all"},
	{Code: "passwords", Title: "Passwords", Icon: "🔑", DefaultVisibility: "all"},
	{Code: "reminders", Title: "Reminders", Icon: "🔔", DefaultVisibility: "all"},
	{Code: "converter", Title: "Converter", Icon: "💱", DefaultVisibility: "all"},
	{Code: "links", Title: "Links", Icon: "🔗", DefaultVisibility: "all"},
	{Code: "articles", Title: "Articles", Icon: "📄", DefaultVisibility: "all"},
	{Code: "servers", Title: "Servers", Icon: "🖥", DefaultVisibility: "personal"},
	{Code: "files", Title: "My Files", Icon: "📁", DefaultVisibility: "personal"},
	{Code: "terminal", Title: "Terminal", Icon: "⌨️", DefaultVisibility: "personal"},
	{Code: "contacts", Title: "Contacts", Icon: "👥", DefaultVisibility: "all"},
	{Code: "projects", Title: "Projects", Icon: "📦", DefaultVisibility: "all"},
	{Code: "food", Title: "Food", Icon: "🍽", DefaultVisibility: "all"},
	{Code: "automation", Title: "Автоматизация", Icon: "🤖", DefaultVisibility: "personal"},
	{Code: "help", Title: "Help", Icon: "🆘", DefaultVisibility: "all"},
}

// FeaturesRegistry — опции страниц, выдаются только персонально.
var FeaturesRegistry = []FeatureInfo{
	{Code: "links.dead_check", Page: "links", Title: "Проверка битых ссылок"},
}

func pageDefault(code string) string {
	for _, p := range PagesRegistry {
		if p.Code == code {
			return p.DefaultVisibility
		}
	}
	return ""
}

// PageVisibilities возвращает действующую видимость всех страниц реестра.
func (s *Store) PageVisibilities(ctx context.Context) (map[string]string, error) {
	result := make(map[string]string, len(PagesRegistry))
	for _, p := range PagesRegistry {
		result[p.Code] = p.DefaultVisibility
	}
	rows, err := s.pool.Query(ctx, `SELECT page, visibility FROM page_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var page, vis string
		if err := rows.Scan(&page, &vis); err != nil {
			return nil, err
		}
		if _, known := result[page]; known {
			result[page] = vis
		}
	}
	return result, rows.Err()
}

func (s *Store) SetPageVisibility(ctx context.Context, page, visibility string) error {
	if pageDefault(page) == "" {
		return ErrNotFound
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO page_settings (page, visibility) VALUES ($1, $2)
		ON CONFLICT (page) DO UPDATE SET visibility = EXCLUDED.visibility`, page, visibility)
	return err
}

// UserPageSet возвращает страницы, к которым у пользователя есть персональный
// доступ, и выданные ему опции.
func (s *Store) UserPageSet(ctx context.Context, userID int64) (pages map[string]bool, features map[string]bool, err error) {
	pages, features = map[string]bool{}, map[string]bool{}
	rows, err := s.pool.Query(ctx, `SELECT page FROM page_access WHERE user_id = $1`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, nil, err
		}
		pages[p] = true
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	frows, err := s.pool.Query(ctx, `SELECT feature FROM feature_access WHERE user_id = $1`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer frows.Close()
	for frows.Next() {
		var f string
		if err := frows.Scan(&f); err != nil {
			return nil, nil, err
		}
		features[f] = true
	}
	return pages, features, frows.Err()
}

// PageAllowed — доступна ли страница пользователю (без учёта админства).
func (s *Store) PageAllowed(ctx context.Context, userID int64, page string) (bool, error) {
	def := pageDefault(page)
	if def == "" {
		return true, nil // страница не управляется доступами
	}
	var vis string
	err := s.pool.QueryRow(ctx, `SELECT visibility FROM page_settings WHERE page = $1`, page).Scan(&vis)
	if err == pgx.ErrNoRows {
		vis = def
	} else if err != nil {
		return false, err
	}
	if vis == "all" {
		return true, nil
	}
	var allowed bool
	err = s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM page_access WHERE page = $1 AND user_id = $2)`,
		page, userID).Scan(&allowed)
	return allowed, err
}

// FeatureUserIDs — все пользователи с выданной опцией (для воркеров).
func (s *Store) FeatureUserIDs(ctx context.Context, feature string) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT user_id FROM feature_access WHERE feature = $1`, feature)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AccessUser — пользователь в списке доступов админки.
type AccessUser struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

// grantTable ограничивает имя таблицы белым списком (page_access/feature_access).
func grantQuery(table string) (list, insert, del string, ok bool) {
	switch table {
	case "page_access":
		return `SELECT u.id, COALESCE(u.username, ''), COALESCE(u.first_name, '')
				FROM page_access a JOIN users u ON u.id = a.user_id
				WHERE a.page = $1 ORDER BY a.created_at`,
			`INSERT INTO page_access (page, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			`DELETE FROM page_access WHERE page = $1 AND user_id = $2`, true
	case "feature_access":
		return `SELECT u.id, COALESCE(u.username, ''), COALESCE(u.first_name, '')
				FROM feature_access a JOIN users u ON u.id = a.user_id
				WHERE a.feature = $1 ORDER BY a.created_at`,
			`INSERT INTO feature_access (feature, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			`DELETE FROM feature_access WHERE feature = $1 AND user_id = $2`, true
	}
	return "", "", "", false
}

func (s *Store) ListGrants(ctx context.Context, table, key string) ([]AccessUser, error) {
	q, _, _, ok := grantQuery(table)
	if !ok {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, q, key)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[AccessUser])
}

func (s *Store) AddGrant(ctx context.Context, table, key string, userID int64) error {
	_, ins, _, ok := grantQuery(table)
	if !ok {
		return ErrNotFound
	}
	_, err := s.pool.Exec(ctx, ins, key, userID)
	if isForeignKeyViolation(err) {
		return ErrNotFound
	}
	return err
}

func (s *Store) RemoveGrant(ctx context.Context, table, key string, userID int64) error {
	_, _, del, ok := grantQuery(table)
	if !ok {
		return ErrNotFound
	}
	tag, err := s.pool.Exec(ctx, del, key, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SearchUsers — поиск пользователя по id, логину или имени (для админки).
func (s *Store) SearchUsers(ctx context.Context, q string, limit int) ([]AccessUser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, COALESCE(username, ''), COALESCE(first_name, '')
		FROM users
		WHERE ($1 ~ '^[0-9]+$' AND id = $1::bigint)
		   OR username ILIKE '%' || $1 || '%'
		   OR first_name ILIKE '%' || $1 || '%'
		ORDER BY last_seen_at DESC
		LIMIT $2`, q, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[AccessUser])
}

// FindUserExact — точное совпадение по id или username (для шаринга
// обычными пользователями, чтобы нельзя было перебирать список).
func (s *Store) FindUserExact(ctx context.Context, q string) (AccessUser, error) {
	var u AccessUser
	err := s.pool.QueryRow(ctx, `
		SELECT id, COALESCE(username, ''), COALESCE(first_name, '')
		FROM users
		WHERE ($1 ~ '^[0-9]+$' AND id = $1::bigint)
		   OR lower(username) = lower(ltrim($1, '@'))
		LIMIT 1`, q).Scan(&u.ID, &u.Username, &u.FirstName)
	if err == pgx.ErrNoRows {
		return u, ErrNotFound
	}
	return u, err
}

// TouchShareRecipient запоминает, что пользователь делился с получателем
// (для подсказок в формах шаринга).
func (s *Store) TouchShareRecipient(ctx context.Context, userID, recipientID int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO share_recipients (user_id, recipient_id) VALUES ($1, $2)
		ON CONFLICT (user_id, recipient_id) DO UPDATE SET last_at = now()`,
		userID, recipientID)
	return err
}

// RecentRecipients — с кем пользователь делился, свежие первыми.
func (s *Store) RecentRecipients(ctx context.Context, userID int64) ([]AccessUser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, COALESCE(u.username, ''), COALESCE(u.first_name, '')
		FROM share_recipients r JOIN users u ON u.id = r.recipient_id
		WHERE r.user_id = $1
		ORDER BY r.last_at DESC
		LIMIT 10`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[AccessUser])
}
