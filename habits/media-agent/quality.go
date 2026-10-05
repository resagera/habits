package main

// «Что стоит пересмотреть» — разбор медиатеки по тому, как она записана:
// низкое разрешение, одна звуковая дорожка, формат, который приставка не
// играет как есть.
//
// Обход идёт по ПЛИТКАМ, а не по файлам: у сериала разбирается первая серия —
// остальные той же раздачи устроены так же, а ffprobe по восьми тысячам файлов
// занял бы минуты ради того же ответа. Разбор кэшируется (probe.json), поэтому
// второй заход мгновенный.
//
// Сами фильтры считает страница: список небольшой (плиток десятки), а
// перебирать условия кнопками проще, чем гонять запрос на каждое.

import (
	"net/http"
	"os"
	"path/filepath"
)

type qualityItem struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Kind     string     `json:"kind"`
	Path     string     `json:"path"`
	File     string     `json:"file"`               // по какому файлу судим
	Episodes int        `json:"episodes,omitempty"` // сколько серий у сериала
	Size     int64      `json:"size"`               // весь элемент на диске
	IsDir    bool       `json:"is_dir"`
	Quality  int        `json:"quality"` // «сколько p», приведённое к 16:9
	Info     *mediaInfo `json:"info"`
}

// quality16x9 — то самое «720p» у широкоэкранного кино. У фильма 2,35:1 кадр
// 1280×546: по высоте это «546p», хотя рядом с обычным 1280×720 он ничем не
// хуже. Сравнивать надо по длинной стороне, приведённой к 16:9, иначе всё
// широкоэкранное разом попадает в «низкое качество».
func quality16x9(w, h int) int {
	if w <= 0 || h <= 0 {
		return h
	}
	return max(h, w*9/16)
}

func (s *server) qualityScan() []qualityItem {
	films, series := s.rawVideoTiles()
	out := []qualityItem{}
	for _, t := range append(append([]catTile{}, films...), series...) {
		p, ok := agentStore.pathOf(t.ID)
		if !ok {
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		file, episodes := p, 0
		if st.IsDir() {
			vids := videosUnder(p)
			if len(vids) == 0 {
				continue
			}
			file, episodes = vids[0], len(vids)
		}
		fst, err := os.Stat(file)
		if err != nil {
			continue
		}
		info := agentStore.probe(file, fst)
		if info == nil || info.VCodec == "" {
			continue
		}
		out = append(out, qualityItem{ID: t.ID, Name: t.Name, Kind: t.Kind, Path: p,
			File: filepath.Base(file), Episodes: episodes, Size: pathSize(p),
			IsDir: st.IsDir(), Quality: quality16x9(info.Width, info.Height), Info: info})
	}
	return out
}

// GET /api/admin/quality
func (s *server) adminQuality(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.qualityScan()})
}
