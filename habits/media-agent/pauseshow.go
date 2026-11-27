package main

// Слайд-шоу на паузе: видео стоит дольше заданного (по умолчанию 10 минут) —
// экран не замирает на одном кадре, а листает картинки. Любая кнопка
// убирает их, видео остаётся на паузе.
//
// Что показывать: свои материалы фильма или сериала (materials.go) вместе с
// постером, если они есть; иначе — снимки выбранной в админке папки «Фото и
// видео»; нет и её — кадры и постер. Выбор делает агент, страница получает
// готовый список.

import (
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
)

// GET /api/pauseshow?id=серия|фильм — картинки для паузы.
func (s *server) pauseShow(w http.ResponseWriter, r *http.Request) {
	v := s.looks.get()
	out := map[string]any{"on": v.pauseOn(), "minutes": v.pauseMinutes(), "source": "", "urls": []string{}}
	if !v.pauseOn() {
		writeJSON(w, http.StatusOK, out)
		return
	}
	var urls []string
	source := ""
	p, ok := agentStore.pathOf(r.URL.Query().Get("id"))
	tile := ""
	if ok && s.inRoots(p) && isVideoLib(s.libraryOf(p).Kind) {
		tile = s.seriesDirOf(p)
	}
	poster := func() []string {
		id := s.remember(tile)
		if s.existingPoster(tile, id) == "" {
			return nil
		}
		kind := "film"
		if st, err := os.Stat(tile); err == nil && st.IsDir() {
			kind = "series"
		}
		return []string{s.posterURL(id, kind)}
	}
	if tile != "" {
		for _, m := range s.materialsOf(s.remember(tile), tile) {
			urls = append(urls, m.URL)
		}
		if len(urls) > 0 {
			rand.Shuffle(len(urls), func(i, j int) { urls[i], urls[j] = urls[j], urls[i] })
			urls = append(poster(), urls...)
			source = "materials"
		}
	}
	if len(urls) == 0 && v.PauseAlbum != "" {
		if list := s.albumBackgrounds(v.PauseAlbum); len(list) > 0 {
			urls, source = list, "album:"+filepath.Base(v.PauseAlbum)
		}
	}
	if len(urls) == 0 && tile != "" {
		id := s.remember(tile)
		for _, n := range s.frameNames(id) {
			urls = append(urls, s.cfg.base+"/frame/"+id+"/"+n+"?k="+s.sign(id))
		}
		urls = append(poster(), urls...)
		if len(urls) > 0 {
			source = "frames"
		}
	}
	if urls == nil {
		urls = []string{}
	}
	out["urls"], out["source"] = urls, source
	writeJSON(w, http.StatusOK, out)
}
