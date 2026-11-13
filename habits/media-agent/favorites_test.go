package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func setFav(t *testing.T, s *server, id string, on bool) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"id": id, "on": on})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/tv/api/favorite", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("отметка %s=%v: %d %s", id, on, rec.Code, rec.Body.String())
	}
}

func names(list []catTile) []string {
	out := make([]string, 0, len(list))
	for _, t := range list {
		out = append(out, t.Name)
	}
	return out
}

// Отмеченное встаёт в начало СВОЕГО раздела, остальной порядок не меняется.
func TestFavoritesFirst(t *testing.T) {
	s, _ := testLibrary(t)
	cat := catalogOf(t, s)
	if got := names(cat.Films); len(got) != 2 || got[0] != "Film" {
		t.Fatalf("исходный порядок фильмов: %v", got)
	}
	if got := names(cat.Series); len(got) != 2 || got[0] != "Mini" {
		t.Fatalf("исходный порядок сериалов: %v", got)
	}

	// второй по алфавиту фильм — в избранное
	dir := tileNamed(cat.Films, "FilmDir")
	setFav(t, s, dir.ID, true)
	// и второй сериал
	show := tileNamed(cat.Series, "Show")
	setFav(t, s, show.ID, true)

	cat = catalogOf(t, s)
	if got := names(cat.Films); got[0] != "FilmDir" || got[1] != "Film" {
		t.Fatalf("избранный фильм не первый: %v", got)
	}
	if !tileNamed(cat.Films, "FilmDir").Fav || tileNamed(cat.Films, "Film").Fav {
		t.Fatalf("отметка проставлена не тому: %+v", cat.Films)
	}
	if got := names(cat.Series); got[0] != "Show" || got[1] != "Mini" {
		t.Fatalf("избранный сериал не первый: %v", got)
	}
	// разделы не перемешались
	if tileNamed(cat.Series, "FilmDir") != nil || tileNamed(cat.Films, "Show") != nil {
		t.Fatal("плитка уехала в чужой раздел")
	}

	setFav(t, s, dir.ID, false)
	if got := names(catalogOf(t, s).Films); got[0] != "Film" {
		t.Fatalf("отметка не снялась: %v", got)
	}
}

// Отметка переживает перезапуск агента: она на диске, а не в памяти.
func TestFavoritesSurviveRestart(t *testing.T) {
	s, _ := testLibrary(t)
	film := tileNamed(catalogOf(t, s).Films, "Film")
	setFav(t, s, film.ID, true)

	again := newFavStore(s.cfg.cache)
	path, _ := agentStore.pathOf(film.ID)
	if !again.has(path) {
		t.Fatal("отметка не сохранилась на диск")
	}
}

// Переименование в админке не сбрасывает отметку: ключ — путь, и он едет следом.
func TestFavoritesFollowRename(t *testing.T) {
	s, _ := adminServer(t)
	film := tileNamed(catalogOf(t, s).Films, "Film")
	setFav(t, s, film.ID, true)
	if code, out := adminCall(t, s, "POST", "/api/admin/rename", testPin,
		map[string]any{"id": film.ID, "name": "Фильм (2019)"}); code != http.StatusOK {
		t.Fatalf("переименование: %d %v", code, out)
	}
	moved := tileNamed(catalogOf(t, s).Films, "Фильм (2019)")
	if moved == nil || !moved.Fav {
		t.Fatalf("после переименования отметка пропала: %+v", moved)
	}
}

// У папки-подборки своего пути нет — ключом идёт её id; внутри папки
// звёздочки тоже проставляются.
func TestFavoritesForCollection(t *testing.T) {
	s, root := adminServer(t)
	catalogOf(t, s)
	c := s.colls.create("Сага")
	s.colls.update(c.ID, func(c *collection) {
		c.Members = []collMember{
			{Path: filepath.Join(root, "Film.mp4")},
			{Path: filepath.Join(root, "FilmDir")},
		}
	})
	setFav(t, s, c.ID, true)
	inner := tileNamed(catalogOf(t, s).Films, "FilmDir")
	setFav(t, s, inner.ID, true)

	cat := catalogOf(t, s)
	// обе отметки впереди неотмеченного, между собой — по названию
	if got := names(cat.Films); got[2] != "Film" {
		t.Fatalf("неотмеченное не ушло в конец: %v", got)
	}
	folder := tileNamed(cat.Films, "Сага")
	if folder == nil || !folder.Fav {
		t.Fatalf("папка не отмечена: %+v", folder)
	}
	if len(folder.Items) != 2 || folder.Items[0].Name != "FilmDir" || !folder.Items[0].Fav {
		t.Fatalf("внутри папки отметка не видна или порядок не тот: %+v", folder.Items)
	}

	// папку убрали через админку — отметка не должна остаться висеть
	if !s.favs.has(c.ID) {
		t.Fatal("отметка папки не записалась")
	}
	if code, out := adminCall(t, s, "DELETE", "/api/admin/collections/"+c.ID, testPin, nil); code != http.StatusOK {
		t.Fatalf("удаление папки: %d %v", code, out)
	}
	if s.favs.has(c.ID) {
		t.Fatal("отметка удалённой папки осталась")
	}
}
