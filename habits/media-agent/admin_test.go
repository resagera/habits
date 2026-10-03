package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testPin = "4321"

func adminServer(t *testing.T) (*server, string) {
	t.Helper()
	s, root := testLibrary(t)
	s.cfg.adminPin = testPin
	s.cfg.trashDays = 3
	// постер рядом с сериалом — должен ездить вместе с ним
	if err := os.WriteFile(filepath.Join(root, "Show.jpg"), []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	adminGuard.mu.Lock()
	adminGuard.fails, adminGuard.until = 0, time.Time{}
	adminGuard.mu.Unlock()
	return s, root
}

func adminCall(t *testing.T, s *server, method, url, pin string, body any) (int, map[string]any) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		rd = bytes.NewReader(data)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, "/tv"+url, rd)
	req.Header.Set("X-Admin-Pin", pin)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestAdminPin(t *testing.T) {
	s, _ := adminServer(t)
	if code, _ := adminCall(t, s, "GET", "/api/admin/check", testPin, nil); code != http.StatusOK {
		t.Fatalf("верный PIN: %d", code)
	}
	for i := 0; i < adminMaxFails; i++ {
		if code, _ := adminCall(t, s, "GET", "/api/admin/check", "0000", nil); code != http.StatusUnauthorized {
			t.Fatalf("неверный PIN %d: %d", i+1, code)
		}
	}
	// после пяти неверных — пауза даже для верного PIN
	if code, _ := adminCall(t, s, "GET", "/api/admin/check", testPin, nil); code != http.StatusTooManyRequests {
		t.Fatalf("после перебора: %d, ожидалась пауза", code)
	}
	s.cfg.adminPin = ""
	if code, out := adminCall(t, s, "GET", "/api/admin/check", "", nil); code != http.StatusForbidden || out["code"] != "disabled" {
		t.Fatalf("без PIN в настройках админка должна быть выключена: %d %v", code, out)
	}
}

func TestCleanName(t *testing.T) {
	for _, bad := range []string{"", "  ", "a/b", "..", ".hidden", "x\\y"} {
		if _, err := cleanName(bad); err == nil {
			t.Errorf("имя %q должно быть отвергнуто", bad)
		}
	}
	if n, err := cleanName("  Fauda (2015) "); err != nil || n != "Fauda (2015)" {
		t.Errorf("обычное имя: %q %v", n, err)
	}
}

// Переименование сериала: папка, постер рядом и запись «Продолжить просмотр»
// переезжают вместе; запись находит ту же серию по новому пути.
func TestAdminRenameSeries(t *testing.T) {
	s, root := adminServer(t)
	save(t, s, filepath.Join(root, "Show", "S01", "E02.mp4"), 600, 2400)
	code, out := adminCall(t, s, "POST", "/api/admin/rename", testPin,
		map[string]string{"id": s.remember(filepath.Join(root, "Show")), "name": "Show (2020)"})
	if code != http.StatusOK {
		t.Fatalf("переименование: %d %v", code, out)
	}
	if !exists(filepath.Join(root, "Show (2020)", "S01", "E02.mp4")) || exists(filepath.Join(root, "Show")) {
		t.Fatal("папка не переименовалась")
	}
	if !exists(filepath.Join(root, "Show (2020).jpg")) || exists(filepath.Join(root, "Show.jpg")) {
		t.Fatal("постер не поехал вместе с сериалом")
	}
	entries := list(t, s)
	if len(entries) != 1 || entries[0].Title != "Show (2020)" || entries[0].Episode != "E02" || entries[0].Position != 600 {
		t.Fatalf("запись после переименования: %+v", entries)
	}
	if entries[0].Key != s.remember(filepath.Join(root, "Show (2020)")) {
		t.Fatalf("ключ записи не пересчитался: %s", entries[0].Key)
	}
}

// Фильм-файл: расширение сохраняется само, если его не написали.
func TestAdminRenameFilmKeepsExt(t *testing.T) {
	s, root := adminServer(t)
	save(t, s, filepath.Join(root, "Film.mp4"), 900, 5400)
	code, out := adminCall(t, s, "POST", "/api/admin/rename", testPin,
		map[string]string{"id": s.remember(filepath.Join(root, "Film.mp4")), "name": "Фильм (2019)"})
	if code != http.StatusOK || out["name"] != "Фильм (2019).mp4" {
		t.Fatalf("переименование фильма: %d %v", code, out)
	}
	if e := list(t, s); len(e) != 1 || e[0].Title != "Фильм (2019)" || e[0].Position != 900 {
		t.Fatalf("запись фильма после переименования: %+v", e)
	}
	// занятое имя и корень библиотеки — отказ
	if code, _ := adminCall(t, s, "POST", "/api/admin/rename", testPin,
		map[string]string{"id": s.remember(filepath.Join(root, "Mini")), "name": "FilmDir"}); code != http.StatusBadRequest {
		t.Fatalf("занятое имя: %d", code)
	}
	if code, _ := adminCall(t, s, "POST", "/api/admin/rename", testPin,
		map[string]string{"id": s.remember(root), "name": "Other"}); code != http.StatusBadRequest {
		t.Fatalf("корень библиотеки: %d", code)
	}
}

// Удаление: в корзину с постером, из каталога пропадает, возвращается на место;
// второе удаление стирается по сроку.
func TestAdminTrash(t *testing.T) {
	s, root := adminServer(t)
	show := filepath.Join(root, "Show")
	code, out := adminCall(t, s, "POST", "/api/admin/delete", testPin, map[string]string{"id": s.remember(show)})
	if code != http.StatusOK {
		t.Fatalf("удаление: %d %v", code, out)
	}
	if exists(show) || exists(filepath.Join(root, "Show.jpg")) {
		t.Fatal("сериал или постер остались на месте")
	}
	for _, sr := range s.buildCatalog().Series {
		if sr.Name == "Show" {
			t.Fatal("удалённый сериал остался в каталоге")
		}
	}
	items := s.trash.list()
	if len(items) != 1 || items[0].Title != "Видео: Show" || len(items[0].Moves) != 2 {
		t.Fatalf("корзина: %+v", items)
	}
	if d := time.Unix(items[0].PurgeAt, 0).Sub(time.Unix(items[0].DeletedAt, 0)); d != 72*time.Hour {
		t.Fatalf("срок в корзине %s, ожидалось 3 дня", d)
	}
	if code, _ := adminCall(t, s, "POST", "/api/admin/trash/"+items[0].ID+"/restore", testPin, nil); code != http.StatusOK {
		t.Fatalf("возврат: %d", code)
	}
	if !exists(filepath.Join(show, "S01", "E01.mp4")) || !exists(filepath.Join(root, "Show.jpg")) {
		t.Fatal("после возврата сериал или постер не на месте")
	}
	if len(s.trash.list()) != 0 {
		t.Fatal("после возврата запись осталась в корзине")
	}
	// опустевшая корзина — [], а не null: страница перебирает список без проверок
	if _, out := adminCall(t, s, "GET", "/api/admin/trash", testPin, nil); out["items"] == nil {
		t.Fatal("пустая корзина отдаётся как null")
	}

	// отдельная серия; срок выходит — стирается с диска
	ep := filepath.Join(show, "S02", "E01.mp4")
	e, err := s.trashPath(ep)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.purgeTrash(e.ID); err != nil {
		t.Fatal(err)
	}
	if exists(e.Dir) || exists(ep) || len(s.trash.list()) != 0 {
		t.Fatal("стирание оставило следы")
	}
	// корень библиотеки удалить нельзя
	if code, _ := adminCall(t, s, "POST", "/api/admin/delete", testPin, map[string]string{"id": s.remember(root)}); code != http.StatusBadRequest {
		t.Fatalf("удаление корня: %d", code)
	}
	// из корзины через админку ничего не открыть
	if code, _ := adminCall(t, s, "GET", "/api/admin/children?id="+s.remember(filepath.Join(root, trashDirName)), testPin, nil); code != http.StatusNotFound {
		t.Fatalf("корзина видна в админке: %d", code)
	}
}

// Возврат не затирает то, что появилось на старом месте.
func TestAdminRestoreConflict(t *testing.T) {
	s, root := adminServer(t)
	film := filepath.Join(root, "Film.mp4")
	e, err := s.trashPath(film)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(film, []byte("new"), 0o644)
	if _, err := s.restoreTrash(e.ID); err == nil {
		t.Fatal("возврат поверх нового файла должен быть отвергнут")
	}
	if data, _ := os.ReadFile(film); string(data) != "new" {
		t.Fatal("новый файл затёрт")
	}
}

func TestNotifyURL(t *testing.T) {
	s := &server{cfg: config{hub: "wss://telegram.resager.ru/app/habits/api/v1/tv/socket"}}
	if got := s.notifyURL(); got != "https://telegram.resager.ru/app/habits/api/v1/tv/notify" {
		t.Fatalf("notifyURL = %s", got)
	}
	s.cfg.hub = "ws://127.0.0.1:8078/api/v1/tv/socket"
	if got := s.notifyURL(); got != "http://127.0.0.1:8078/api/v1/tv/notify" {
		t.Fatalf("notifyURL = %s", got)
	}
}
