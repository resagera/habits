package main

// Материалы фильма или сериала: свои снимки и постеры (промо, постеры
// сезонов, фото со съёмок), которые показываются в карточке «Инфо» и на
// паузе — слайд-шоу (pauseshow.go).
//
// Лежат в библиотеке, а не в кэше агента: это то, что добавили руками, и
// терять его при переустановке нельзя. Папка скрытая — обход файлов, каталог
// и «Файлы» её не видят:
//   - у сериала или фильма в своей папке — «<папка>/.materials/»; едет вместе
//     с папкой при переименовании, переезде и в корзину;
//   - у фильма одним файлом — «.materials/<имя фильма>/» рядом с ним;
//     переименование и корзина переносят её отдельно.

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	materialsDirName = ".materials"
	materialMax      = 1920 // сторона картинки: хватает на весь экран
)

func materialsDir(p string, isDir bool) string {
	if isDir {
		return filepath.Join(p, materialsDirName)
	}
	return filepath.Join(filepath.Dir(p), materialsDirName, stemOf(p))
}

func materialsDirOf(p string) string {
	st, err := os.Stat(p)
	return materialsDir(p, err == nil && st.IsDir())
}

type materialRef struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// materialNames — имена картинок по порядку добавления.
func materialNames(p string) []string {
	list, err := os.ReadDir(materialsDirOf(p))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range list {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jpg") && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func (s *server) materialsOf(id, p string) []materialRef {
	out := []materialRef{}
	for _, n := range materialNames(p) {
		out = append(out, materialRef{Name: n, URL: s.cfg.base + "/material/" + id + "?k=" + s.sign(id) + "&n=" + n})
	}
	return out
}

// validMaterialName — имя картинки без путей: «m-1731500000000.jpg».
func validMaterialName(n string) bool {
	return n != "" && n == filepath.Base(n) && strings.HasSuffix(n, ".jpg") && !strings.HasPrefix(n, ".")
}

// GET /material/{id}?k=&n= — картинка материала плитки id.
func (s *server) material(w http.ResponseWriter, r *http.Request) {
	id, n := r.PathValue("id"), r.URL.Query().Get("n")
	if !s.validKey(id, r.URL.Query().Get("k")) || !validMaterialName(n) {
		http.Error(w, "нет доступа", http.StatusForbidden)
		return
	}
	p, ok := agentStore.pathOf(id)
	if !ok || !s.inRoots(p) || !s.visible(p, r) {
		http.NotFound(w, r)
		return
	}
	f := filepath.Join(materialsDirOf(p), n)
	if !fileReady(f) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, f)
}

// GET /api/materials?id= — материалы плитки (для «Инфо» и админки).
func (s *server) materials(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	p, ok := agentStore.pathOf(id)
	if !ok || !s.inRoots(p) || !s.visible(p, r) {
		writeJSON(w, http.StatusOK, map[string]any{"items": []materialRef{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.materialsOf(id, p)})
}

// addMaterial кладёт картинку src в материалы плитки p.
func addMaterial(src, p string) (string, error) {
	dir := materialsDirOf(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("m-%d.jpg", time.Now().UnixNano()/1e6)
	for exists(filepath.Join(dir, name)) {
		time.Sleep(time.Millisecond)
		name = fmt.Sprintf("m-%d.jpg", time.Now().UnixNano()/1e6)
	}
	if err := renderJPEG(src, filepath.Join(dir, name), materialMax, materialMax, 88); err != nil {
		return "", errors.New("картинка не читается (годятся JPG и PNG): " + err.Error())
	}
	return name, nil
}

// materialTarget — плитка фильма или сериала по id из админки.
func (s *server) materialTarget(w http.ResponseWriter, id string) (string, bool) {
	p, ok := s.adminPath(w, id)
	if !ok {
		return "", false
	}
	if !isVideoLib(s.libraryOf(p).Kind) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "материалы бывают у фильмов и сериалов"})
		return "", false
	}
	return p, true
}

// DELETE /api/admin/materials?id=&n= — убрать картинку.
func (s *server) adminMaterialDelete(w http.ResponseWriter, r *http.Request) {
	p, ok := s.materialTarget(w, r.URL.Query().Get("id"))
	if !ok {
		return
	}
	n := r.URL.Query().Get("n")
	if !validMaterialName(n) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверное имя"})
		return
	}
	dir := materialsDirOf(p)
	if err := os.Remove(filepath.Join(dir, n)); err != nil && !os.IsNotExist(err) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = os.Remove(dir) // пустую папку — тоже; непустую Remove не тронет
	if parent := filepath.Dir(dir); filepath.Base(parent) == materialsDirName {
		_ = os.Remove(parent)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// moveFileMaterials — материалы фильма-файла едут за ним при переименовании.
// У папки они внутри неё и едут сами.
func moveFileMaterials(old, neu string) {
	from, to := materialsDir(old, false), materialsDir(neu, false)
	if !exists(from) || exists(to) {
		return
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err == nil {
		_ = os.Rename(from, to)
		_ = os.Remove(filepath.Dir(from))
	}
}
