package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrLimit — превышен лимит (например, фото на контакт).
var ErrLimit = errors.New("limit exceeded")

// ContactPhoto — одно фото контакта (галерея).
type ContactPhoto struct {
	ID  int64  `json:"id"`
	URL string `json:"url"`
}

// Contact — запись страницы Contacts. ContactID — id зарегистрированного
// пользователя; NULL означает «ещё не в боте»: тогда известное хранится в
// tg_id / ext_username / ext_name, а привязка случится в TouchUser.
type Contact struct {
	ID          int64          `json:"id"`
	ContactID   *int64         `json:"contact_id"`
	TgID        *int64         `json:"tg_id"`
	Username    string         `json:"username"`
	FirstName   string         `json:"first_name"`
	ExtUsername string         `json:"ext_username"`
	ExtName     string         `json:"ext_name"`
	Note        string         `json:"note"`
	AutoAccept  bool           `json:"auto_accept"`
	Photos      []ContactPhoto `json:"photos"`
}

const contactCols = `c.id, c.contact_id, c.tg_id, COALESCE(u.username, ''), COALESCE(u.first_name, ''),
	c.ext_username, c.ext_name, c.note, c.auto_accept`

func scanContact(row scanner) (Contact, error) {
	var c Contact
	err := row.Scan(&c.ID, &c.ContactID, &c.TgID, &c.Username, &c.FirstName,
		&c.ExtUsername, &c.ExtName, &c.Note, &c.AutoAccept)
	c.Photos = []ContactPhoto{}
	return c, err
}

func (s *Store) ListContacts(ctx context.Context, userID int64) ([]Contact, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+contactCols+`
		FROM contacts c LEFT JOIN users u ON u.id = c.contact_id
		WHERE c.user_id = $1
		ORDER BY c.created_at DESC, c.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	contacts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Contact, error) {
		return scanContact(row)
	})
	if err != nil || len(contacts) == 0 {
		return contacts, err
	}
	byID := make(map[int64]*Contact, len(contacts))
	for i := range contacts {
		byID[contacts[i].ID] = &contacts[i]
	}
	photoRows, err := s.pool.Query(ctx, `
		SELECT p.contact_id, p.id, p.photo
		FROM contact_photos p JOIN contacts c ON c.id = p.contact_id
		WHERE c.user_id = $1
		ORDER BY p.id`, userID)
	if err != nil {
		return nil, err
	}
	defer photoRows.Close()
	for photoRows.Next() {
		var cid int64
		var p ContactPhoto
		if err := photoRows.Scan(&cid, &p.ID, &p.URL); err != nil {
			return nil, err
		}
		if c, ok := byID[cid]; ok {
			c.Photos = append(c.Photos, p)
		}
	}
	return contacts, photoRows.Err()
}

// AddContactUser добавляет зарегистрированного пользователя (идемпотентно).
func (s *Store) AddContactUser(ctx context.Context, userID, contactUserID int64) (Contact, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO contacts (user_id, contact_id, tg_id) VALUES ($1, $2, $2)
		ON CONFLICT (user_id, contact_id) DO UPDATE SET contact_id = EXCLUDED.contact_id
		RETURNING id`, userID, contactUserID).Scan(&id)
	if err != nil {
		return Contact{}, err
	}
	return s.contactByID(ctx, userID, id)
}

// AddContactExternal добавляет человека, которого ещё нет в боте.
// Дедупликация — по tg_id или (без учёта регистра) по ext_username.
func (s *Store) AddContactExternal(ctx context.Context, userID int64, tgID *int64, extUsername, extName string) (Contact, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM contacts
		WHERE user_id = $1
		  AND (($2::bigint IS NOT NULL AND tg_id = $2)
		    OR ($3 <> '' AND lower(ext_username) = lower($3)))
		LIMIT 1`, userID, tgID, extUsername).Scan(&id)
	if err == nil {
		// уже есть — дополним пустые поля тем, что узнали
		_, _ = s.pool.Exec(ctx, `
			UPDATE contacts SET
				tg_id = COALESCE(tg_id, $3),
				ext_username = CASE WHEN ext_username = '' THEN $4 ELSE ext_username END,
				ext_name = CASE WHEN ext_name = '' THEN $5 ELSE ext_name END
			WHERE id = $1 AND user_id = $2`, id, userID, tgID, extUsername, extName)
		return s.contactByID(ctx, userID, id)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Contact{}, err
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO contacts (user_id, tg_id, ext_username, ext_name)
		VALUES ($1, $2, $3, $4)
		RETURNING id`, userID, tgID, extUsername, extName).Scan(&id)
	if err != nil {
		return Contact{}, err
	}
	return s.contactByID(ctx, userID, id)
}

func (s *Store) contactByID(ctx context.Context, userID, id int64) (Contact, error) {
	c, err := scanContact(s.pool.QueryRow(ctx, `
		SELECT `+contactCols+`
		FROM contacts c LEFT JOIN users u ON u.id = c.contact_id
		WHERE c.id = $1 AND c.user_id = $2`, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, photo FROM contact_photos WHERE contact_id = $1 ORDER BY id`, id)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var p ContactPhoto
		if err := rows.Scan(&p.ID, &p.URL); err != nil {
			return c, err
		}
		c.Photos = append(c.Photos, p)
	}
	return c, rows.Err()
}

// ContactPatch — частичное обновление контакта.
type ContactPatch struct {
	Note       *string
	AutoAccept *bool
}

func (s *Store) UpdateContact(ctx context.Context, userID, id int64, p ContactPatch) (Contact, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE contacts SET note = COALESCE($3, note), auto_accept = COALESCE($4, auto_accept)
		WHERE id = $1 AND user_id = $2`, id, userID, p.Note, p.AutoAccept)
	if err != nil {
		return Contact{}, err
	}
	if tag.RowsAffected() == 0 {
		return Contact{}, ErrNotFound
	}
	return s.contactByID(ctx, userID, id)
}

// maxContactPhotos — лимит галереи одного контакта.
const maxContactPhotos = 20

// AddContactPhoto добавляет фото в галерею контакта (с проверкой владения и лимита).
func (s *Store) AddContactPhoto(ctx context.Context, userID, contactID int64, url string) (ContactPhoto, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT count(p.*) FROM contacts c
		LEFT JOIN contact_photos p ON p.contact_id = c.id
		WHERE c.id = $1 AND c.user_id = $2
		GROUP BY c.id`, contactID, userID).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return ContactPhoto{}, ErrNotFound
	}
	if err != nil {
		return ContactPhoto{}, err
	}
	if count >= maxContactPhotos {
		return ContactPhoto{}, ErrLimit
	}
	p := ContactPhoto{URL: url}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO contact_photos (contact_id, photo) VALUES ($1, $2) RETURNING id`,
		contactID, url).Scan(&p.ID)
	return p, err
}

// DeleteContactPhoto удаляет фото, возвращая путь (для удаления файла).
func (s *Store) DeleteContactPhoto(ctx context.Context, userID, contactID, photoID int64) (string, error) {
	var url string
	err := s.pool.QueryRow(ctx, `
		DELETE FROM contact_photos p USING contacts c
		WHERE p.id = $1 AND p.contact_id = $2 AND c.id = p.contact_id AND c.user_id = $3
		RETURNING p.photo`, photoID, contactID, userID).Scan(&url)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return url, err
}

// DeleteContact удаляет контакт, возвращая пути всех фото (для удаления файлов).
func (s *Store) DeleteContact(ctx context.Context, userID, id int64) ([]string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
		SELECT p.photo FROM contact_photos p JOIN contacts c ON c.id = p.contact_id
		WHERE c.id = $1 AND c.user_id = $2`, id, userID)
	if err != nil {
		return nil, err
	}
	var photos []string
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			rows.Close()
			return nil, err
		}
		photos = append(photos, url)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM contacts WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return photos, tx.Commit(ctx)
}

// AutoAcceptFrom — есть ли у получателя отправитель в контактах с галочкой
// «принимать сразу».
func (s *Store) AutoAcceptFrom(ctx context.Context, toUser, fromUser int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM contacts
		               WHERE user_id = $1 AND contact_id = $2 AND auto_accept)`,
		toUser, fromUser).Scan(&ok)
	return ok, err
}

// --- входящие шаринги ---

// IncomingShare — шаринг, ожидающий подтверждения получателем.
type IncomingShare struct {
	ID            int64     `json:"id"`
	FromID        int64     `json:"from_id"`
	FromUsername  string    `json:"from_username"`
	FromFirstName string    `json:"from_first_name"`
	Kind          string    `json:"kind"`
	Title         string    `json:"title"`
	CreatedAt     time.Time `json:"created_at"`
}

func (s *Store) CreateIncomingShare(ctx context.Context, fromUser, toUser int64, kind string, refID int64, title string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO incoming_shares (from_user, to_user, kind, ref_id, title)
		VALUES ($1, $2, $3, $4, $5)`, fromUser, toUser, kind, refID, title)
	return err
}

func (s *Store) ListIncomingShares(ctx context.Context, toUser int64) ([]IncomingShare, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT i.id, i.from_user, COALESCE(u.username, ''), COALESCE(u.first_name, ''),
		       i.kind, i.title, i.created_at
		FROM incoming_shares i JOIN users u ON u.id = i.from_user
		WHERE i.to_user = $1
		ORDER BY i.created_at DESC, i.id DESC`, toUser)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[IncomingShare])
}

// IncomingShareRef — служебные поля входящего шаринга для применения.
type IncomingShareRef struct {
	FromUser int64
	Kind     string
	RefID    int64
	Title    string
}

func (s *Store) GetIncomingShare(ctx context.Context, id, toUser int64) (IncomingShareRef, error) {
	var row IncomingShareRef
	err := s.pool.QueryRow(ctx, `
		SELECT from_user, kind, ref_id, title FROM incoming_shares
		WHERE id = $1 AND to_user = $2`, id, toUser).Scan(
		&row.FromUser, &row.Kind, &row.RefID, &row.Title)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrNotFound
	}
	return row, err
}

func (s *Store) DeleteIncomingShare(ctx context.Context, id, toUser int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM incoming_shares WHERE id = $1 AND to_user = $2`, id, toUser)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- применение шаринга ---

// ShareTitle возвращает название источника с проверкой владения отправителем
// (для текста уведомления и строки во «входящих»).
func (s *Store) ShareTitle(ctx context.Context, kind string, ownerID, refID int64) (string, error) {
	var q string
	switch kind {
	case "checker_template":
		q = `SELECT name FROM checker_templates WHERE id = $1 AND user_id = $2`
	case "checker_group":
		q = `SELECT name FROM checker_groups WHERE id = $1 AND user_id = $2 AND parent_id IS NULL`
	case "reminder_category":
		q = `SELECT name FROM reminder_categories WHERE id = $1 AND user_id = $2`
	case "article":
		q = `SELECT title FROM articles WHERE id = $1 AND user_id = $2`
	case "links_folder":
		q = `SELECT name FROM links_folders WHERE id = $1 AND user_id = $2`
	case "link":
		q = `SELECT name FROM links WHERE id = $1 AND user_id = $2`
	case "tracker":
		q = `SELECT name FROM tracker_categories WHERE id = $1 AND user_id = $2`
	case "task_project":
		q = `SELECT name FROM task_projects WHERE id = $1 AND user_id = $2`
	case "project":
		q = `SELECT name FROM projects WHERE id = $1 AND user_id = $2`
	case "food":
		// объект — весь дневник владельца; ref_id обязан совпадать с отправителем
		if refID != ownerID {
			return "", ErrNotFound
		}
		return "Дневник питания", nil
	default:
		return "", errors.New("unknown share kind")
	}
	var name string
	err := s.pool.QueryRow(ctx, q, refID, ownerID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return name, err
}

// ApplyShare применяет шаринг kind/refID от fromUser к toUser: копия
// (checker/reminders/articles) или доступ (tracker/tasks). Возвращает название.
func (s *Store) ApplyShare(ctx context.Context, kind string, fromUser, refID, toUser int64) (string, error) {
	switch kind {
	case "checker_template":
		return s.CopyTemplateTo(ctx, fromUser, refID, toUser)
	case "checker_group":
		return s.CopyGroupTo(ctx, fromUser, refID, toUser)
	case "reminder_category":
		return s.CopyReminderCategoryTo(ctx, fromUser, refID, toUser, time.Now().UTC())
	case "article":
		return s.CopyArticleTo(ctx, fromUser, refID, toUser)
	case "links_folder":
		return s.CopyLinkFolderTo(ctx, fromUser, refID, toUser)
	case "link":
		return s.CopyLinkTo(ctx, fromUser, refID, toUser)
	case "tracker":
		return s.ShareTracker(ctx, fromUser, refID, toUser)
	case "task_project":
		return s.ShareTaskProject(ctx, fromUser, refID, toUser)
	case "project":
		return s.ShareProject(ctx, fromUser, refID, toUser)
	case "food":
		if refID != fromUser {
			return "", ErrNotFound
		}
		return s.ShareFoodDiary(ctx, fromUser, toUser)
	}
	return "", errors.New("unknown share kind")
}
