package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type ArticleFolder struct {
	ID       int64  `json:"id"`
	ParentID *int64 `json:"parent_id"`
	Name     string `json:"name"`
	Position int32  `json:"position"`
}

// Article в дереве отдаётся без content (он бывает большим).
type Article struct {
	ID            int64     `json:"id"`
	FolderID      *int64    `json:"folder_id"`
	Title         string    `json:"title"`
	Content       string    `json:"content,omitempty"`
	ShareToken    *string   `json:"share_token,omitempty"`
	DownloadToken *string   `json:"download_token,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// articleCols — колонки, которые сканируются в Article (scanArticle).
const articleCols = `id, folder_id, title, content, share_token, download_token, created_at, updated_at`

func scanArticle(row pgx.Row) (Article, error) {
	var a Article
	err := row.Scan(&a.ID, &a.FolderID, &a.Title, &a.Content, &a.ShareToken, &a.DownloadToken, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (s *Store) ArticlesTree(ctx context.Context, userID int64) ([]ArticleFolder, []Article, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, parent_id, name, position FROM articles_folders
		WHERE user_id = $1 ORDER BY position, id`, userID)
	if err != nil {
		return nil, nil, err
	}
	folders, err := pgx.CollectRows(rows, pgx.RowToStructByPos[ArticleFolder])
	if err != nil {
		return nil, nil, err
	}
	rows, err = s.pool.Query(ctx, `
		SELECT id, folder_id, title, share_token, download_token, created_at, updated_at FROM articles
		WHERE user_id = $1 ORDER BY position, id`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var articles []Article
	for rows.Next() {
		var a Article
		if err := rows.Scan(&a.ID, &a.FolderID, &a.Title, &a.ShareToken, &a.DownloadToken, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, nil, err
		}
		articles = append(articles, a)
	}
	return folders, articles, rows.Err()
}

func (s *Store) articleFolderOwned(ctx context.Context, userID int64, folderID *int64) error {
	if folderID == nil {
		return nil
	}
	var owned bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM articles_folders WHERE id = $1 AND user_id = $2)`,
		*folderID, userID).Scan(&owned)
	if err != nil {
		return err
	}
	if !owned {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateArticleFolder(ctx context.Context, userID int64, name string, parentID *int64) (ArticleFolder, error) {
	if err := s.articleFolderOwned(ctx, userID, parentID); err != nil {
		return ArticleFolder{}, err
	}
	var f ArticleFolder
	err := s.pool.QueryRow(ctx, `
		INSERT INTO articles_folders (user_id, parent_id, name, position)
		VALUES ($1, $2, $3,
		        (SELECT COALESCE(MAX(position) + 1, 0) FROM articles_folders
		         WHERE user_id = $1 AND parent_id IS NOT DISTINCT FROM $2))
		RETURNING id, parent_id, name, position`,
		userID, parentID, name).Scan(&f.ID, &f.ParentID, &f.Name, &f.Position)
	return f, err
}

func (s *Store) UpdateArticleFolder(ctx context.Context, userID, id int64, name *string, parentID **int64) (ArticleFolder, error) {
	if parentID != nil {
		if err := s.articleFolderOwned(ctx, userID, *parentID); err != nil {
			return ArticleFolder{}, err
		}
	}
	setParent := parentID != nil
	var newParent *int64
	if setParent {
		newParent = *parentID
	}
	var f ArticleFolder
	err := s.pool.QueryRow(ctx, `
		UPDATE articles_folders
		SET name = COALESCE($3, name),
		    parent_id = CASE WHEN $4 THEN $5 ELSE parent_id END
		WHERE id = $1 AND user_id = $2
		RETURNING id, parent_id, name, position`,
		id, userID, name, setParent, newParent).Scan(&f.ID, &f.ParentID, &f.Name, &f.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}

func (s *Store) DeleteArticleFolder(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM articles_folders WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetArticle возвращает статью с содержимым.
func (s *Store) GetArticle(ctx context.Context, userID, id int64) (Article, error) {
	return scanArticle(s.pool.QueryRow(ctx, `
		SELECT `+articleCols+` FROM articles WHERE id = $1 AND user_id = $2`, id, userID))
}

func (s *Store) CreateArticle(ctx context.Context, userID int64, title, content string, folderID *int64) (Article, error) {
	if err := s.articleFolderOwned(ctx, userID, folderID); err != nil {
		return Article{}, err
	}
	return scanArticle(s.pool.QueryRow(ctx, `
		INSERT INTO articles (user_id, folder_id, title, content, position)
		VALUES ($1, $2, $3, $4,
		        (SELECT COALESCE(MAX(position) + 1, 0) FROM articles
		         WHERE user_id = $1 AND folder_id IS NOT DISTINCT FROM $2))
		RETURNING `+articleCols, userID, folderID, title, content))
}

// maxArticleRevisions — сколько последних ревизий хранится на статью.
const maxArticleRevisions = 30

func (s *Store) UpdateArticle(ctx context.Context, userID, id int64, title, content *string, folderID **int64) (Article, error) {
	if folderID != nil {
		if err := s.articleFolderOwned(ctx, userID, *folderID); err != nil {
			return Article{}, err
		}
	}
	setFolder := folderID != nil
	var newFolder *int64
	if setFolder {
		newFolder = *folderID
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Article{}, err
	}
	defer tx.Rollback(ctx)

	// история: если content меняется, старая версия уходит в ревизии
	if content != nil {
		var old string
		err := tx.QueryRow(ctx, `
			SELECT content FROM articles WHERE id = $1 AND user_id = $2 FOR UPDATE`,
			id, userID).Scan(&old)
		if errors.Is(err, pgx.ErrNoRows) {
			return Article{}, ErrNotFound
		}
		if err != nil {
			return Article{}, err
		}
		if old != *content {
			if _, err := tx.Exec(ctx, `
				INSERT INTO article_revisions (article_id, content) VALUES ($1, $2)`,
				id, old); err != nil {
				return Article{}, err
			}
			if _, err := tx.Exec(ctx, `
				DELETE FROM article_revisions WHERE article_id = $1 AND id NOT IN (
					SELECT id FROM article_revisions WHERE article_id = $1
					ORDER BY saved_at DESC, id DESC LIMIT $2)`,
				id, maxArticleRevisions); err != nil {
				return Article{}, err
			}
		}
	}

	a, err := scanArticle(tx.QueryRow(ctx, `
		UPDATE articles
		SET title = COALESCE($3, title),
		    content = COALESCE($4, content),
		    folder_id = CASE WHEN $5 THEN $6 ELSE folder_id END,
		    updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING `+articleCols,
		id, userID, title, content, setFolder, newFolder))
	if err != nil {
		return a, err
	}
	return a, tx.Commit(ctx)
}

// ArticleRevision — запись истории изменений (без content в списке).
type ArticleRevision struct {
	ID      int64     `json:"id"`
	SavedAt time.Time `json:"saved_at"`
	Size    int64     `json:"size"`
}

// ListArticleRevisions — история своей статьи, новые сверху.
func (s *Store) ListArticleRevisions(ctx context.Context, userID, articleID int64) ([]ArticleRevision, error) {
	var owned bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM articles WHERE id = $1 AND user_id = $2)`,
		articleID, userID).Scan(&owned); err != nil {
		return nil, err
	}
	if !owned {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, saved_at, octet_length(content) FROM article_revisions
		WHERE article_id = $1 ORDER BY saved_at DESC, id DESC`, articleID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ArticleRevision])
}

// GetArticleRevision — содержимое ревизии своей статьи.
func (s *Store) GetArticleRevision(ctx context.Context, userID, revID int64) (string, time.Time, error) {
	var content string
	var savedAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT r.content, r.saved_at FROM article_revisions r
		JOIN articles a ON a.id = r.article_id
		WHERE r.id = $1 AND a.user_id = $2`, revID, userID).Scan(&content, &savedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", savedAt, ErrNotFound
	}
	return content, savedAt, err
}

// --- позиция чтения ---

// SetReadPosition сохраняет долю прокрутки (0..1) для доступной статьи.
func (s *Store) SetReadPosition(ctx context.Context, userID, articleID int64, pos float32) error {
	tag, err := s.pool.Exec(ctx, sharedSubtreeSQL+`
		INSERT INTO article_read_positions (user_id, article_id, pos, updated_at)
		SELECT $1, id, $3, now() FROM articles
		WHERE id = $2 AND (user_id = $1 OR folder_id IN (SELECT id FROM shared))
		ON CONFLICT (user_id, article_id)
		DO UPDATE SET pos = EXCLUDED.pos, updated_at = now()`,
		userID, articleID, pos)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ReadPosition — сохранённая доля прокрутки (0, если не было).
func (s *Store) ReadPosition(ctx context.Context, userID, articleID int64) (float32, error) {
	var pos float32
	err := s.pool.QueryRow(ctx, `
		SELECT pos FROM article_read_positions WHERE user_id = $1 AND article_id = $2`,
		userID, articleID).Scan(&pos)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return pos, err
}

func (s *Store) DeleteArticle(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM articles WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func randomToken() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// EnsureArticleToken выдаёт (или возвращает) токен указанного вида:
// column — 'share_token', 'download_token' или 'read_token'.
func (s *Store) EnsureArticleToken(ctx context.Context, userID, id int64, column string) (string, error) {
	if column != "share_token" && column != "download_token" && column != "read_token" {
		return "", ErrNotFound
	}
	var token *string
	err := s.pool.QueryRow(ctx, `
		SELECT `+column+` FROM articles WHERE id = $1 AND user_id = $2`, id, userID).Scan(&token)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if token != nil {
		return *token, nil
	}
	fresh, err := randomToken()
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE articles SET `+column+` = $3 WHERE id = $1 AND user_id = $2`, id, userID, fresh)
	return fresh, err
}

// ArticleByDownloadToken — публичное скачивание .md по токену.
func (s *Store) ArticleByDownloadToken(ctx context.Context, token string) (Article, error) {
	return scanArticle(s.pool.QueryRow(ctx, `
		SELECT `+articleCols+` FROM articles WHERE download_token = $1`, token))
}

// ArticleByReadToken — публичное чтение по токену.
func (s *Store) ArticleByReadToken(ctx context.Context, token string) (Article, error) {
	return scanArticle(s.pool.QueryRow(ctx, `
		SELECT `+articleCols+` FROM articles WHERE read_token = $1`, token))
}

// CopyArticleTo копирует статью получателю (шаринг по пользователю).
func (s *Store) CopyArticleTo(ctx context.Context, ownerID, id, recipientID int64) (string, error) {
	a, err := s.GetArticle(ctx, ownerID, id)
	if err != nil {
		return "", err
	}
	_, err = s.CreateArticle(ctx, recipientID, a.Title, a.Content, nil)
	return a.Title, err
}

// RedeemArticleToken копирует статью по токену-приглашению.
func (s *Store) RedeemArticleToken(ctx context.Context, userID int64, token string) (Article, error) {
	var title, content string
	err := s.pool.QueryRow(ctx, `
		SELECT title, content FROM articles WHERE share_token = $1`, token).Scan(&title, &content)
	if errors.Is(err, pgx.ErrNoRows) {
		return Article{}, ErrNotFound
	}
	if err != nil {
		return Article{}, err
	}
	return s.CreateArticle(ctx, userID, title, content, nil)
}

// --- шаринг категорий доступом (без дублирования контента) ---

// SharedTree — поддерево чужой категории, доступное пользователю.
type SharedTree struct {
	Root     ArticleFolder   `json:"root"`
	Folders  []ArticleFolder `json:"folders"`
	Articles []Article       `json:"articles"`
	Owner    AccessUser      `json:"owner"`
}

// sharedSubtreeIDs — id всех папок, доступных пользователю через шаринг
// (корни шаринга + их потомки у владельца).
const sharedSubtreeSQL = `
	WITH RECURSIVE shared AS (
		SELECT f.id FROM articles_folders f
		JOIN articles_folder_shares s ON s.folder_id = f.id AND s.user_id = $1
		UNION
		SELECT c.id FROM articles_folders c JOIN shared p ON c.parent_id = p.id
	)`

// SharedTrees возвращает доступные пользователю чужие категории.
func (s *Store) SharedTrees(ctx context.Context, userID int64) ([]SharedTree, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.parent_id, f.name, f.position,
		       u.id, COALESCE(u.username, ''), COALESCE(u.first_name, '')
		FROM articles_folder_shares sh
		JOIN articles_folders f ON f.id = sh.folder_id
		JOIN users u ON u.id = f.user_id
		WHERE sh.user_id = $1
		ORDER BY f.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var trees []SharedTree
	for rows.Next() {
		var t SharedTree
		if err := rows.Scan(&t.Root.ID, &t.Root.ParentID, &t.Root.Name, &t.Root.Position,
			&t.Owner.ID, &t.Owner.Username, &t.Owner.FirstName); err != nil {
			return nil, err
		}
		t.Root.ParentID = nil // корень секции «доступные мне»
		t.Folders = []ArticleFolder{}
		t.Articles = []Article{}
		trees = append(trees, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range trees {
		t := &trees[i]
		frows, err := s.pool.Query(ctx, `
			WITH RECURSIVE sub AS (
				SELECT id FROM articles_folders WHERE id = $1
				UNION ALL
				SELECT c.id FROM articles_folders c JOIN sub p ON c.parent_id = p.id
			)
			SELECT f.id, f.parent_id, f.name, f.position FROM articles_folders f
			JOIN sub ON sub.id = f.id AND f.id <> $1
			ORDER BY f.position, f.id`, t.Root.ID)
		if err != nil {
			return nil, err
		}
		folders, err := pgx.CollectRows(frows, pgx.RowToStructByPos[ArticleFolder])
		if err != nil {
			return nil, err
		}
		if folders != nil {
			t.Folders = folders
		}
		arows, err := s.pool.Query(ctx, `
			WITH RECURSIVE sub AS (
				SELECT id FROM articles_folders WHERE id = $1
				UNION ALL
				SELECT c.id FROM articles_folders c JOIN sub p ON c.parent_id = p.id
			)
			SELECT a.id, a.folder_id, a.title, a.created_at, a.updated_at FROM articles a
			JOIN sub ON sub.id = a.folder_id
			ORDER BY a.position, a.id`, t.Root.ID)
		if err != nil {
			return nil, err
		}
		defer arows.Close()
		for arows.Next() {
			var a Article
			if err := arows.Scan(&a.ID, &a.FolderID, &a.Title, &a.CreatedAt, &a.UpdatedAt); err != nil {
				return nil, err
			}
			t.Articles = append(t.Articles, a)
		}
		if err := arows.Err(); err != nil {
			return nil, err
		}
	}
	return trees, nil
}

// GetArticleShared — статья своя ИЛИ из расшаренного мне поддерева.
func (s *Store) GetArticleShared(ctx context.Context, userID, id int64) (Article, error) {
	return scanArticle(s.pool.QueryRow(ctx, sharedSubtreeSQL+`
		SELECT `+articleCols+` FROM articles
		WHERE id = $2 AND (user_id = $1 OR folder_id IN (SELECT id FROM shared))`,
		userID, id))
}

// ShareArticleFolder выдаёт доступ к своей категории.
func (s *Store) ShareArticleFolder(ctx context.Context, ownerID, folderID, recipientID int64) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `
		SELECT name FROM articles_folders WHERE id = $1 AND user_id = $2`,
		folderID, ownerID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO articles_folder_shares (folder_id, user_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, folderID, recipientID)
	return name, err
}

// ListFolderShares — кому владелец открыл категорию.
func (s *Store) ListFolderShares(ctx context.Context, ownerID, folderID int64) ([]AccessUser, error) {
	var owned bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM articles_folders WHERE id = $1 AND user_id = $2)`,
		folderID, ownerID).Scan(&owned); err != nil {
		return nil, err
	}
	if !owned {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, COALESCE(u.username, ''), COALESCE(u.first_name, '')
		FROM articles_folder_shares s JOIN users u ON u.id = s.user_id
		WHERE s.folder_id = $1 ORDER BY s.created_at`, folderID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[AccessUser])
}

// RevokeFolderShare: владелец отзывает (checkOwner=true) или получатель
// сам покидает категорию (checkOwner=false, userID — получатель).
func (s *Store) RevokeFolderShare(ctx context.Context, ownerID, folderID, userID int64, checkOwner bool) error {
	if checkOwner {
		var owned bool
		if err := s.pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM articles_folders WHERE id = $1 AND user_id = $2)`,
			folderID, ownerID).Scan(&owned); err != nil {
			return err
		}
		if !owned {
			return ErrNotFound
		}
	}
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM articles_folder_shares WHERE folder_id = $1 AND user_id = $2`, folderID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ArticleSearchHit — результат поиска по содержимому.
type ArticleSearchHit struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

// SearchArticlesContent — поиск по содержимому своих и доступных статей,
// со сниппетом вокруг совпадения.
func (s *Store) SearchArticlesContent(ctx context.Context, userID int64, q string, limit int) ([]ArticleSearchHit, error) {
	rows, err := s.pool.Query(ctx, sharedSubtreeSQL+`
		SELECT a.id, a.title,
		       substr(a.content,
		              greatest(1, position(lower($2) in lower(a.content)) - 30), 90)
		FROM articles a
		WHERE (a.user_id = $1 OR a.folder_id IN (SELECT id FROM shared))
		  AND (a.content ILIKE '%' || $2 || '%' OR a.title ILIKE '%' || $2 || '%')
		ORDER BY a.updated_at DESC
		LIMIT $3`, userID, q, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ArticleSearchHit])
}
