package extension

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Подписанный файл — не пересобираемый артефакт: подпись покрывает байты.
// Если он потеряется или окажется неподписанным, Firefox поставит расширение
// только временно, а мы этого не заметим — отсюда проверки.
func TestXPIIsSigned(t *testing.T) {
	data, err := XPI()
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var hasSig, hasManifest bool
	for _, f := range zr.File {
		switch f.Name {
		case "META-INF/mozilla.rsa":
			hasSig = true
		case "manifest.json":
			hasManifest = true
		}
	}
	if !hasSig {
		t.Error("в .xpi нет подписи Mozilla (META-INF/mozilla.rsa) — Firefox поставит только временно")
	}
	if !hasManifest {
		t.Error("в .xpi нет manifest.json")
	}
}

// Версия и id читаются из самого архива, а не дублируются в коде: разъедутся —
// Firefox перестанет видеть обновления.
func TestXPIInfoMatchesArchive(t *testing.T) {
	id, version, err := xpiInfo()
	if err != nil {
		t.Fatal(err)
	}
	if id != "habits@resager.ru" {
		t.Errorf("id = %q", id)
	}
	if version == "" || strings.Count(version, ".") != 2 {
		t.Errorf("версия выглядит странно: %q", version)
	}
}

func TestUpdatesManifest(t *testing.T) {
	a := New("https://example.test/app/habits")
	data, err := a.Updates()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Addons map[string]struct {
			Updates []struct {
				Version    string `json:"version"`
				UpdateLink string `json:"update_link"`
			} `json:"updates"`
		} `json:"addons"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	entry, ok := doc.Addons["habits@resager.ru"]
	if !ok {
		t.Fatalf("в манифесте обновлений нет нашего id: %s", data)
	}
	if len(entry.Updates) != 1 {
		t.Fatalf("ожидали одну версию, получили %d", len(entry.Updates))
	}
	_, version, _ := xpiInfo()
	if entry.Updates[0].Version != version {
		t.Errorf("версия в манифесте %q не совпадает с версией в .xpi %q",
			entry.Updates[0].Version, version)
	}
	want := "https://example.test/app/habits/ext/habits-firefox.xpi"
	if entry.Updates[0].UpdateLink != want {
		t.Errorf("ссылка %q, ожидали %q", entry.Updates[0].UpdateLink, want)
	}
}

// В подписанном файле адрес приложения и update_url уже подставлены —
// плейсхолдеров остаться не должно.
func TestXPIHasNoPlaceholders(t *testing.T) {
	data, _ := XPI()
	if bytes.Contains(data, []byte("{{APP_URL}}")) {
		t.Error("в подписанном .xpi остался плейсхолдер {{APP_URL}}")
	}
}

// А в zip плейсхолдер должен подставляться на лету — в т.ч. в манифесте,
// где теперь живёт update_url.
func TestZipManifestSubstituted(t *testing.T) {
	data, err := New("https://example.test/app/habits/").Zip(Firefox)
	if err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	f, err := zr.Open("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var buf bytes.Buffer
	buf.ReadFrom(f)
	if bytes.Contains(buf.Bytes(), []byte("{{APP_URL}}")) {
		t.Error("в манифесте zip остался плейсхолдер")
	}
	if !bytes.Contains(buf.Bytes(), []byte("https://example.test/app/habits/ext/updates.json")) {
		t.Errorf("update_url не подставлен: %s", buf.String())
	}
}
