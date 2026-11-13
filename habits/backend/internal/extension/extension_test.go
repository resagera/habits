package extension

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func names(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("не читается zip: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		var buf bytes.Buffer
		buf.ReadFrom(rc)
		rc.Close()
		out[f.Name] = buf.String()
	}
	return out
}

func TestZipContents(t *testing.T) {
	a := New("https://example.test/app/habits")

	for _, b := range []Browser{Chrome, Firefox} {
		data, err := a.Zip(b)
		if err != nil {
			t.Fatalf("%s: %v", b, err)
		}
		files := names(t, data)

		for _, want := range []string{"manifest.json", "popup.html", "icons/icon16.png", "icons/icon48.png", "icons/icon128.png"} {
			if _, ok := files[want]; !ok {
				t.Errorf("%s: в архиве нет %s", b, want)
			}
		}
		// адрес приложения подставлен, плейсхолдер не остался
		if strings.Contains(files["popup.html"], "{{APP_URL}}") {
			t.Errorf("%s: плейсхолдер не заменён", b)
		}
		if !strings.Contains(files["popup.html"], "https://example.test/app/habits/") {
			t.Errorf("%s: адрес приложения не подставлен", b)
		}
		// манифест — правильного варианта
		isFirefoxManifest := strings.Contains(files["manifest.json"], "browser_specific_settings")
		if b == Firefox && !isFirefoxManifest {
			t.Error("firefox: манифест без browser_specific_settings")
		}
		if b == Chrome && isFirefoxManifest {
			t.Error("chrome: в манифест попали firefox-настройки")
		}
	}
}

func TestZipUnknownBrowser(t *testing.T) {
	if _, err := New("https://x/").Zip("opera"); err == nil {
		t.Fatal("неизвестный браузер должен давать ошибку")
	}
}

func TestFileName(t *testing.T) {
	if got := FileName(Firefox); got != "habits-firefox.zip" {
		t.Fatalf("got %q", got)
	}
}
