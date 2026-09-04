// Package extension отдаёт готовые архивы расширения браузера: свой для
// Chrome и свой для Firefox (различаются только манифестом). Файлы вшиты в
// бинарник, zip собирается в памяти при первом запросе и кэшируется —
// архив всегда соответствует задеплоенной версии, ничего собирать руками
// и класть в репозиторий не нужно.
package extension

import (
	"archive/zip"
	"bytes"
	"embed"
	"errors"
	"io/fs"
	"strings"
	"sync"
)

//go:embed files
var src embed.FS

// Browser — вариант сборки; отличается только тем, какой манифест кладётся
// в архив под именем manifest.json.
type Browser string

const (
	Chrome  Browser = "chrome"
	Firefox Browser = "firefox"
)

var ErrUnknownBrowser = errors.New("unknown browser")

// Archives собирает и кэширует zip-архивы. appURL — адрес приложения,
// который подставляется в popup.html (плейсхолдер {{APP_URL}}), чтобы
// скачанное расширение указывало на тот сервер, откуда его взяли.
type Archives struct {
	appURL string

	mu    sync.Mutex
	cache map[Browser][]byte
}

func New(appURL string) *Archives {
	if !strings.HasSuffix(appURL, "/") {
		appURL += "/"
	}
	return &Archives{appURL: appURL, cache: map[Browser][]byte{}}
}

// FileName — имя архива для скачивания.
func FileName(b Browser) string {
	return "habits-" + string(b) + ".zip"
}

// Zip возвращает архив расширения (собирается один раз на процесс).
func (a *Archives) Zip(b Browser) ([]byte, error) {
	if b != Chrome && b != Firefox {
		return nil, ErrUnknownBrowser
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if data, ok := a.cache[b]; ok {
		return data, nil
	}
	data, err := a.build(b)
	if err != nil {
		return nil, err
	}
	a.cache[b] = data
	return data, nil
}

func (a *Archives) build(b Browser) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	manifest, err := src.ReadFile("files/manifest." + string(b) + ".json")
	if err != nil {
		return nil, err
	}
	manifest = bytes.ReplaceAll(manifest, []byte("{{APP_URL}}"), []byte(a.appURL))
	if err := writeFile(zw, "manifest.json", manifest); err != nil {
		return nil, err
	}

	popup, err := src.ReadFile("files/popup.html")
	if err != nil {
		return nil, err
	}
	popup = bytes.ReplaceAll(popup, []byte("{{APP_URL}}"), []byte(a.appURL))
	if err := writeFile(zw, "popup.html", popup); err != nil {
		return nil, err
	}

	icons, err := fs.ReadDir(src, "files/icons")
	if err != nil {
		return nil, err
	}
	for _, ic := range icons {
		data, err := src.ReadFile("files/icons/" + ic.Name())
		if err != nil {
			return nil, err
		}
		if err := writeFile(zw, "icons/"+ic.Name(), data); err != nil {
			return nil, err
		}
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeFile(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
