package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// photoLibrary — библиотека фото с альбомом и подпапкой внутри него: пин
// ставится именно на подпапку, ради этого всё и затевалось.
func photoLibrary(t *testing.T) (*server, string) {
	t.Helper()
	root := t.TempDir()
	for _, f := range []string{
		"Отпуск/IMG_1.jpg", "Отпуск/IMG_2.jpg",
		"Отпуск/Личное/IMG_3.jpg", "Отпуск/Личное/IMG_4.jpg",
		"Разное/IMG_5.jpg",
	} {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		writeTestJPEG(t, p, 600, 400)
	}
	// Кэш не через t.TempDir: прогрев превью идёт в фоне и пишет в него уже
	// после теста — встроенная уборка спорила бы с ним и валила тест.
	cache, err := os.MkdirTemp("", "media-locks-cache")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for i := 0; i < 100; i++ {
			if os.RemoveAll(cache) == nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	s := newServer(config{base: "/tv", cache: cache,
		roots: []library{{ID: shortID(root), Path: root, Title: "Фото", Kind: "photo"}}})
	s.cfg.adminPin = testPin
	adminGuard.mu.Lock()
	adminGuard.fails, adminGuard.until = 0, time.Time{}
	adminGuard.mu.Unlock()
	unlockGuard.mu.Lock()
	unlockGuard.fails, unlockGuard.until = 0, time.Time{}
	unlockGuard.mu.Unlock()
	return s, root
}

func viewCall(t *testing.T, s *server, method, url, ticket string, body any) (int, map[string]any) {
	t.Helper()
	var rd *strings.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		rd = strings.NewReader(string(data))
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, "/tv"+url, rd)
	if ticket != "" {
		req.Header.Set("X-Unlock", ticket)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func dirNames(out map[string]any) []string {
	var names []string
	for _, v := range out["dirs"].([]any) {
		names = append(names, v.(map[string]any)["name"].(string))
	}
	return names
}

func TestLockHidesFolderFromListing(t *testing.T) {
	s, root := photoLibrary(t)
	album := filepath.Join(root, "Отпуск")
	secret := filepath.Join(album, "Личное")
	albumID, secretID := s.remember(album), s.remember(secret)

	// до замка подпапка видна
	code, out := viewCall(t, s, "GET", "/api/photos?id="+albumID, "", nil)
	if code != 200 || len(dirNames(out)) != 1 {
		t.Fatalf("до замка: %d %v", code, out["dirs"])
	}

	if code, out := adminCall(t, s, "POST", "/api/admin/locks", testPin,
		map[string]any{"id": secretID, "pin": "2468"}); code != 200 {
		t.Fatalf("поставить код: %d %v", code, out)
	}

	// в списке альбома подпапки больше нет
	code, out = viewCall(t, s, "GET", "/api/photos?id="+albumID, "", nil)
	if code != 200 {
		t.Fatalf("список альбома: %d", code)
	}
	if n := len(out["dirs"].([]any)); n != 0 {
		t.Errorf("закрытая подпапка осталась в списке: %v", out["dirs"])
	}
	// и внутрь по прямой ссылке не пускает
	if code, _ := viewCall(t, s, "GET", "/api/photos?id="+secretID, "", nil); code != 404 {
		t.Errorf("прямая ссылка в закрытую папку: %d, ждали 404", code)
	}
}

func TestUnlockOpensOnlyItsFolder(t *testing.T) {
	s, root := photoLibrary(t)
	secret := filepath.Join(root, "Отпуск", "Личное")
	other := filepath.Join(root, "Разное")
	secretID, otherID := s.remember(secret), s.remember(other)

	adminCall(t, s, "POST", "/api/admin/locks", testPin, map[string]any{"id": secretID, "pin": "2468"})
	adminCall(t, s, "POST", "/api/admin/locks", testPin, map[string]any{"id": otherID, "pin": "1357"})

	// неверный код не открывает ничего
	if code, _ := viewCall(t, s, "POST", "/api/unlock", "", map[string]any{"pin": "0000"}); code != 401 {
		t.Fatalf("неверный код: %d, ждали 401", code)
	}
	code, out := viewCall(t, s, "POST", "/api/unlock", "", map[string]any{"pin": "2468"})
	if code != 200 {
		t.Fatalf("верный код: %d %v", code, out)
	}
	ticket, _ := out["ticket"].(string)
	if ticket == "" || out["name"] != "Личное" {
		t.Fatalf("билет и имя: %v", out)
	}

	// по билету своя папка открыта
	if code, _ := viewCall(t, s, "GET", "/api/photos?id="+secretID, ticket, nil); code != 200 {
		t.Errorf("своя папка по билету: %d", code)
	}
	// а чужая — нет: код у каждой свой
	if code, _ := viewCall(t, s, "GET", "/api/photos?id="+otherID, ticket, nil); code != 404 {
		t.Errorf("чужая закрытая папка открылась по чужому билету")
	}
	// без билета — снова закрыта
	if code, _ := viewCall(t, s, "GET", "/api/photos?id="+secretID, "", nil); code != 404 {
		t.Errorf("папка открыта без билета")
	}
}

// Прямая ссылка на файл внутри закрытой папки не должна работать: id это хеш
// пути, и подпись `k` считается от него же — без этой проверки замок был бы
// нарисованным.
func TestLockBlocksDirectFile(t *testing.T) {
	s, root := photoLibrary(t)
	secret := filepath.Join(root, "Отпуск", "Личное")
	shot := filepath.Join(secret, "IMG_3.jpg")
	shotID := s.remember(shot)
	adminCall(t, s, "POST", "/api/admin/locks", testPin,
		map[string]any{"id": s.remember(secret), "pin": "2468"})

	url := "/tv/photo/" + shotID + "?k=" + s.sign(shotID) + "&s=thumb"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("снимок из закрытой папки отдан: %d", rec.Code)
	}

	_, out := viewCall(t, s, "POST", "/api/unlock", "", map[string]any{"pin": "2468"})
	ticket, _ := out["ticket"].(string)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url+"&u="+ticket, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("снимок не отдан по билету: %d", rec.Code)
	}
}

func TestLockHidesTileFromCatalog(t *testing.T) {
	s, root := photoLibrary(t)
	album := filepath.Join(root, "Отпуск")
	adminCall(t, s, "POST", "/api/admin/locks", testPin,
		map[string]any{"id": s.remember(album), "pin": "4321"})

	code, out := viewCall(t, s, "GET", "/api/catalog", "", nil)
	if code != 200 {
		t.Fatalf("каталог: %d", code)
	}
	for _, v := range out["photos"].([]any) {
		if v.(map[string]any)["name"] == "Отпуск" {
			t.Fatal("закрытый альбом остался в каталоге")
		}
	}
	// админке по-прежнему видно всё: у неё свой пин
	_, adm := adminCall(t, s, "GET", "/api/admin/overview", testPin, nil)
	found := false
	for _, v := range adm["sections"].(map[string]any)["photos"].([]any) {
		if v.(map[string]any)["name"] == "Отпуск" {
			found = true
		}
	}
	if !found {
		t.Error("закрытый альбом пропал из админки — его там прятать незачем")
	}
}

// Замок висит на пути, поэтому переименование папки не должно его сбрасывать.
func TestLockFollowsRename(t *testing.T) {
	s, root := photoLibrary(t)
	secret := filepath.Join(root, "Отпуск", "Личное")
	adminCall(t, s, "POST", "/api/admin/locks", testPin,
		map[string]any{"id": s.remember(secret), "pin": "2468"})

	neu, err := s.renamePath(secret, "Своё")
	if err != nil {
		t.Fatal(err)
	}
	if !s.locks.has(neu) {
		t.Fatal("после переименования замок потерялся — папка открылась сама")
	}
	if s.locks.has(secret) {
		t.Error("замок остался и на старом пути")
	}
	if code, _ := viewCall(t, s, "GET", "/api/photos?id="+s.remember(neu), "", nil); code != 404 {
		t.Error("переименованная папка видна без кода")
	}
}

func TestLockDropAndRules(t *testing.T) {
	s, root := photoLibrary(t)
	secret := filepath.Join(root, "Отпуск", "Личное")
	secretID := s.remember(secret)

	// слишком короткий и нецифровой код
	for _, bad := range []string{"12", "abcd", ""} {
		if code, _ := adminCall(t, s, "POST", "/api/admin/locks", testPin,
			map[string]any{"id": secretID, "pin": bad}); code != 400 {
			t.Errorf("код %q принят", bad)
		}
	}
	// целый раздел закрывать нельзя
	if code, _ := adminCall(t, s, "POST", "/api/admin/locks", testPin,
		map[string]any{"id": s.remember(root), "pin": "1111"}); code != 400 {
		t.Error("корень библиотеки закрылся кодом")
	}
	// два одинаковых кода на разные папки — отказ: иначе непонятно, какую открывать
	adminCall(t, s, "POST", "/api/admin/locks", testPin, map[string]any{"id": secretID, "pin": "2468"})
	if code, out := adminCall(t, s, "POST", "/api/admin/locks", testPin,
		map[string]any{"id": s.remember(filepath.Join(root, "Разное")), "pin": "2468"}); code != 400 {
		t.Errorf("тот же код на второй папке принят: %d %v", code, out)
	}
	// свой же код сменить на себя же можно
	if code, _ := adminCall(t, s, "POST", "/api/admin/locks", testPin,
		map[string]any{"id": secretID, "pin": "2468"}); code != 200 {
		t.Error("повторная установка того же кода на ту же папку отклонена")
	}

	// список для админки есть, самих кодов в нём нет
	code, out := adminCall(t, s, "GET", "/api/admin/locks", testPin, nil)
	items, _ := out["items"].([]any)
	if code != 200 || len(items) != 1 {
		t.Fatalf("список замков: %d %v", code, out)
	}
	// время установки — чтобы в админке не писалось «закрыта 01.01 1970»
	if when, _ := items[0].(map[string]any)["set"].(float64); when < 1e9 {
		t.Errorf("время установки кода не доехало: %v", items[0])
	}
	body, _ := json.Marshal(out)
	for _, leak := range []string{"2468", "hash", "salt", "pin"} {
		if strings.Contains(strings.ToLower(string(body)), leak) {
			t.Errorf("в списке замков утекает %q: %s", leak, body)
		}
	}

	if code, _ := adminCall(t, s, "POST", "/api/admin/locks/drop", testPin,
		map[string]any{"id": secretID}); code != 200 {
		t.Fatal("код не снялся")
	}
	if code, _ := viewCall(t, s, "GET", "/api/photos?id="+secretID, "", nil); code != 200 {
		t.Error("после снятия кода папка не открылась")
	}
}

// «Вышли из раздела» — закрывается сразу, не дожидаясь получаса простоя.
func TestLockCloseOnLeave(t *testing.T) {
	s, root := photoLibrary(t)
	secret := filepath.Join(root, "Отпуск", "Личное")
	secretID := s.remember(secret)
	adminCall(t, s, "POST", "/api/admin/locks", testPin, map[string]any{"id": secretID, "pin": "2468"})

	_, out := viewCall(t, s, "POST", "/api/unlock", "", map[string]any{"pin": "2468"})
	ticket, _ := out["ticket"].(string)
	if code, _ := viewCall(t, s, "GET", "/api/photos?id="+secretID, ticket, nil); code != 200 {
		t.Fatal("папка не открылась")
	}
	viewCall(t, s, "POST", "/api/lock", "", map[string]any{"ticket": ticket})
	if code, _ := viewCall(t, s, "GET", "/api/photos?id="+secretID, ticket, nil); code != 404 {
		t.Error("после выхода из раздела папка осталась открытой")
	}
}

// Простой дольше получаса закрывает папку сам.
func TestUnlockExpiresOnIdle(t *testing.T) {
	us := newUnlockStore()
	token := us.open("", "/любая/папка")
	if !us.allowed(token, "/любая/папка") {
		t.Fatal("только что открытая папка закрыта")
	}
	us.mu.Lock()
	us.items[token].seen = time.Now().Add(-unlockIdle - time.Minute)
	us.mu.Unlock()
	if us.allowed(token, "/любая/папка") {
		t.Error("папка осталась открытой после простоя")
	}
}

// Перебор кода запирается, как и у админского пина.
func TestUnlockBruteForceGuard(t *testing.T) {
	s, root := photoLibrary(t)
	adminCall(t, s, "POST", "/api/admin/locks", testPin,
		map[string]any{"id": s.remember(filepath.Join(root, "Разное")), "pin": "2468"})
	for i := 0; i < unlockMaxFails; i++ {
		if code, _ := viewCall(t, s, "POST", "/api/unlock", "", map[string]any{"pin": "0000"}); code != 401 {
			t.Fatalf("промах %d: %d", i+1, code)
		}
	}
	// даже верный код теперь ждёт
	if code, _ := viewCall(t, s, "POST", "/api/unlock", "", map[string]any{"pin": "2468"}); code != 429 {
		t.Fatalf("после %d промахов ввод не заперт: %d", unlockMaxFails, code)
	}
	unlockGuard.mu.Lock()
	unlockGuard.fails, unlockGuard.until = 0, time.Time{}
	unlockGuard.mu.Unlock()
}

// Папку удалили — замок уходит с ней: иначе новая папка с тем же именем
// открылась бы по старому коду.
func TestLockForgottenOnDelete(t *testing.T) {
	s, root := photoLibrary(t)
	secret := filepath.Join(root, "Отпуск", "Личное")
	secretID := s.remember(secret)
	adminCall(t, s, "POST", "/api/admin/locks", testPin, map[string]any{"id": secretID, "pin": "2468"})
	if code, out := adminCall(t, s, "POST", "/api/admin/delete", testPin,
		map[string]any{"id": secretID}); code != 200 {
		t.Fatalf("удаление: %d %v", code, out)
	}
	if s.locks.has(secret) {
		t.Error("замок пережил папку")
	}
}

// Главный путь — кука: её браузер носит сам, и поэтому не пришлось дописывать
// билет в каждый `<img src>` и `<video src>` на странице.
func TestUnlockByCookie(t *testing.T) {
	s, root := photoLibrary(t)
	secret := filepath.Join(root, "Отпуск", "Личное")
	secretID := s.remember(secret)
	adminCall(t, s, "POST", "/api/admin/locks", testPin, map[string]any{"id": secretID, "pin": "2468"})

	// открываем и берём куку из ответа, как это сделал бы браузер
	req := httptest.NewRequest(http.MethodPost, "/tv/api/unlock",
		strings.NewReader(`{"pin":"2468"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("открытие: %d", rec.Code)
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == unlockCookie {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value == "" {
		t.Fatal("кука с билетом не выдана")
	}
	if !cookie.HttpOnly {
		t.Error("кука доступна скриптам — ей там делать нечего")
	}

	// запрос без заголовка, только с кукой
	req = httptest.NewRequest(http.MethodGet, "/tv/api/photos?id="+secretID, nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("по куке папка не открылась: %d", rec.Code)
	}
	// и картинка внутри — тоже по куке, без «u=» в ссылке
	shotID := s.remember(filepath.Join(secret, "IMG_3.jpg"))
	req = httptest.NewRequest(http.MethodGet, "/tv/photo/"+shotID+"?k="+s.sign(shotID)+"&s=thumb", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("снимок по куке не отдан: %d", rec.Code)
	}
}
