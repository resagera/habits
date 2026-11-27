package main

// Режим «Файлы»: папка целиком — не только то, что играется, но и снимки,
// PDF, тексты, архивы. Сведения о файлах берутся из индекса (fileindex.go),
// а не читаются в момент открытия: открытие папки остаётся чтением каталога.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// browseAll — содержимое папки для режима «Файлы». Медиа идут тем же путём,
// что и раньше (с разбором и вердиктом «заиграет ли»), остальное
// дописывается сверху. Служебное рядом с фильмом — постер «Имя.jpg» и
// описание «Имя.info.json» — не показываем: это часть плитки, а не файл
// медиатеки. В библиотеке фото такой «постер» — обычный снимок рядом с
// одноимённым роликом (живые фото с телефона), его прятать нельзя.
func (s *server) browseAll(dir string) ([]entry, error) {
	media, err := agentStore.browse(dir, s.q.ready)
	if err != nil {
		return nil, err
	}
	list, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	photoLib := s.libraryOf(dir).Kind == "photo"
	bases := map[string]bool{} // имена без расширения у видео и папок
	for _, it := range list {
		n := it.Name()
		if it.IsDir() {
			bases[n] = true
		} else if isMedia(n) {
			bases[strings.TrimSuffix(n, filepath.Ext(n))] = true
		}
	}
	out := media
	for _, it := range list {
		n := it.Name()
		if strings.HasPrefix(n, ".") || it.IsDir() || isMedia(n) || !it.Type().IsRegular() {
			continue
		}
		if strings.HasSuffix(n, infoSuffix) {
			continue
		}
		ext := strings.ToLower(filepath.Ext(n))
		if !photoLib && isPosterExt(ext) && bases[strings.TrimSuffix(n, filepath.Ext(n))] {
			continue
		}
		if !photoLib && (n == "poster.jpg" || n == "folder.jpg" || n == "cover.jpg") {
			continue
		}
		st, err := it.Info()
		if err != nil {
			continue
		}
		full := filepath.Join(dir, n)
		id := shortID(full)
		agentStore.rememberPath(id, full)
		out = append(out, entry{ID: id, Name: n, Size: st.Size()})
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].IsDir != out[b].IsDir {
			return out[a].IsDir
		}
		return naturalLess(out[a].Name, out[b].Name)
	})
	return out, nil
}

func isPosterExt(ext string) bool {
	for _, e := range posterExts {
		if e == ext {
			return true
		}
	}
	return false
}

// decorate дописывает заголовки, вид файла и то, что знает индекс. Сведения
// индекса отдаются, только если файл с тех пор не менялся: иначе под
// заменённым файлом стояло бы разрешение прежнего.
func (s *server) decorate(items []entry) {
	for i := range items {
		p, ok := agentStore.pathOf(items[i].ID)
		if !ok {
			continue
		}
		m := s.meta.get(p)
		items[i].Title, items[i].Tags = m.Title, m.Tags
		if items[i].IsDir {
			if d, ok := s.index.dir(p); ok {
				d := d
				items[i].Dir = &d
			}
			continue
		}
		items[i].Kind = fileKind(items[i].Name)
		if rec, ok := s.index.get(p); ok && rec.Size == items[i].Size {
			rec := rec
			items[i].X = &rec
		}
	}
}

// viewURLs — чем открыть не играющийся файл.
func (s *server) viewURLs(o *outItem) {
	kind := o.Kind
	if kind == "" {
		kind = fileKind(o.Name)
	}
	p, ok := agentStore.pathOf(o.ID)
	if !ok {
		return
	}
	switch kind {
	case "photo":
		ext := strings.ToLower(filepath.Ext(p))
		if photoExt[ext] {
			o.View, o.Thumb = s.photoURL(p, "view"), s.photoURL(p, "thumb")
		}
	case "pdf":
		o.View = s.cfg.base + "/pdf/" + o.ID + "?k=" + s.sign(o.ID)
	case "text":
		o.View = s.cfg.base + "/text/" + o.ID + "?k=" + s.sign(o.ID)
	}
}
