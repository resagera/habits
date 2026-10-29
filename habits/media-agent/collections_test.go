package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func catalogOf(t *testing.T, s *server) catalogData {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tv/api/catalog", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("каталог: %d", rec.Code)
	}
	var out catalogData
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func tileNamed(list []catTile, name string) *catTile {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

// Папка стоит в разделе своего содержимого, внутри у неё настоящие плитки, а
// «только в папке» убирает фильм из общего списка.
func TestCollectionTiles(t *testing.T) {
	s, root := testLibrary(t)
	catalogOf(t, s) // плитки разобраны, пути запомнены

	c := s.colls.create("Гарри Поттер")
	s.colls.update(c.ID, func(c *collection) {
		c.Members = []collMember{
			{Path: filepath.Join(root, "Film.mp4")},
			{Path: filepath.Join(root, "FilmDir"), Only: true},
		}
	})

	cat := catalogOf(t, s)
	folder := tileNamed(cat.Films, "Гарри Поттер")
	if folder == nil {
		t.Fatalf("папки нет среди фильмов: %+v", cat.Films)
	}
	if folder.Kind != "collection" || folder.Of != "film" || folder.Count != 2 {
		t.Fatalf("плитка папки: %+v", folder)
	}
	if len(folder.Items) != 2 || folder.Items[0].Name != "Film" || folder.Items[0].File == "" {
		t.Fatalf("внутри папки: %+v", folder.Items)
	}
	// порядок членов — порядок просмотра, менять его каталог не должен
	if folder.Items[1].Name != "FilmDir" {
		t.Fatalf("порядок внутри папки сбился: %+v", folder.Items)
	}
	if tileNamed(cat.Films, "FilmDir") != nil {
		t.Fatal("«только в папке» остался в общем списке")
	}
	if tileNamed(cat.Films, "Film") == nil {
		t.Fatal("фильм без галочки должен быть и в общем списке")
	}
}

// Папка из сериалов встаёт в раздел «Сериалы».
func TestCollectionOfSeries(t *testing.T) {
	s, root := testLibrary(t)
	catalogOf(t, s)
	c := s.colls.create("Трек")
	s.colls.update(c.ID, func(c *collection) {
		c.Members = []collMember{{Path: filepath.Join(root, "Show")}, {Path: filepath.Join(root, "Mini")}}
	})
	cat := catalogOf(t, s)
	if tileNamed(cat.Series, "Трек") == nil {
		t.Fatalf("папки нет среди сериалов: %+v", cat.Series)
	}
	if tileNamed(cat.Films, "Трек") != nil {
		t.Fatal("папка сериалов попала в фильмы")
	}
}

// Пропавший член молча выпадает, а не ломает папку.
func TestCollectionSkipsMissing(t *testing.T) {
	s, root := testLibrary(t)
	catalogOf(t, s)
	c := s.colls.create("Сборник")
	s.colls.update(c.ID, func(c *collection) {
		c.Members = []collMember{
			{Path: filepath.Join(root, "нет-такого.mp4")},
			{Path: filepath.Join(root, "Film.mp4")},
		}
	})
	folder := tileNamed(catalogOf(t, s).Films, "Сборник")
	if folder == nil || folder.Count != 1 || folder.Items[0].Name != "Film" {
		t.Fatalf("папка с пропавшим членом: %+v", folder)
	}
}

// Весь путь через админку: создать, набрать состав, поставить галочку,
// переименовать, записать описание, удалить.
func TestCollectionAdmin(t *testing.T) {
	s, root := adminServer(t)
	cat := catalogOf(t, s)
	film, dir := tileNamed(cat.Films, "Film"), tileNamed(cat.Films, "FilmDir")
	if film == nil || dir == nil {
		t.Fatalf("фильмов не нашлось: %+v", cat.Films)
	}

	code, out := adminCall(t, s, "POST", "/api/admin/collections", testPin, map[string]any{"name": "Фантастические твари"})
	if code != http.StatusOK {
		t.Fatalf("создание: %d %v", code, out)
	}
	id, _ := out["id"].(string)
	if id == "" {
		t.Fatalf("без id: %v", out)
	}

	// состав приходит целиком — вторым идёт тот, что только в папке
	code, out = adminCall(t, s, "POST", "/api/admin/collections/"+id, testPin, map[string]any{
		"set_members": true,
		"members": []map[string]any{
			{"id": film.ID, "only": false},
			{"id": dir.ID, "only": true},
		}})
	if code != http.StatusOK {
		t.Fatalf("состав: %d %v", code, out)
	}

	code, out = adminCall(t, s, "GET", "/api/admin/collections", testPin, nil)
	if code != http.StatusOK {
		t.Fatalf("список: %d %v", code, out)
	}
	var view struct {
		Collections []collView       `json:"collections"`
		Films       []collMemberView `json:"films"`
	}
	raw, _ := json.Marshal(out)
	_ = json.Unmarshal(raw, &view)
	if len(view.Collections) != 1 || len(view.Collections[0].Members) != 2 {
		t.Fatalf("состав не сохранился: %+v", view.Collections)
	}
	if !view.Collections[0].Members[1].Only {
		t.Fatalf("галочка «только в папке» не сохранилась: %+v", view.Collections[0].Members)
	}
	// список для набора — полный, вместе со спрятанным: иначе галочку не снять
	if len(view.Films) != 2 {
		t.Fatalf("в наборе должны быть все фильмы: %+v", view.Films)
	}

	// имя
	if code, out = adminCall(t, s, "POST", "/api/admin/collections/"+id, testPin,
		map[string]any{"name": "Твари"}); code != http.StatusOK {
		t.Fatalf("переименование: %d %v", code, out)
	}
	if tileNamed(catalogOf(t, s).Films, "Твари") == nil {
		t.Fatal("новое имя не доехало до каталога")
	}

	// описание пишется руками, поиск в интернете для папки запрещён
	if code, _ = adminCall(t, s, "POST", "/api/admin/info", testPin,
		map[string]any{"id": id, "fetch": true}); code != http.StatusBadRequest {
		t.Fatalf("поиск описания для папки должен отказывать: %d", code)
	}
	if code, out = adminCall(t, s, "POST", "/api/admin/info", testPin, map[string]any{
		"id": id, "info": map[string]any{"description": "Пять фильмов про Ньюта"}}); code != http.StatusOK {
		t.Fatalf("описание: %d %v", code, out)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tv/api/info?id="+id, nil))
	var info struct {
		Info *itemInfo `json:"info"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &info)
	if info.Info == nil || info.Info.Description != "Пять фильмов про Ньюта" {
		t.Fatalf("описание папки не читается: %s", rec.Body.String())
	}

	// обложка живёт в кэше: своего места на диске у папки нет
	cover := s.collCoverPath(id)
	if err := os.MkdirAll(filepath.Dir(cover), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cover, []byte("jpg"), 0o600); err != nil {
		t.Fatal(err)
	}
	folder := tileNamed(catalogOf(t, s).Films, "Твари")
	if folder == nil || !strings.Contains(folder.Poster, id) {
		t.Fatalf("своя обложка не подхватилась: %+v", folder)
	}

	// удаление папки не трогает фильмы, спрятанный возвращается в общий список
	if code, out = adminCall(t, s, "DELETE", "/api/admin/collections/"+id, testPin, nil); code != http.StatusOK {
		t.Fatalf("удаление: %d %v", code, out)
	}
	cat = catalogOf(t, s)
	if tileNamed(cat.Films, "FilmDir") == nil || tileNamed(cat.Films, "Film") == nil {
		t.Fatalf("фильмы пропали вместе с папкой: %+v", cat.Films)
	}
	if exists(cover) {
		t.Fatal("обложка удалённой папки осталась в кэше")
	}
	_ = root
}

// Переименование в админке тянет за собой состав папки: иначе фильм молча
// выпал бы из неё.
func TestCollectionFollowsRename(t *testing.T) {
	s, root := adminServer(t)
	cat := catalogOf(t, s)
	film := tileNamed(cat.Films, "Film")
	if film == nil {
		t.Fatal("фильма нет")
	}
	c := s.colls.create("Сага")
	s.colls.update(c.ID, func(c *collection) {
		c.Members = []collMember{{Path: filepath.Join(root, "Film.mp4")}}
	})
	if code, out := adminCall(t, s, "POST", "/api/admin/rename", testPin,
		map[string]any{"id": film.ID, "name": "Фильм (2019)"}); code != http.StatusOK {
		t.Fatalf("переименование: %d %v", code, out)
	}
	folder := tileNamed(catalogOf(t, s).Films, "Сага")
	if folder == nil || folder.Count != 1 {
		t.Fatalf("после переименования папка опустела: %+v", folder)
	}
	if folder.Items[0].Name != "Фильм (2019)" {
		t.Fatalf("внутри папки старое имя: %+v", folder.Items)
	}
}
