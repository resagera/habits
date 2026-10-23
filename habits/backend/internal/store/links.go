package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type LinkFolder struct {
	ID       int64  `json:"id"`
	ParentID *int64 `json:"parent_id"`
	Name     string `json:"name"`
	Position int32  `json:"position"`
}

type Link struct {
	ID       int64    `json:"id"`
	FolderID *int64   `json:"folder_id"`
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Tags     []string `json:"tags"`
	Pinned   bool     `json:"pinned"`
	Position int32    `json:"position"`
	Clicks   int32    `json:"clicks"`
	Dead     bool     `json:"dead"`
}

type LinkPatch struct {
	Name     *string
	URL      *string
	Tags     *[]string
	Pinned   *bool
	Position *int32
	// FolderID: указатель на указатель, чтобы отличать "не менять" (nil)
	// от "переместить в корень" (*nil).
	FolderID **int64
}

type LinkFolderPatch struct {
	Name     *string
	Position *int32
	ParentID **int64
}

func (s *Store) LinksTree(ctx context.Context, userID int64) ([]LinkFolder, []Link, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, parent_id, name, position FROM links_folders
		WHERE user_id = $1 ORDER BY position, id`, userID)
	if err != nil {
		return nil, nil, err
	}
	folders, err := pgx.CollectRows(rows, pgx.RowToStructByPos[LinkFolder])
	if err != nil {
		return nil, nil, err
	}

	rows, err = s.pool.Query(ctx, `
		SELECT id, folder_id, name, url, tags, pinned, position, clicks, dead FROM links
		WHERE user_id = $1 ORDER BY position, id`, userID)
	if err != nil {
		return nil, nil, err
	}
	links, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Link])
	if err != nil {
		return nil, nil, err
	}
	return folders, links, nil
}

func (s *Store) folderOwned(ctx context.Context, q pgx.Tx, userID int64, folderID *int64) (bool, error) {
	if folderID == nil {
		return true, nil
	}
	var owned bool
	var err error
	query := `SELECT EXISTS (SELECT 1 FROM links_folders WHERE id = $1 AND user_id = $2)`
	if q != nil {
		err = q.QueryRow(ctx, query, *folderID, userID).Scan(&owned)
	} else {
		err = s.pool.QueryRow(ctx, query, *folderID, userID).Scan(&owned)
	}
	return owned, err
}

func (s *Store) CreateLinkFolder(ctx context.Context, userID int64, name string, parentID *int64) (LinkFolder, error) {
	var f LinkFolder
	if owned, err := s.folderOwned(ctx, nil, userID, parentID); err != nil {
		return f, err
	} else if !owned {
		return f, ErrNotFound
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO links_folders (user_id, parent_id, name, position)
		VALUES ($1, $2, $3,
		        (SELECT COALESCE(MAX(position) + 1, 0) FROM links_folders
		         WHERE user_id = $1 AND parent_id IS NOT DISTINCT FROM $2))
		RETURNING id, parent_id, name, position`,
		userID, parentID, name).Scan(&f.ID, &f.ParentID, &f.Name, &f.Position)
	return f, err
}

func (s *Store) UpdateLinkFolder(ctx context.Context, userID, id int64, p LinkFolderPatch) (LinkFolder, error) {
	var f LinkFolder
	if p.ParentID != nil {
		if owned, err := s.folderOwned(ctx, nil, userID, *p.ParentID); err != nil {
			return f, err
		} else if !owned {
			return f, ErrNotFound
		}
	}
	setParent := p.ParentID != nil
	var newParent *int64
	if setParent {
		newParent = *p.ParentID
	}
	err := s.pool.QueryRow(ctx, `
		UPDATE links_folders
		SET name = COALESCE($3, name),
		    position = COALESCE($4, position),
		    parent_id = CASE WHEN $5 THEN $6 ELSE parent_id END
		WHERE id = $1 AND user_id = $2
		RETURNING id, parent_id, name, position`,
		id, userID, p.Name, p.Position, setParent, newParent).
		Scan(&f.ID, &f.ParentID, &f.Name, &f.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}

func (s *Store) DeleteLinkFolder(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM links_folders WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateLink(ctx context.Context, userID int64, l Link) (Link, error) {
	var out Link
	if owned, err := s.folderOwned(ctx, nil, userID, l.FolderID); err != nil {
		return out, err
	} else if !owned {
		return out, ErrNotFound
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO links (user_id, folder_id, name, url, tags, pinned, position)
		VALUES ($1, $2, $3, $4, $5, $6,
		        (SELECT COALESCE(MAX(position) + 1, 0) FROM links
		         WHERE user_id = $1 AND folder_id IS NOT DISTINCT FROM $2))
		RETURNING id, folder_id, name, url, tags, pinned, position, clicks, dead`,
		userID, l.FolderID, l.Name, l.URL, l.Tags, l.Pinned).
		Scan(&out.ID, &out.FolderID, &out.Name, &out.URL, &out.Tags, &out.Pinned, &out.Position, &out.Clicks, &out.Dead)
	return out, err
}

// ClickLink увеличивает счётчик переходов и возвращает новое значение.
func (s *Store) ClickLink(ctx context.Context, userID, id int64) (int32, error) {
	var clicks int32
	err := s.pool.QueryRow(ctx, `
		UPDATE links SET clicks = clicks + 1
		WHERE id = $1 AND user_id = $2
		RETURNING clicks`, id, userID).Scan(&clicks)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return clicks, err
}

func (s *Store) UpdateLink(ctx context.Context, userID, id int64, p LinkPatch) (Link, error) {
	var out Link
	if p.FolderID != nil {
		if owned, err := s.folderOwned(ctx, nil, userID, *p.FolderID); err != nil {
			return out, err
		} else if !owned {
			return out, ErrNotFound
		}
	}
	setFolder := p.FolderID != nil
	var newFolder *int64
	if setFolder {
		newFolder = *p.FolderID
	}
	err := s.pool.QueryRow(ctx, `
		UPDATE links
		SET name = COALESCE($3, name),
		    url = COALESCE($4, url),
		    tags = COALESCE($5, tags),
		    pinned = COALESCE($6, pinned),
		    position = COALESCE($7, position),
		    folder_id = CASE WHEN $8 THEN $9 ELSE folder_id END,
		    -- при смене URL прошлая проверка неактуальна
		    dead = CASE WHEN $4::text IS NOT NULL AND $4 <> url THEN false ELSE dead END,
		    checked_at = CASE WHEN $4::text IS NOT NULL AND $4 <> url THEN NULL ELSE checked_at END,
		    updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING id, folder_id, name, url, tags, pinned, position, clicks, dead`,
		id, userID, p.Name, p.URL, p.Tags, p.Pinned, p.Position, setFolder, newFolder).
		Scan(&out.ID, &out.FolderID, &out.Name, &out.URL, &out.Tags, &out.Pinned, &out.Position, &out.Clicks, &out.Dead)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, err
}

func (s *Store) DeleteLink(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM links WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CopyLinkFolderTo копирует папку владельца (с вложенными папками и ссылками)
// в корень ссылок получателя — для «отправить пользователю». Возвращает имя
// папки. Работает только с серверным хранилищем: в локальном режиме id папки
// клиентский и на сервере её нет — обработчик отдаст 404.
func (s *Store) CopyLinkFolderTo(ctx context.Context, ownerID, folderID, recipientID int64) (string, error) {
	var rootName string
	err := s.pool.QueryRow(ctx, `
		SELECT name FROM links_folders WHERE id = $1 AND user_id = $2`,
		folderID, ownerID).Scan(&rootName)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}

	folders, links, err := s.LinksTree(ctx, ownerID)
	if err != nil {
		return "", err
	}
	// дети по родителю и ссылки по папке — для обхода поддерева
	childFolders := make(map[int64][]LinkFolder)
	for _, f := range folders {
		if f.ParentID != nil {
			childFolders[*f.ParentID] = append(childFolders[*f.ParentID], f)
		}
	}
	linksByFolder := make(map[int64][]Link)
	for _, l := range links {
		if l.FolderID != nil {
			linksByFolder[*l.FolderID] = append(linksByFolder[*l.FolderID], l)
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	// copyFolder создаёт папку у получателя, копирует её ссылки и рекурсивно
	// поддеревья. position берём как MAX+1 в целевой папке.
	var copyFolder func(srcID int64, newParent *int64, name string) error
	copyFolder = func(srcID int64, newParent *int64, name string) error {
		var newID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO links_folders (user_id, parent_id, name, position)
			VALUES ($1, $2, $3,
			        (SELECT COALESCE(MAX(position) + 1, 0) FROM links_folders
			         WHERE user_id = $1 AND parent_id IS NOT DISTINCT FROM $2))
			RETURNING id`, recipientID, newParent, name).Scan(&newID); err != nil {
			return err
		}
		for _, l := range linksByFolder[srcID] {
			if _, err := tx.Exec(ctx, `
				INSERT INTO links (user_id, folder_id, name, url, tags, pinned, position)
				VALUES ($1, $2, $3, $4, $5, $6,
				        (SELECT COALESCE(MAX(position) + 1, 0) FROM links
				         WHERE user_id = $1 AND folder_id IS NOT DISTINCT FROM $2))`,
				recipientID, newID, l.Name, l.URL, l.Tags, l.Pinned); err != nil {
				return err
			}
		}
		for _, sub := range childFolders[srcID] {
			if err := copyFolder(sub.ID, &newID, sub.Name); err != nil {
				return err
			}
		}
		return nil
	}
	if err := copyFolder(folderID, nil, rootName); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return rootName, nil
}

// CopyLinkTo копирует одну ссылку владельца в корень ссылок получателя —
// для «отправить пользователю» и redeem по токену. Возвращает имя ссылки.
func (s *Store) CopyLinkTo(ctx context.Context, ownerID, linkID, recipientID int64) (string, error) {
	var l Link
	err := s.pool.QueryRow(ctx, `
		SELECT id, folder_id, name, url, tags, pinned, position, clicks, dead
		FROM links WHERE id = $1 AND user_id = $2`, linkID, ownerID).
		Scan(&l.ID, &l.FolderID, &l.Name, &l.URL, &l.Tags, &l.Pinned, &l.Position, &l.Clicks, &l.Dead)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if l.Tags == nil {
		l.Tags = []string{}
	}
	// копия попадает в корень: у получателя может не быть папки-родителя
	if _, err := s.CreateLink(ctx, recipientID, Link{
		Name: l.Name, URL: l.URL, Tags: l.Tags, Pinned: l.Pinned,
	}); err != nil {
		return "", err
	}
	return l.Name, nil
}

// ensureLinksShareToken выдаёт (или возвращает существующий) токен-приглашение
// для строки table (links_folders | links) с проверкой владения. table —
// внутренняя константа, не пользовательский ввод.
func (s *Store) ensureLinksShareToken(ctx context.Context, table string, userID, id int64) (string, error) {
	var token *string
	err := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT share_token FROM %s WHERE id = $1 AND user_id = $2`, table),
		id, userID).Scan(&token)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if token != nil {
		return *token, nil
	}
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	fresh := hex.EncodeToString(buf)
	_, err = s.pool.Exec(ctx,
		fmt.Sprintf(`UPDATE %s SET share_token = $3 WHERE id = $1 AND user_id = $2`, table),
		id, userID, fresh)
	return fresh, err
}

// EnsureLinkFolderShareToken — токен-приглашение на папку (копия поддерева).
func (s *Store) EnsureLinkFolderShareToken(ctx context.Context, userID, folderID int64) (string, error) {
	return s.ensureLinksShareToken(ctx, "links_folders", userID, folderID)
}

// EnsureLinkShareToken — токен-приглашение на одну ссылку.
func (s *Store) EnsureLinkShareToken(ctx context.Context, userID, linkID int64) (string, error) {
	return s.ensureLinksShareToken(ctx, "links", userID, linkID)
}

// RedeemLinkFolderShareToken копирует папку (поддерево) по токену получателю.
// Своя же папка не дублируется — просто возвращаем имя.
func (s *Store) RedeemLinkFolderShareToken(ctx context.Context, userID int64, token string) (string, error) {
	var folderID, ownerID int64
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id FROM links_folders WHERE share_token = $1`, token).Scan(&folderID, &ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if ownerID == userID {
		var name string
		err := s.pool.QueryRow(ctx, `SELECT name FROM links_folders WHERE id = $1`, folderID).Scan(&name)
		return name, err
	}
	return s.CopyLinkFolderTo(ctx, ownerID, folderID, userID)
}

// RedeemLinkShareToken копирует одну ссылку по токену получателю.
func (s *Store) RedeemLinkShareToken(ctx context.Context, userID int64, token string) (string, error) {
	var linkID, ownerID int64
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id FROM links WHERE share_token = $1`, token).Scan(&linkID, &ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if ownerID == userID {
		var name string
		err := s.pool.QueryRow(ctx, `SELECT name FROM links WHERE id = $1`, linkID).Scan(&name)
		return name, err
	}
	return s.CopyLinkTo(ctx, ownerID, linkID, userID)
}

// BulkFolder/BulkLink — элементы полного импорта (перенос локального
// хранилища на сервер): клиентские id временные, сервер выдаёт свои.
type BulkFolder struct {
	TmpID       int64
	ParentTmpID *int64
	Name        string
	Position    int32
}

type BulkLink struct {
	FolderTmpID *int64
	Name        string
	URL         string
	Tags        []string
	Pinned      bool
	Position    int32
	Clicks      int32
}

// ReplaceLinks атомарно заменяет все папки и ссылки пользователя.
func (s *Store) ReplaceLinks(ctx context.Context, userID int64, folders []BulkFolder, links []BulkLink) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM links WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM links_folders WHERE user_id = $1`, userID); err != nil {
		return err
	}

	// Папки вставляем по уровням: сначала корневые, затем те, чей родитель
	// уже вставлен (id родителя маппится из временного в реальный).
	idMap := make(map[int64]int64, len(folders))
	pending := append([]BulkFolder(nil), folders...)
	for len(pending) > 0 {
		progressed := false
		next := pending[:0]
		for _, f := range pending {
			var parentReal *int64
			if f.ParentTmpID != nil {
				real, ok := idMap[*f.ParentTmpID]
				if !ok {
					next = append(next, f)
					continue
				}
				parentReal = &real
			}
			var newID int64
			if err := tx.QueryRow(ctx, `
				INSERT INTO links_folders (user_id, parent_id, name, position)
				VALUES ($1, $2, $3, $4) RETURNING id`,
				userID, parentReal, f.Name, f.Position).Scan(&newID); err != nil {
				return err
			}
			idMap[f.TmpID] = newID
			progressed = true
		}
		if !progressed {
			return errors.New("links import: folder parent cycle or missing parent")
		}
		pending = next
	}

	for _, l := range links {
		var folderReal *int64
		if l.FolderTmpID != nil {
			real, ok := idMap[*l.FolderTmpID]
			if !ok {
				continue // ссылка на несуществующую папку — кладём в корень
			}
			folderReal = &real
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO links (user_id, folder_id, name, url, tags, pinned, position, clicks)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			userID, folderReal, l.Name, l.URL, l.Tags, l.Pinned, l.Position, l.Clicks); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// --- проверка битых ссылок (опция links.dead_check) ---

// DeadCheckCandidate — ссылка, которую пора проверить.
type DeadCheckCandidate struct {
	ID   int64
	URL  string
	Dead bool
}

// DeadCheckCandidates возвращает ссылки пользователей с включённой опцией,
// не проверявшиеся дольше 12 часов.
func (s *Store) DeadCheckCandidates(ctx context.Context, limit int) ([]DeadCheckCandidate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT l.id, l.url, l.dead
		FROM links l
		JOIN feature_access f ON f.user_id = l.user_id AND f.feature = 'links.dead_check'
		WHERE l.checked_at IS NULL OR l.checked_at < now() - interval '12 hours'
		ORDER BY l.checked_at NULLS FIRST
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[DeadCheckCandidate])
}

func (s *Store) SetLinkDead(ctx context.Context, id int64, dead bool) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE links SET dead = $2, checked_at = now() WHERE id = $1`, id, dead)
	return err
}
