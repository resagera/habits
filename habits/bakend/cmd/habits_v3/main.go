package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/lib/pq"
	_ "github.com/lib/pq"
	"github.com/rs/cors"
)

type Link struct {
	ID           int64     `json:"id"`
	UserID       string    `json:"userId"`
	Name         string    `json:"name"`
	URL          string    `json:"url"`
	Description  string    `json:"description,omitempty"`
	FaviconURL   string    `json:"faviconUrl,omitempty"`
	FaviconImage string    `json:"faviconImage,omitempty"`
	Thumbnail    string    `json:"thumbnail,omitempty"`
	Note         string    `json:"note,omitempty"`
	Content      string    `json:"content,omitempty"`
	Status       int       `json:"status,omitempty"`
	Pinned       bool      `json:"pinned"`
	Usage        int       `json:"usage"`
	Tags         []string  `json:"tags"`
	Path         string    `json:"path"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type Node struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	URL         string   `json:"url,omitempty"`
	Description string   `json:"description,omitempty"`
	FaviconURL  string   `json:"faviconUrl,omitempty"`
	Thumbnail   string   `json:"thumbnail,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Pinned      bool     `json:"pinned"`
	Usage       int      `json:"usage"`
	Path        string   `json:"path"`
	Children    []*Node  `json:"children"`
}

var db *sql.DB

func main() {
	var err error
	connStr := "postgres://webapp_bot:webapp_bot_pgpwd4habr@localhost:5432/webapp_bot?sslmode=disable"
	//host=localhost port=5432 user=webapp_bot password=webapp_bot_pgpwd4habr dbname=webapp_bot sslmode=disable
	//postgres://webapp_bot:webapp_bot_pgpwd4habr@localhost:5432/webapp_bot?sslmode=disable
	//postgres://habits:habits5432@localhost:5432/habits?sslmode=disable
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}

	// Создаём мультиплексор (можно использовать и nil, но лучше явно)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/habits/links", handleLinks)
	mux.HandleFunc("/api/habits/links/search", handleSearchLinks)
	mux.HandleFunc("/api/habits/links/sync", handleLinksSync)
	mux.HandleFunc("/api/habits/links/folder", handleFolderLinks)
	mux.HandleFunc("/api/habits/links/children", handleChildrenLinks)
	mux.HandleFunc("/api/habits/links/tree", handleLinksTree)
	mux.HandleFunc("/api/habits/links/tree/update_path", handleUpdatePath)
	mux.HandleFunc("/api/habits/links/tree/delete_branch", handleDeleteBranch)
	mux.HandleFunc("/api/habits/links/delete", handleDeleteLink)
	mux.HandleFunc("/api/habits/links/delete_bulk", handleBulkDelete)
	mux.HandleFunc("/api/habits/links/add_bulk", handleBulkAdd)
	mux.HandleFunc("/api/habits/folders", handleFolders)                           // GET список
	mux.HandleFunc("/api/habits/folders/sync", handleFoldersSync)                  // POST массив upsert / GET экспорт
	mux.HandleFunc("/api/habits/folders/delete_branch", handleFoldersDeleteBranch) // DELETE ветка
	//mux.HandleFunc("/api/habits/folders", handleFolders)
	mux.HandleFunc("/api/habits/folders/update", handleFolderUpdate)
	mux.HandleFunc("/api/habits/folders/delete", handleFolderDelete)

	// Настраиваем CORS
	c := cors.New(cors.Options{
		AllowedOrigins: []string{"*"}, // ← разрешить все домены (для продакшена укажите конкретные)
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"*"},
	})

	// Оборачиваем mux в CORS middleware
	handler := c.Handler(mux)

	fmt.Println("🚀 API started on :8676")
	log.Fatal(http.ListenAndServe(":8676", handler))
}

func handleLinks(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		http.Error(w, "missing userId", 400)
		return
	}

	switch r.Method {
	case http.MethodGet:
		rows, err := db.Query(`
			SELECT id, user_id, name, url, description, favicon_url, favicon_image, thumbnail,
			       note, content, status, pinned, usage, tags, path, created_at, updated_at
			FROM links WHERE user_id = $1 ORDER BY id`, userID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()

		var links []Link
		for rows.Next() {
			var l Link
			err := rows.Scan(&l.ID, &l.UserID, &l.Name, &l.URL, &l.Description, &l.FaviconURL,
				&l.FaviconImage, &l.Thumbnail, &l.Note, &l.Content, &l.Status,
				&l.Pinned, &l.Usage, pq.Array(&l.Tags), &l.Path, &l.CreatedAt, &l.UpdatedAt)
			if err == nil {
				links = append(links, l)
			}
		}
		writeJSON(w, links)

	case http.MethodPost:
		var l Link
		if err := json.NewDecoder(r.Body).Decode(&l); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		l.UserID = userID
		l.UpdatedAt = time.Now()
		if l.CreatedAt.IsZero() {
			l.CreatedAt = l.UpdatedAt
		}

		query := `
			INSERT INTO links (user_id, name, url, description, favicon_url, favicon_image, thumbnail,
			                   note, content, status, pinned, usage, tags, path, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
			ON CONFLICT (user_id, url) DO UPDATE SET
				name=EXCLUDED.name, description=EXCLUDED.description,
				favicon_url=EXCLUDED.favicon_url, favicon_image=EXCLUDED.favicon_image,
				thumbnail=EXCLUDED.thumbnail, note=EXCLUDED.note,
				content=EXCLUDED.content, status=EXCLUDED.status,
				pinned=EXCLUDED.pinned, usage=EXCLUDED.usage,
				tags=EXCLUDED.tags, path=EXCLUDED.path, updated_at=EXCLUDED.updated_at;
		`

		_, err := db.Exec(query, l.UserID, l.Name, l.URL, l.Description, l.FaviconURL, l.FaviconImage,
			l.Thumbnail, l.Note, l.Content, l.Status, l.Pinned, l.Usage,
			pq.Array(l.Tags), l.Path, l.CreatedAt, l.UpdatedAt)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func handleSearchLinks(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	query := r.URL.Query().Get("q")
	if userID == "" {
		http.Error(w, "missing userId", 400)
		return
	}

	q := "%" + strings.ToLower(query) + "%"
	rows, err := db.Query(`
		SELECT id, user_id, name, url, description, favicon_url, thumbnail, note, status, tags, usage, path
		FROM links
		WHERE user_id = $1
		AND (
			LOWER(name) ILIKE $2 OR
			LOWER(description) ILIKE $2 OR
			EXISTS (SELECT 1 FROM unnest(tags) t WHERE LOWER(t) ILIKE $2)
		)
		ORDER BY usage DESC LIMIT 100`, userID, q)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var res []Link
	for rows.Next() {
		var l Link
		rows.Scan(&l.ID, &l.UserID, &l.Name, &l.URL, &l.Description, &l.FaviconURL,
			&l.Thumbnail, &l.Note, &l.Status, pq.Array(&l.Tags), &l.Usage, &l.Path)
		res = append(res, l)
	}
	writeJSON(w, res)
}

// POST /api/v3/links/sync?userId=123
// Body: [ {Link}, {Link}, ... ]
func handleLinksSync(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		http.Error(w, "missing userId", 400)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	var links []Link
	if err := json.NewDecoder(r.Body).Decode(&links); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	stmt, err := tx.Prepare(`
		INSERT INTO links (user_id, name, url, description, favicon_url, favicon_image, thumbnail,
		                   note, content, status, pinned, usage, tags, path, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (user_id, url) DO UPDATE SET
			name=EXCLUDED.name,
			description=EXCLUDED.description,
			favicon_url=EXCLUDED.favicon_url,
			favicon_image=EXCLUDED.favicon_image,
			thumbnail=EXCLUDED.thumbnail,
			note=EXCLUDED.note,
			content=EXCLUDED.content,
			status=EXCLUDED.status,
			pinned=EXCLUDED.pinned,
			usage=EXCLUDED.usage,
			tags=EXCLUDED.tags,
			path=EXCLUDED.path,
			updated_at=EXCLUDED.updated_at;
	`)
	if err != nil {
		tx.Rollback()
		http.Error(w, err.Error(), 500)
		return
	}
	defer stmt.Close()

	now := time.Now()
	count := 0
	for _, l := range links {
		if l.URL == "" {
			continue
		}
		if l.CreatedAt.IsZero() {
			l.CreatedAt = now
		}
		l.UpdatedAt = now
		_, err = stmt.Exec(userID, l.Name, l.URL, l.Description, l.FaviconURL,
			l.FaviconImage, l.Thumbnail, l.Note, l.Content, l.Status,
			l.Pinned, l.Usage, pq.Array(l.Tags), l.Path, l.CreatedAt, l.UpdatedAt)
		if err != nil {
			tx.Rollback()
			http.Error(w, err.Error(), 500)
			return
		}
		count++
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	writeJSON(w, map[string]any{"ok": true, "count": count})
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// GET /api/v3/links/folder?userId=123&path=root.sub1
func handleFolderLinks(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	path := r.URL.Query().Get("path")
	if userID == "" || path == "" {
		http.Error(w, "missing userId or path", 400)
		return
	}

	rows, err := db.Query(`
		SELECT id, user_id, name, url, description, favicon_url, thumbnail, note, status, tags, usage, path, pinned
		FROM links
		WHERE user_id = $1 AND path = $2
		ORDER BY pinned DESC, usage DESC, id ASC
	`, userID, path)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var links []Link
	for rows.Next() {
		var l Link
		rows.Scan(&l.ID, &l.UserID, &l.Name, &l.URL, &l.Description,
			&l.FaviconURL, &l.Thumbnail, &l.Note, &l.Status,
			pq.Array(&l.Tags), &l.Usage, &l.Path, &l.Pinned)
		links = append(links, l)
	}
	writeJSON(w, links)
}

// GET /api/v3/links/children?userId=123&path=root.sub1
func handleChildrenLinks(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	path := r.URL.Query().Get("path")
	if userID == "" || path == "" {
		http.Error(w, "missing userId or path", 400)
		return
	}

	rows, err := db.Query(`
		SELECT id, user_id, name, url, description, favicon_url, thumbnail, note, status, tags, usage, path, pinned
		FROM links
		WHERE user_id = $1 AND path <@ $2
		ORDER BY path ASC, pinned DESC, usage DESC
	`, userID, path)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var links []Link
	for rows.Next() {
		var l Link
		rows.Scan(&l.ID, &l.UserID, &l.Name, &l.URL, &l.Description,
			&l.FaviconURL, &l.Thumbnail, &l.Note, &l.Status,
			pq.Array(&l.Tags), &l.Usage, &l.Path, &l.Pinned)
		links = append(links, l)
	}
	writeJSON(w, links)
}

func handleLinksTree(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		http.Error(w, "missing userId", 400)
		return
	}

	var folders []Folder
	rows, _ := db.Query(`SELECT id, name, code, path, open, password, color, settings
                         FROM folders WHERE user_id=$1 ORDER BY path`, userID)
	defer rows.Close()
	for rows.Next() {
		var f Folder
		_ = rows.Scan(&f.ID, &f.Name, &f.Code, &f.Path, &f.Open, &f.Password, &f.Color, &f.Settings)
		folders = append(folders, f)
	}

	links := []Link{}
	rows2, _ := db.Query(`SELECT id, name, url, pinned, usage, tags, path FROM links WHERE user_id=$1`, userID)
	defer rows2.Close()
	for rows2.Next() {
		var l Link
		_ = rows2.Scan(&l.ID, &l.Name, &l.URL, &l.Pinned, &l.Usage, pq.Array(&l.Tags), &l.Path)
		links = append(links, l)
	}

	// 🔄 Восстановление иерархии
	tree := assembleTree(folders, links)

	writeJSON(w, tree)
	//userID := r.URL.Query().Get("userId")
	//if userID == "" {
	//	http.Error(w, "missing userId", 400)
	//	return
	//}
	//
	//rows, err := db.Query(`
	//	SELECT id, user_id, name, url, description, favicon_url, thumbnail, note, status, tags, usage, path, pinned
	//	FROM links
	//	WHERE user_id = $1
	//	ORDER BY path ASC
	//`, userID)
	//if err != nil {
	//	http.Error(w, err.Error(), 500)
	//	return
	//}
	//defer rows.Close()
	//
	//all := []*Node{}
	//for rows.Next() {
	//	var l Link
	//	err := rows.Scan(&l.ID, &l.UserID, &l.Name, &l.URL, &l.Description,
	//		&l.FaviconURL, &l.Thumbnail, &l.Note, &l.Status, pq.Array(&l.Tags),
	//		&l.Usage, &l.Path, &l.Pinned)
	//	if err == nil {
	//		all = append(all, &Node{
	//			ID:          l.ID,
	//			Name:        l.Name,
	//			URL:         l.URL,
	//			Description: l.Description,
	//			FaviconURL:  l.FaviconURL,
	//			Thumbnail:   l.Thumbnail,
	//			Tags:        l.Tags,
	//			Pinned:      l.Pinned,
	//			Usage:       l.Usage,
	//			Path:        l.Path,
	//		})
	//	}
	//}
	//
	//root := buildTree(all)
	//writeJSON(w, root)
}

type Folder struct {
	ID        int64          `json:"id"`
	UserID    string         `json:"userId"`
	Name      string         `json:"name"`
	Code      string         `json:"code"` // сегмент
	Path      string         `json:"path"` // dot path из code-сегментов
	Open      bool           `json:"open"`
	Password  string         `json:"password,omitempty"`
	Color     string         `json:"color,omitempty"`
	Settings  map[string]any `json:"settings,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type FolderNode struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Icon     string `json:"icon"`
	Open     bool   `json:"open,omitempty"`
	Password string `json:"password,omitempty"`
	Color    string `json:"color,omitempty"`
	Code     string `json:"code,omitempty"`
	Path     string `json:"path"`
	Children []any  `json:"children,omitempty"`
}

func assembleTree(folders []Folder, links []Link) []any {

	nodes := make(map[string]*FolderNode)

	// 1. Создаём узлы для папок
	for _, f := range folders {
		nodes[f.Path] = &FolderNode{
			Type:     "folder",
			Name:     f.Name,
			Open:     f.Open,
			Password: f.Password,
			Color:    f.Color,
			Code:     f.Code,
			Path:     f.Path,
		}
	}

	// 2. Добавляем ссылки в папки по path
	for _, l := range links {
		if folder, ok := nodes[l.Path]; ok {
			folder.Children = append(folder.Children, map[string]any{
				"type":   "link",
				"name":   l.Name,
				"url":    l.URL,
				"pinned": l.Pinned,
				"usage":  l.Usage,
				"tags":   l.Tags,
			})
		}
	}

	// 3. Встраиваем папки в иерархию
	var root []any
	for _, f := range folders {
		parentPath := parentOf(f.Path)
		if parent, ok := nodes[parentPath]; ok {
			parent.Children = append(parent.Children, nodes[f.Path])
		} else {
			root = append(root, nodes[f.Path])
		}
	}

	return root
}

func parentOf(p string) string {
	i := strings.LastIndex(p, ".")
	if i == -1 {
		return ""
	}
	return p[:i]
}

func buildTree(all []*Node) []*Node {
	nodes := map[string]*Node{}
	var roots []*Node

	for _, n := range all {
		nodes[n.Path] = n
	}

	for _, n := range all {
		parts := strings.Split(n.Path, ".")
		if len(parts) > 1 {
			parentPath := strings.Join(parts[:len(parts)-1], ".")
			if parent, ok := nodes[parentPath]; ok {
				parent.Children = append(parent.Children, n)
				continue
			}
		}
		roots = append(roots, n)
	}
	return roots
}

// POST /api/v3/links/tree/update_path?userId=123
// Body: { "oldPath": "root.dev.go", "newPath": "root.backend.go" }

func handleUpdatePath(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		http.Error(w, "missing userId", 400)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	var req struct {
		OldPath string `json:"oldPath"`
		NewPath string `json:"newPath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if req.OldPath == "" || req.NewPath == "" {
		http.Error(w, "missing oldPath or newPath", 400)
		return
	}

	// Обновляем все элементы, у которых path начинается с oldPath
	query := `
		UPDATE links
		SET path = newpath
		FROM (
			SELECT id,
			       text2ltree(replace(ltree2text(path), $1, $2)) AS newpath
			FROM links
			WHERE user_id = $3 AND path <@ $1::ltree
		) AS sub
		WHERE links.id = sub.id;
	`

	_, err := db.Exec(query, req.OldPath, req.NewPath, userID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	writeJSON(w, map[string]any{
		"ok":        true,
		"movedFrom": req.OldPath,
		"movedTo":   req.NewPath,
	})
}

// DELETE /api/v3/links/tree/delete_branch?userId=123&path=root.dev
func handleDeleteBranch(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	path := r.URL.Query().Get("path")

	if userID == "" || path == "" {
		http.Error(w, "missing userId or path", 400)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", 405)
		return
	}

	query := `
		DELETE FROM links
		WHERE user_id = $1
		AND path <@ $2::ltree
	`

	res, err := db.Exec(query, userID, path)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	rows, _ := res.RowsAffected()
	writeJSON(w, map[string]any{
		"ok":      true,
		"deleted": rows,
		"path":    path,
	})
}

// DELETE /api/v3/links/delete?userId=123&url=https://example.com
func handleDeleteLink(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	url := r.URL.Query().Get("url")

	if userID == "" || url == "" {
		http.Error(w, "missing userId or url", 400)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", 405)
		return
	}

	query := `DELETE FROM links WHERE user_id=$1 AND url=$2`
	res, err := db.Exec(query, userID, url)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	rows, _ := res.RowsAffected()
	writeJSON(w, map[string]any{
		"ok":      true,
		"deleted": rows,
		"url":     url,
	})
}

// POST /api/v3/links/delete_bulk?userId=123
// Body: ["https://site1.com", "https://site2.com"]

func handleBulkDelete(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		http.Error(w, "missing userId", 400)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	var urls []string
	if err := json.NewDecoder(r.Body).Decode(&urls); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}

	if len(urls) == 0 {
		writeJSON(w, map[string]any{"ok": true, "deleted": 0})
		return
	}

	query := `DELETE FROM links WHERE user_id=$1 AND url = ANY($2)`
	res, err := db.Exec(query, userID, pq.Array(urls))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	rows, _ := res.RowsAffected()
	writeJSON(w, map[string]any{"ok": true, "deleted": rows, "count": len(urls)})
}

// POST /api/v3/links/add_bulk?userId=123
// Body: [{ "name": "...", "url": "...", ... }, {...}]

func handleBulkAdd(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		http.Error(w, "missing userId", 400)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	var links []Link
	if err := json.NewDecoder(r.Body).Decode(&links); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}
	if len(links) == 0 {
		writeJSON(w, map[string]any{"ok": true, "inserted": 0})
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	stmt, err := tx.Prepare(`
		INSERT INTO links (user_id, name, url, description, favicon_url, favicon_image, thumbnail,
		                   note, content, status, pinned, usage, tags, path, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (user_id, url) DO UPDATE SET
			name=EXCLUDED.name,
			description=EXCLUDED.description,
			favicon_url=EXCLUDED.favicon_url,
			favicon_image=EXCLUDED.favicon_image,
			thumbnail=EXCLUDED.thumbnail,
			note=EXCLUDED.note,
			content=EXCLUDED.content,
			status=EXCLUDED.status,
			pinned=EXCLUDED.pinned,
			usage=EXCLUDED.usage,
			tags=EXCLUDED.tags,
			path=EXCLUDED.path,
			updated_at=EXCLUDED.updated_at;
	`)
	if err != nil {
		tx.Rollback()
		http.Error(w, err.Error(), 500)
		return
	}
	defer stmt.Close()

	now := time.Now()
	for _, l := range links {
		if l.URL == "" {
			continue
		}
		if l.CreatedAt.IsZero() {
			l.CreatedAt = now
		}
		l.UpdatedAt = now
		_, err = stmt.Exec(userID, l.Name, l.URL, l.Description, l.FaviconURL,
			l.FaviconImage, l.Thumbnail, l.Note, l.Content, l.Status,
			l.Pinned, l.Usage, pq.Array(l.Tags), l.Path, l.CreatedAt, l.UpdatedAt)
		if err != nil {
			tx.Rollback()
			http.Error(w, err.Error(), 500)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	writeJSON(w, map[string]any{"ok": true, "inserted": len(links)})
}

func handleFolders(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		http.Error(w, "missing userId", 400)
		return
	}

	switch r.Method {
	case http.MethodGet:
		rows, err := db.Query(`
			SELECT id, user_id, name, code, path, open, password, color, settings, created_at, updated_at
			FROM folders WHERE user_id = $1 ORDER BY path ASC`, userID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()

		var out []Folder
		for rows.Next() {
			var f Folder
			var settingsRaw []byte
			if err := rows.Scan(
				&f.ID, &f.UserID, &f.Name, &f.Code, &f.Path,
				&f.Open, &f.Password, &f.Color, &settingsRaw, &f.CreatedAt, &f.UpdatedAt,
			); err == nil {
				if len(settingsRaw) > 0 {
					_ = json.Unmarshal(settingsRaw, &f.Settings)
				}
				out = append(out, f)
			}
		}
		writeJSON(w, out)

	case http.MethodPost:
		var f Folder
		if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		f.UserID = userID
		f.UpdatedAt = time.Now()
		if f.CreatedAt.IsZero() {
			f.CreatedAt = f.UpdatedAt
		}

		settingsJSON, _ := json.Marshal(f.Settings)

		_, err := db.Exec(`
			INSERT INTO folders (user_id, name, code, path, open, password, color, settings, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (user_id, path) DO UPDATE SET
				name=EXCLUDED.name, open=EXCLUDED.open, password=EXCLUDED.password,
				color=EXCLUDED.color, settings=EXCLUDED.settings, updated_at=EXCLUDED.updated_at
		`, f.UserID, f.Name, f.Code, f.Path, f.Open, f.Password, f.Color, settingsJSON, f.CreatedAt, f.UpdatedAt)

		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		writeJSON(w, map[string]any{"ok": true})

	default:
		http.Error(w, "method not allowed", 405)
	}
}

func handleFoldersSync(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		http.Error(w, "missing userId", 400)
		return
	}

	switch r.Method {
	case http.MethodGet:
		// экспорт всех папок
		rows, err := db.Query(`SELECT id, user_id, name, code, path, open, password, color, settings, created_at, updated_at
		                       FROM folders WHERE user_id = $1 ORDER BY path`, userID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		var out []Folder
		for rows.Next() {
			var f Folder
			var settingsRaw []byte
			if err := rows.Scan(
				&f.ID, &f.UserID, &f.Name, &f.Code, &f.Path, &f.Open,
				&f.Password, &f.Color, &settingsRaw, &f.CreatedAt, &f.UpdatedAt,
			); err == nil {
				if len(settingsRaw) > 0 {
					_ = json.Unmarshal(settingsRaw, &f.Settings)
				}
				out = append(out, f)
			}
		}
		writeJSON(w, out)
		return

	case http.MethodPost:
		// импорт/синк массива папок
		var folders []Folder
		if err := json.NewDecoder(r.Body).Decode(&folders); err != nil {
			http.Error(w, "invalid json", 400)
			return
		}
		if len(folders) == 0 {
			writeJSON(w, map[string]any{"ok": true, "count": 0})
			return
		}

		tx, err := db.Begin()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		stmt, err := tx.Prepare(`
		  INSERT INTO folders (user_id, name, code, path, open, password, color, settings, created_at, updated_at)
		  VALUES ($1,$2,$3,text2ltree($4),$5,$6,$7,$8,$9,$10)
		  ON CONFLICT (user_id, code) DO UPDATE SET
		    name=EXCLUDED.name,
		    path=EXCLUDED.path,
		    open=EXCLUDED.open,
		    password=EXCLUDED.password,
		    color=EXCLUDED.color,
		    settings=EXCLUDED.settings,
		    updated_at=EXCLUDED.updated_at;
		`)
		if err != nil {
			tx.Rollback()
			http.Error(w, err.Error(), 500)
			return
		}
		defer stmt.Close()

		now := time.Now()
		count := 0
		for _, f := range folders {
			// safety
			if f.Code == "" || f.Path == "" {
				continue
			}

			settingsRaw, _ := json.Marshal(f.Settings)
			created := f.CreatedAt
			if created.IsZero() {
				created = now
			}
			if _, err := stmt.Exec(
				userID, f.Name, f.Code, f.Path, f.Open, f.Password, f.Color, settingsRaw, created, now,
			); err != nil {
				tx.Rollback()
				http.Error(w, err.Error(), 500)
				return
			}
			count++
		}
		if err := tx.Commit(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		writeJSON(w, map[string]any{"ok": true, "count": count})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

// DELETE /api/v3/folders/delete_branch?userId=...&path=root.dev&withLinks=true
func handleFoldersDeleteBranch(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	path := r.URL.Query().Get("path")
	withLinks := r.URL.Query().Get("withLinks") == "true"

	if userID == "" || path == "" {
		http.Error(w, "missing userId or path", 400)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", 405)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var delLinks int64
	if withLinks {
		res, err := tx.Exec(`DELETE FROM links WHERE user_id=$1 AND path <@ $2::ltree`, userID, path)
		if err != nil {
			tx.Rollback()
			http.Error(w, err.Error(), 500)
			return
		}
		delLinks, _ = res.RowsAffected()
	}

	res, err := tx.Exec(`DELETE FROM folders WHERE user_id=$1 AND path <@ $2::ltree`, userID, path)
	if err != nil {
		tx.Rollback()
		http.Error(w, err.Error(), 500)
		return
	}
	delFolders, _ := res.RowsAffected()

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	writeJSON(w, map[string]any{"ok": true, "deletedFolders": delFolders, "deletedLinks": delLinks})
}

func handleFolderUpdate(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		http.Error(w, "missing userId", 400)
		return
	}

	var payload struct {
		Folder  Folder `json:"folder"`
		OldPath string `json:"oldPath"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	f := payload.Folder
	f.UserID = userID
	f.UpdatedAt = time.Now()
	settingsJSON, _ := json.Marshal(f.Settings)

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer tx.Rollback()

	// обновляем саму папку
	_, err = tx.Exec(`
		UPDATE folders SET
			name=$1, code=$2, path=$3, open=$4,
			password=$5, color=$6, settings=$7, updated_at=$8
		WHERE user_id=$9 AND path=$10
	`, f.Name, f.Code, f.Path, f.Open, f.Password, f.Color, settingsJSON, f.UpdatedAt, f.UserID, payload.OldPath)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// обновляем пути всех вложенных папок и ссылок
	_, err = tx.Exec(`
		UPDATE folders SET path = regexp_replace(path::text, $1, $2) WHERE user_id=$3 AND path <@ $1::ltree
	`, payload.OldPath, f.Path, f.UserID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	_, err = tx.Exec(`
		UPDATE links SET path = regexp_replace(path::text, $1, $2) WHERE user_id=$3 AND path <@ $1::ltree
	`, payload.OldPath, f.Path, f.UserID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	writeJSON(w, map[string]any{"ok": true})
}

func handleFolderDelete(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		http.Error(w, "missing userId", 400)
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM links WHERE user_id=$1 AND path <@ $2::ltree`, userID, req.Path)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	_, err = tx.Exec(`DELETE FROM folders WHERE user_id=$1 AND path <@ $2::ltree`, userID, req.Path)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	writeJSON(w, map[string]any{"ok": true})
}
