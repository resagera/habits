package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Сейф: сервер хранит шифротекст и конверты ключей, но не может ничего
// расшифровать — пароль остаётся в браузере. Отсюда и форма таблиц: имена
// файлов и типы лежат в meta_env одним непрозрачным полем.

type VaultFolder struct {
	ID       int64  `json:"id"`
	ParentID *int64 `json:"parent_id"`
	Name     string `json:"name"`
	Hint     string `json:"hint"`
	Thumbs   bool   `json:"thumbs"`
	// косметика: не показывать вложенные папки, пока не введён пароль
	HideChildren   bool   `json:"hide_children"`
	AutoDeleteDays int32  `json:"auto_delete_days"`
	KdfSalt        string `json:"kdf_salt"`
	KdfIter        int32  `json:"kdf_iter"`
	WrappedKey     string `json:"wrapped_key"`
	WrapIV         string `json:"wrap_iv"`
	Position       int32  `json:"position"`
	OwnerID        int64  `json:"owner_id"`
	Mine           bool   `json:"mine"`
	Shared         bool   `json:"shared"`               // есть участники или получена по шарингу
	OwnerName      string `json:"owner_name,omitempty"` // имя владельца у полученных
}

type VaultFile struct {
	ID        int64      `json:"id"`
	FolderID  int64      `json:"folder_id"`
	Size      int64      `json:"size_bytes"`
	PlainSize int64      `json:"plain_size"`
	KeyEnv    string     `json:"key_env"`
	MetaEnv   string     `json:"meta_env"`
	ChunkSize int32      `json:"chunk_size"`
	HasThumb  bool       `json:"has_thumb"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	OwnerID   int64      `json:"owner_id"`
	Mine      bool       `json:"mine"`
	Shared    bool       `json:"shared"`
}

// VaultQuota — сколько занято и сколько можно (лимиты по типу пользователя).
type VaultQuota struct {
	Used       int64 `json:"used"`
	TotalLimit int64 `json:"total_limit"`
	FileLimit  int64 `json:"file_limit"`
}

const folderCols = `f.id, f.parent_id, f.name, f.hint, f.thumbs, f.hide_children,
	f.auto_delete_days, f.kdf_salt, f.kdf_iter, f.wrapped_key, f.wrap_iv, f.position, f.user_id`

// ListVaultFolders — свои папки и папки, расшаренные пользователю.
// Расшаренная подпапка приходит как корневая: родителя получатель не видит.
func (s *Store) ListVaultFolders(ctx context.Context, userID int64) ([]VaultFolder, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+folderCols+`,
		       (f.user_id = $1) AS mine,
		       (f.user_id <> $1 OR EXISTS (
		           SELECT 1 FROM vault_shares s
		           WHERE s.kind = 'folder' AND s.target_id = f.id)) AS shared,
		       CASE WHEN f.user_id = $1 THEN ''
		            ELSE COALESCE(NULLIF(u.first_name, ''), '@' || u.username, '#' || f.user_id::text)
		       END AS owner_name
		FROM vault_folders f JOIN users u ON u.id = f.user_id
		WHERE f.user_id = $1
		   OR EXISTS (SELECT 1 FROM vault_shares s
		              WHERE s.kind = 'folder' AND s.target_id = f.id AND s.user_id = $1)
		ORDER BY (f.user_id = $1) DESC, f.position, f.id`, userID)
	if err != nil {
		return nil, err
	}
	folders, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (VaultFolder, error) {
		var f VaultFolder
		err := row.Scan(&f.ID, &f.ParentID, &f.Name, &f.Hint, &f.Thumbs, &f.HideChildren,
			&f.AutoDeleteDays, &f.KdfSalt, &f.KdfIter, &f.WrappedKey, &f.WrapIV, &f.Position,
			&f.OwnerID, &f.Mine, &f.Shared, &f.OwnerName)
		return f, err
	})
	if err != nil {
		return nil, err
	}
	// у чужой папки родителя в списке нет — показываем её как корневую
	own := make(map[int64]bool, len(folders))
	for _, f := range folders {
		own[f.ID] = true
	}
	for i := range folders {
		if folders[i].ParentID != nil && !own[*folders[i].ParentID] {
			folders[i].ParentID = nil
		}
	}
	return folders, nil
}

// ListVaultFiles — файлы доступных папок плюс файлы, расшаренные поштучно.
func (s *Store) ListVaultFiles(ctx context.Context, userID int64) ([]VaultFile, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT v.id, v.folder_id, v.size_bytes, v.plain_size, v.key_env, v.meta_env,
		       v.chunk_size, v.thumb_name <> '', v.created_at, v.expires_at, v.user_id,
		       (v.user_id = $1) AS mine,
		       (v.user_id <> $1 OR EXISTS (
		           SELECT 1 FROM vault_shares s
		           WHERE s.kind = 'file' AND s.target_id = v.id)) AS shared
		FROM vault_files v
		WHERE v.user_id = $1
		   OR EXISTS (SELECT 1 FROM vault_shares s
		              WHERE s.kind = 'file' AND s.target_id = v.id AND s.user_id = $1)
		   OR EXISTS (SELECT 1 FROM vault_shares s
		              WHERE s.kind = 'folder' AND s.target_id = v.folder_id AND s.user_id = $1)
		ORDER BY v.created_at DESC, v.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (VaultFile, error) {
		var f VaultFile
		err := row.Scan(&f.ID, &f.FolderID, &f.Size, &f.PlainSize, &f.KeyEnv, &f.MetaEnv,
			&f.ChunkSize, &f.HasThumb, &f.CreatedAt, &f.ExpiresAt, &f.OwnerID, &f.Mine, &f.Shared)
		return f, err
	})
}

// VaultUsage — занято байт (считается ТОЛЬКО владельцу: расшаренный файл
// висит на том, кто его загрузил).
func (s *Store) VaultUsage(ctx context.Context, userID int64) (int64, error) {
	var used int64
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(plain_size), 0) FROM vault_files WHERE user_id = $1`, userID).Scan(&used)
	return used, err
}

func (s *Store) VaultQuotaFor(ctx context.Context, userID int64) (VaultQuota, error) {
	var q VaultQuota
	limits, err := s.LimitsForUser(ctx, userID)
	if err != nil {
		return q, err
	}
	used, err := s.VaultUsage(ctx, userID)
	if err != nil {
		return q, err
	}
	const mb = 1 << 20
	return VaultQuota{
		Used:       used,
		TotalLimit: int64(limits.VaultTotalMB) * mb,
		FileLimit:  int64(limits.VaultFileMB) * mb,
	}, nil
}

func (s *Store) CreateVaultFolder(ctx context.Context, userID int64, f VaultFolder) (VaultFolder, error) {
	var out VaultFolder
	err := s.pool.QueryRow(ctx, `
		INSERT INTO vault_folders (user_id, parent_id, name, hint, thumbs, hide_children,
		                           auto_delete_days, kdf_salt, kdf_iter, wrapped_key, wrap_iv, position)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
		        (SELECT COALESCE(MAX(position) + 1, 0) FROM vault_folders
		         WHERE user_id = $1 AND parent_id IS NOT DISTINCT FROM $2))
		RETURNING id, parent_id, name, hint, thumbs, hide_children, auto_delete_days,
		          kdf_salt, kdf_iter, wrapped_key, wrap_iv, position, user_id`,
		userID, f.ParentID, f.Name, f.Hint, f.Thumbs, f.HideChildren, f.AutoDeleteDays,
		f.KdfSalt, f.KdfIter, f.WrappedKey, f.WrapIV).
		Scan(&out.ID, &out.ParentID, &out.Name, &out.Hint, &out.Thumbs, &out.HideChildren,
			&out.AutoDeleteDays, &out.KdfSalt, &out.KdfIter, &out.WrappedKey, &out.WrapIV,
			&out.Position, &out.OwnerID)
	out.Mine = true
	return out, err
}

// UpdateVaultFolder меняет имя/подсказку и, при смене пароля, обёртку ключа.
// Обёртка приходит только целиком: salt, iter, wrapped_key и iv меняются вместе.
func (s *Store) UpdateVaultFolder(ctx context.Context, userID, id int64, name, hint *string,
	hideChildren *bool, autoDeleteDays *int32, wrap *VaultFolder) (VaultFolder, error) {
	var out VaultFolder
	var salt, iv, key *string
	var iter *int32
	if wrap != nil {
		salt, iv, key, iter = &wrap.KdfSalt, &wrap.WrapIV, &wrap.WrappedKey, &wrap.KdfIter
	}
	err := s.pool.QueryRow(ctx, `
		UPDATE vault_folders
		SET name = COALESCE($3, name),
		    hint = COALESCE($4, hint),
		    kdf_salt = COALESCE($5, kdf_salt),
		    kdf_iter = COALESCE($6, kdf_iter),
		    wrapped_key = COALESCE($7, wrapped_key),
		    wrap_iv = COALESCE($8, wrap_iv),
		    hide_children = COALESCE($9, hide_children),
		    auto_delete_days = COALESCE($10, auto_delete_days),
		    updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING id, parent_id, name, hint, thumbs, hide_children, auto_delete_days,
		          kdf_salt, kdf_iter, wrapped_key, wrap_iv, position, user_id`,
		id, userID, name, hint, salt, iter, key, iv, hideChildren, autoDeleteDays).
		Scan(&out.ID, &out.ParentID, &out.Name, &out.Hint, &out.Thumbs, &out.HideChildren,
			&out.AutoDeleteDays, &out.KdfSalt, &out.KdfIter, &out.WrappedKey, &out.WrapIV,
			&out.Position, &out.OwnerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	out.Mine = true
	return out, err
}

// DeleteVaultFolder удаляет папку с поддеревом и возвращает имена файлов на
// диске, чтобы вызывающий их стёр (в базе они уходят каскадом).
func (s *Store) DeleteVaultFolder(ctx context.Context, userID, id int64) ([]string, error) {
	var names []string
	rows, err := s.pool.Query(ctx, `
		WITH RECURSIVE sub AS (
			SELECT id FROM vault_folders WHERE id = $1 AND user_id = $2
			UNION ALL
			SELECT f.id FROM vault_folders f JOIN sub ON f.parent_id = sub.id
		)
		SELECT blob_name, thumb_name FROM vault_files WHERE folder_id IN (SELECT id FROM sub)`,
		id, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var blob, thumb string
		if err := rows.Scan(&blob, &thumb); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, blob)
		if thumb != "" {
			names = append(names, thumb)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM vault_folders WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return names, nil
}

// CanWriteVaultFolder — можно ли класть файлы в папку (владелец или участник).
func (s *Store) CanWriteVaultFolder(ctx context.Context, userID, folderID int64) (bool, int64, error) {
	var ownerID int64
	err := s.pool.QueryRow(ctx, `
		SELECT f.user_id FROM vault_folders f
		WHERE f.id = $1 AND (f.user_id = $2 OR EXISTS (
			SELECT 1 FROM vault_shares s
			WHERE s.kind = 'folder' AND s.target_id = f.id AND s.user_id = $2))`,
		folderID, userID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, 0, nil
	}
	return err == nil, ownerID, err
}

func (s *Store) CreateVaultFile(ctx context.Context, userID int64, f VaultFile, blobName, thumbName string) (VaultFile, error) {
	var out VaultFile
	err := s.pool.QueryRow(ctx, `
		INSERT INTO vault_files (user_id, folder_id, blob_name, thumb_name, size_bytes,
		                         plain_size, key_env, meta_env, chunk_size, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
		        -- срок жизни наследуется от папки: 0 дней = без срока
		        (SELECT now() + make_interval(days => auto_delete_days)
		         FROM vault_folders WHERE id = $2 AND auto_delete_days > 0))
		RETURNING id, folder_id, size_bytes, plain_size, key_env, meta_env, chunk_size,
		          thumb_name <> '', created_at, expires_at, user_id`,
		userID, f.FolderID, blobName, thumbName, f.Size, f.PlainSize, f.KeyEnv, f.MetaEnv, f.ChunkSize).
		Scan(&out.ID, &out.FolderID, &out.Size, &out.PlainSize, &out.KeyEnv, &out.MetaEnv,
			&out.ChunkSize, &out.HasThumb, &out.CreatedAt, &out.ExpiresAt, &out.OwnerID)
	out.Mine = true
	return out, err
}

// VaultFileAccess — файл и его имена на диске, если у пользователя есть
// доступ (свой, расшаренный поштучно или через папку). Владелец нужен
// вызывающему: чужие обращения пишутся в журнал, свои — нет.
func (s *Store) VaultFileAccess(ctx context.Context, userID, id int64) (VaultFile, string, string, error) {
	var f VaultFile
	var blob, thumb string
	err := s.pool.QueryRow(ctx, `
		SELECT v.id, v.folder_id, v.size_bytes, v.plain_size, v.key_env, v.meta_env,
		       v.chunk_size, v.thumb_name <> '', v.created_at, v.expires_at, v.user_id,
		       v.blob_name, v.thumb_name
		FROM vault_files v
		WHERE v.id = $1 AND (v.user_id = $2
		  OR EXISTS (SELECT 1 FROM vault_shares s
		             WHERE s.kind = 'file' AND s.target_id = v.id AND s.user_id = $2)
		  OR EXISTS (SELECT 1 FROM vault_shares s
		             WHERE s.kind = 'folder' AND s.target_id = v.folder_id AND s.user_id = $2))`,
		id, userID).Scan(&f.ID, &f.FolderID, &f.Size, &f.PlainSize, &f.KeyEnv, &f.MetaEnv,
		&f.ChunkSize, &f.HasThumb, &f.CreatedAt, &f.ExpiresAt, &f.OwnerID, &blob, &thumb)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, "", "", ErrNotFound
	}
	f.Mine = f.OwnerID == userID
	return f, blob, thumb, err
}

// CopyVaultFile — копия файла в другой папке. Байты на диске уже скопированы
// вызывающим; здесь только строка. Ключ содержимого приходит перевёрнутым
// под ключ целевой папки, перезаливки и перешифровки нет.
func (s *Store) CopyVaultFile(ctx context.Context, userID int64, src VaultFile,
	folderID int64, keyEnv, metaEnv, blobName, thumbName string) (VaultFile, error) {
	return s.CreateVaultFile(ctx, userID, VaultFile{
		FolderID: folderID, Size: src.Size, PlainSize: src.PlainSize,
		KeyEnv: keyEnv, MetaEnv: metaEnv, ChunkSize: src.ChunkSize,
	}, blobName, thumbName)
}

// SetVaultFileExpiry — самоуничтожение через N дней (0 — снять срок).
func (s *Store) SetVaultFileExpiry(ctx context.Context, userID int64, ids []int64, days int32) (int, error) {
	var expires *time.Time
	if days > 0 {
		t := time.Now().AddDate(0, 0, int(days))
		expires = &t
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE vault_files SET expires_at = $3 WHERE user_id = $1 AND id = ANY($2)`,
		userID, ids, expires)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// SweepVaultFiles удаляет файлы с истёкшим сроком и возвращает имена на
// диске. Место освобождается сразу — корзины в сейфе нет.
func (s *Store) SweepVaultFiles(ctx context.Context) ([]string, int, error) {
	rows, err := s.pool.Query(ctx, `
		DELETE FROM vault_files WHERE expires_at IS NOT NULL AND expires_at < now()
		RETURNING blob_name, thumb_name`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var names []string
	deleted := 0
	for rows.Next() {
		var blob, thumb string
		if err := rows.Scan(&blob, &thumb); err != nil {
			return nil, 0, err
		}
		deleted++
		names = append(names, blob)
		if thumb != "" {
			names = append(names, thumb)
		}
	}
	return names, deleted, rows.Err()
}

// --- журнал доступа ---

type VaultAccessEntry struct {
	UserID   *int64    `json:"user_id"`
	UserName string    `json:"user_name"`
	Via      string    `json:"via"` // share | link
	At       time.Time `json:"at"`
}

// LogVaultAccess пишет чужое обращение к файлу. Ошибку глотать нельзя молча
// у вызывающего, но и рвать выдачу файла из-за журнала не стоит.
func (s *Store) LogVaultAccess(ctx context.Context, fileID int64, userID *int64, via string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO vault_access_log (file_id, user_id, via) VALUES ($1, $2, $3)`,
		fileID, userID, via)
	return err
}

// ListVaultAccess — последние обращения к своему файлу.
func (s *Store) ListVaultAccess(ctx context.Context, ownerID, fileID int64) ([]VaultAccessEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT l.user_id,
		       COALESCE(NULLIF(u.first_name, ''), '@' || u.username, '') AS user_name,
		       l.via, l.at
		FROM vault_access_log l
		LEFT JOIN users u ON u.id = l.user_id
		WHERE l.file_id = $1
		  AND EXISTS (SELECT 1 FROM vault_files v WHERE v.id = $1 AND v.user_id = $2)
		ORDER BY l.at DESC LIMIT 50`, fileID, ownerID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[VaultAccessEntry])
}

// --- временные ссылки ---

// VaultLink — ссылка на ОДИН файл. В базе только хеш токена: утечка базы не
// даёт рабочих ссылок. Ключ содержимого завёрнут под пароль ссылки, ключ
// папки наружу не уходит.
type VaultLink struct {
	ID        int64     `json:"id"`
	FileID    int64     `json:"file_id"`
	KdfSalt   string    `json:"kdf_salt"`
	KdfIter   int32     `json:"kdf_iter"`
	KeyEnv    string    `json:"key_env"`
	MetaEnv   string    `json:"meta_env"`
	ExpiresAt time.Time `json:"expires_at"`
	MaxViews  int32     `json:"max_views"`
	Views     int32     `json:"views"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CreateVaultLink(ctx context.Context, userID int64, tokenHash string, l VaultLink) (VaultLink, error) {
	var out VaultLink
	err := s.pool.QueryRow(ctx, `
		INSERT INTO vault_links (file_id, user_id, token_hash, kdf_salt, kdf_iter,
		                         key_env, meta_env, expires_at, max_views)
		SELECT $2, $1, $3, $4, $5, $6, $7, $8, $9
		WHERE EXISTS (SELECT 1 FROM vault_files v WHERE v.id = $2 AND v.user_id = $1)
		RETURNING id, file_id, kdf_salt, kdf_iter, key_env, meta_env, expires_at,
		          max_views, views, created_at`,
		userID, l.FileID, tokenHash, l.KdfSalt, l.KdfIter, l.KeyEnv, l.MetaEnv, l.ExpiresAt, l.MaxViews).
		Scan(&out.ID, &out.FileID, &out.KdfSalt, &out.KdfIter, &out.KeyEnv, &out.MetaEnv,
			&out.ExpiresAt, &out.MaxViews, &out.Views, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, err
}

// ListVaultLinks — живые ссылки на свой файл. Самого токена здесь нет и быть
// не может: он показывается один раз при создании.
func (s *Store) ListVaultLinks(ctx context.Context, userID, fileID int64) ([]VaultLink, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT l.id, l.file_id, l.kdf_salt, l.kdf_iter, l.key_env, l.meta_env,
		       l.expires_at, l.max_views, l.views, l.created_at
		FROM vault_links l
		WHERE l.file_id = $1 AND l.user_id = $2 AND l.expires_at > now()
		ORDER BY l.created_at DESC`, fileID, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[VaultLink])
}

func (s *Store) RevokeVaultLink(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM vault_links WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// VaultLinkByToken отдаёт ссылку и файл по хешу токена. consume=true
// засчитывает открытие (и упирается в лимит открытий) — считаем на выдаче
// блоба, а не на загрузке страницы: иначе счётчик тратился бы впустую.
// Просроченная, исчерпанная и несуществующая ссылка неотличимы: ErrNotFound.
func (s *Store) VaultLinkByToken(ctx context.Context, tokenHash string, consume bool) (VaultLink, VaultFile, string, error) {
	var l VaultLink
	var f VaultFile
	var blob string
	q := `
		SELECT l.id, l.file_id, l.kdf_salt, l.kdf_iter, l.key_env, l.meta_env,
		       l.expires_at, l.max_views, l.views, l.created_at,
		       v.plain_size, v.size_bytes, v.chunk_size, v.blob_name
		FROM vault_links l JOIN vault_files v ON v.id = l.file_id
		WHERE l.token_hash = $1 AND l.expires_at > now()
		  AND (l.max_views = 0 OR l.views < l.max_views)`
	if consume {
		q = `
		WITH taken AS (
			UPDATE vault_links SET views = views + 1
			WHERE token_hash = $1 AND expires_at > now()
			  AND (max_views = 0 OR views < max_views)
			RETURNING id, file_id, kdf_salt, kdf_iter, key_env, meta_env,
			          expires_at, max_views, views, created_at
		)
		SELECT l.id, l.file_id, l.kdf_salt, l.kdf_iter, l.key_env, l.meta_env,
		       l.expires_at, l.max_views, l.views, l.created_at,
		       v.plain_size, v.size_bytes, v.chunk_size, v.blob_name
		FROM taken l JOIN vault_files v ON v.id = l.file_id`
	}
	err := s.pool.QueryRow(ctx, q, tokenHash).Scan(&l.ID, &l.FileID, &l.KdfSalt, &l.KdfIter,
		&l.KeyEnv, &l.MetaEnv, &l.ExpiresAt, &l.MaxViews, &l.Views, &l.CreatedAt,
		&f.PlainSize, &f.Size, &f.ChunkSize, &blob)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, f, "", ErrNotFound
	}
	f.ID = l.FileID
	return l, f, blob, err
}

// SweepVaultLinks убирает просроченные ссылки и старые записи журнала.
func (s *Store) SweepVaultLinks(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM vault_links WHERE expires_at < now() - interval '1 day'`); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM vault_access_log WHERE at < now() - interval '90 days'`)
	return err
}

// UpdateVaultFile — переименование (meta_env) и перенос в другую папку
// (key_env перевёрнут под ключ новой папки). Только владелец.
func (s *Store) UpdateVaultFile(ctx context.Context, userID, id int64, metaEnv, keyEnv *string, folderID *int64) (VaultFile, error) {
	var out VaultFile
	err := s.pool.QueryRow(ctx, `
		UPDATE vault_files
		SET meta_env = COALESCE($3, meta_env),
		    key_env = COALESCE($4, key_env),
		    folder_id = COALESCE($5, folder_id)
		WHERE id = $1 AND user_id = $2
		RETURNING id, folder_id, size_bytes, plain_size, key_env, meta_env, chunk_size,
		          thumb_name <> '', created_at, expires_at, user_id`,
		id, userID, metaEnv, keyEnv, folderID).
		Scan(&out.ID, &out.FolderID, &out.Size, &out.PlainSize, &out.KeyEnv, &out.MetaEnv,
			&out.ChunkSize, &out.HasThumb, &out.CreatedAt, &out.ExpiresAt, &out.OwnerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	out.Mine = true
	return out, err
}

// DeleteVaultFiles удаляет файлы владельца и возвращает имена на диске.
// Удаление у владельца забирает файл и у всех, кому он был расшарен: это
// доступ к одному файлу, а не копии.
func (s *Store) DeleteVaultFiles(ctx context.Context, userID int64, ids []int64) ([]string, int, error) {
	rows, err := s.pool.Query(ctx, `
		DELETE FROM vault_files WHERE user_id = $1 AND id = ANY($2)
		RETURNING blob_name, thumb_name`, userID, ids)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var names []string
	deleted := 0
	for rows.Next() {
		var blob, thumb string
		if err := rows.Scan(&blob, &thumb); err != nil {
			return nil, 0, err
		}
		// имён на диске бывает два (блоб и превью), а файл удалён один
		deleted++
		names = append(names, blob)
		if thumb != "" {
			names = append(names, thumb)
		}
	}
	return names, deleted, rows.Err()
}

// --- шаринг: доступ к папке или к отдельному файлу ---

// vaultShareTitle — название для уведомления и «входящих». У файла имя
// зашифровано, поэтому показываем нейтральное «файл из папки «X»».
func (s *Store) vaultShareTitle(ctx context.Context, kind string, ownerID, refID int64) (string, error) {
	var name string
	var err error
	switch kind {
	case "vault_folder":
		err = s.pool.QueryRow(ctx,
			`SELECT name FROM vault_folders WHERE id = $1 AND user_id = $2`, refID, ownerID).Scan(&name)
	case "vault_file":
		err = s.pool.QueryRow(ctx, `
			SELECT 'файл из папки «' || f.name || '»'
			FROM vault_files v JOIN vault_folders f ON f.id = v.folder_id
			WHERE v.id = $1 AND v.user_id = $2`, refID, ownerID).Scan(&name)
	default:
		return "", ErrNotFound
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return name, err
}

// ShareVault выдаёт доступ (не копию): получателю видны те же шифроданные,
// а открыть их он сможет, только зная пароль папки.
func (s *Store) ShareVault(ctx context.Context, kind string, ownerID, refID, recipientID int64) (string, error) {
	name, err := s.vaultShareTitle(ctx, kind, ownerID, refID)
	if err != nil {
		return "", err
	}
	target := "folder"
	if kind == "vault_file" {
		target = "file"
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO vault_shares (kind, target_id, user_id) VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, target, refID, recipientID)
	return name, err
}

func (s *Store) ListVaultShares(ctx context.Context, userID int64, kind string, refID int64) ([]AccessUser, error) {
	if _, err := s.vaultShareTitle(ctx, "vault_"+kind, userID, refID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, COALESCE(u.username, ''), COALESCE(u.first_name, '')
		FROM vault_shares s JOIN users u ON u.id = s.user_id
		WHERE s.kind = $1 AND s.target_id = $2 ORDER BY s.created_at`, kind, refID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[AccessUser])
}

// RevokeVaultShare: владелец снимает доступ у любого, участник — у себя
// («убрать из своего списка»).
func (s *Store) RevokeVaultShare(ctx context.Context, requesterID int64, kind string, refID, targetID int64) error {
	table := "vault_folders"
	if kind == "file" {
		table = "vault_files"
	}
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM vault_shares s
		USING `+table+` t
		WHERE s.kind = $1 AND s.target_id = $2 AND s.user_id = $3 AND t.id = s.target_id
		  AND (t.user_id = $4 OR $3 = $4)`, kind, refID, targetID, requesterID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
