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
	Children    []*Node  `json:"children,omitempty"`
}

var db *sql.DB

func main() {
	var err error
	connStr := "postgres://habits:habits5432@localhost:5432/habits?sslmode=disable"
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/api/habits/links", handleLinks)
	http.HandleFunc("/api/habits/links/search", handleSearchLinks)
	http.HandleFunc("/api/habits/links/sync", handleLinksSync)
	http.HandleFunc("/api/habits/links/folder", handleFolderLinks)
	http.HandleFunc("/api/habits/links/children", handleChildrenLinks)
	http.HandleFunc("/api/habits/links/tree", handleLinksTree)
	http.HandleFunc("/api/habits/links/tree/update_path", handleUpdatePath)
	http.HandleFunc("/api/habits/links/tree/delete_branch", handleDeleteBranch)
	http.HandleFunc("/api/habits/links/delete", handleDeleteLink)
	http.HandleFunc("/api/habits/links/delete_bulk", handleBulkDelete)
	http.HandleFunc("/api/habits/links/add_bulk", handleBulkAdd)

	fmt.Println("API started on :8676")
	log.Fatal(http.ListenAndServe(":8676", nil))
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

func handleLinksSync(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		http.Error(w, "missing userId", 400)
		return
	}

	switch r.Method {
	case http.MethodGet:
		rows, err := db.Query("SELECT * FROM links WHERE user_id=$1", userID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		var links []Link
		for rows.Next() {
			var l Link
			rows.Scan(&l.ID, &l.UserID, &l.Name, &l.URL, &l.Description, &l.FaviconURL,
				&l.FaviconImage, &l.Thumbnail, &l.Note, &l.Content, &l.Status,
				&l.Pinned, &l.Usage, pq.Array(&l.Tags), &l.Path, &l.CreatedAt, &l.UpdatedAt)
			links = append(links, l)
		}
		writeJSON(w, links)

	case http.MethodPost:
		var links []Link
		if err := json.NewDecoder(r.Body).Decode(&links); err != nil {
			http.Error(w, err.Error(), 400)
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
				name=EXCLUDED.name, description=EXCLUDED.description,
				favicon_url=EXCLUDED.favicon_url, thumbnail=EXCLUDED.thumbnail,
				tags=EXCLUDED.tags, path=EXCLUDED.path, updated_at=EXCLUDED.updated_at;
		`)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer stmt.Close()

		for _, l := range links {
			_, err := stmt.Exec(userID, l.Name, l.URL, l.Description, l.FaviconURL,
				l.FaviconImage, l.Thumbnail, l.Note, l.Content, l.Status,
				l.Pinned, l.Usage, pq.Array(l.Tags), l.Path, l.CreatedAt, time.Now())
			if err != nil {
				tx.Rollback()
				http.Error(w, err.Error(), 500)
				return
			}
		}
		tx.Commit()
		writeJSON(w, map[string]any{"ok": true, "count": len(links)})
	default:
		http.Error(w, "method not allowed", 405)
	}
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

	rows, err := db.Query(`
		SELECT id, user_id, name, url, description, favicon_url, thumbnail, note, status, tags, usage, path, pinned
		FROM links
		WHERE user_id = $1
		ORDER BY path ASC
	`, userID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	all := []*Node{}
	for rows.Next() {
		var l Link
		err := rows.Scan(&l.ID, &l.UserID, &l.Name, &l.URL, &l.Description,
			&l.FaviconURL, &l.Thumbnail, &l.Note, &l.Status, pq.Array(&l.Tags),
			&l.Usage, &l.Path, &l.Pinned)
		if err == nil {
			all = append(all, &Node{
				ID:          l.ID,
				Name:        l.Name,
				URL:         l.URL,
				Description: l.Description,
				FaviconURL:  l.FaviconURL,
				Thumbnail:   l.Thumbnail,
				Tags:        l.Tags,
				Pinned:      l.Pinned,
				Usage:       l.Usage,
				Path:        l.Path,
			})
		}
	}

	root := buildTree(all)
	writeJSON(w, root)
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
