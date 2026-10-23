package main

// Сборка альбомов: новая папка и перенос треков между папками.
//
// Флоу в админке: создать альбом (или папку внутри него), открыть любую
// другую папку, послушать трек и добавить его копией или перенести. Поэтому
// здесь всего две операции: mkdir и transfer.
//
// Перенос в пределах одного диска — это rename, то есть мгновенно и без
// второго экземпляра файла. Между дисками — копия и удаление исходника, но
// только после успешной копии: оборвалось — исходник на месте.

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// POST /api/admin/mkdir {name, id?, lib?} — новая папка: внутри папки id или
// в корне библиотеки lib (по умолчанию первой музыкальной).
func (s *server) adminMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		ID   string `json:"id"`
		Lib  string `json:"lib"`
		Kind string `json:"kind"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужно имя"})
		return
	}
	name, err := cleanName(req.Name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	parent := ""
	if req.ID != "" {
		p, ok := s.adminPath(w, req.ID)
		if !ok {
			return
		}
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "папку можно создать только внутри папки"})
			return
		}
		parent = p
	} else {
		kind := req.Kind
		if kind == "" {
			kind = "music"
		}
		lib := s.libraryByKind(kind, req.Lib)
		if lib.Path == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "в настройках агента нет такой библиотеки"})
			return
		}
		parent = lib.Path
	}
	dir := filepath.Join(parent, name)
	if exists(dir) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "«" + name + "» уже есть"})
		return
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	log.Printf("админка: создана папка %s", dir)
	writeJSON(w, http.StatusOK, map[string]any{"id": s.remember(dir), "name": name, "path": dir})
}

// POST /api/admin/transfer {ids, to, move} — копировать или перенести файлы в
// папку. Копия и перенос — одна кнопка в двух видах, поэтому и запрос один.
func (s *server) adminTransfer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs  []string `json:"ids"`
		To   string   `json:"to"`
		Move bool     `json:"move"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req) != nil || len(req.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужны ids и to"})
		return
	}
	dir, ok := s.adminPath(w, req.To)
	if !ok {
		return
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "переносить можно только в папку"})
		return
	}
	done, skipped := 0, []string{}
	for _, id := range req.IDs {
		src, okSrc := agentStore.pathOf(id)
		if !okSrc || !s.inRoots(src) || strings.Contains(src, string(filepath.Separator)+trashDirName) {
			skipped = append(skipped, "не найдено")
			continue
		}
		st, err := os.Stat(src)
		if err != nil || st.IsDir() {
			skipped = append(skipped, filepath.Base(src)+": это папка")
			continue
		}
		if filepath.Dir(src) == dir {
			skipped = append(skipped, filepath.Base(src)+": уже здесь")
			continue
		}
		dst := freeName(filepath.Join(dir, filepath.Base(src)))
		if err := transferFile(src, dst, req.Move); err != nil {
			skipped = append(skipped, filepath.Base(src)+": "+err.Error())
			continue
		}
		s.remember(dst)
		if req.Move {
			// место в «Продолжить просмотр» переезжает вместе с файлом
			s.moveProgress(src, dst, false)
		}
		done++
	}
	log.Printf("админка: %s %d файлов в %s", map[bool]string{true: "перенесено", false: "скопировано"}[req.Move], done, dir)
	writeJSON(w, http.StatusOK, map[string]any{"done": done, "skipped": skipped, "move": req.Move})
}

func transferFile(src, dst string, move bool) error {
	if move {
		if err := os.Rename(src, dst); err == nil {
			return nil
		}
		// разные диски: копируем и только потом убираем исходник
		if err := copyFile(src, dst); err != nil {
			return err
		}
		if err := os.Remove(src); err != nil {
			return errors.New("скопировано, но исходник остался: " + err.Error())
		}
		return nil
	}
	return copyFile(src, dst)
}
