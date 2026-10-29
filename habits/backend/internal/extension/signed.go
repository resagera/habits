package extension

// Подписанное расширение для Firefox.
//
// Firefox ставит неподписанное дополнение только временно — до перезапуска
// браузера. Поэтому для него мы раздаём не zip, а .xpi, подписанный Mozilla
// (канал unlisted: подпись есть, в каталоге AMO не публикуется).
//
// Подпись покрывает содержимое архива, поэтому собрать его на лету, как zip,
// нельзя: адрес приложения зашит внутрь в момент подписи. Файл лежит рядом с
// исходниками и вшивается в бинарник — обновляется скриптом sign-firefox.sh.

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"sync"
)

//go:embed files/signed/habits-firefox.xpi
var firefoxXPI []byte

// XPIFileName — имя подписанного файла для скачивания.
const XPIFileName = "habits-firefox.xpi"

// UpdatesFileName — манифест обновлений, на который ссылается update_url
// внутри подписанного расширения.
const UpdatesFileName = "updates.json"

// ErrNoXPI — подписанного файла нет (собран без него).
var ErrNoXPI = errors.New("signed xpi is not embedded")

var (
	xpiOnce sync.Once
	xpiMeta struct {
		version string
		id      string
		err     error
	}
)

// XPI возвращает подписанное расширение.
func XPI() ([]byte, error) {
	if len(firefoxXPI) == 0 {
		return nil, ErrNoXPI
	}
	return firefoxXPI, nil
}

// xpiInfo читает id и версию из манифеста внутри подписанного архива.
// Дублировать их в коде нельзя: они должны совпадать с подписанным файлом,
// иначе Firefox не увидит обновление.
func xpiInfo() (id, version string, err error) {
	xpiOnce.Do(func() {
		if len(firefoxXPI) == 0 {
			xpiMeta.err = ErrNoXPI
			return
		}
		zr, err := zip.NewReader(bytes.NewReader(firefoxXPI), int64(len(firefoxXPI)))
		if err != nil {
			xpiMeta.err = err
			return
		}
		f, err := zr.Open("manifest.json")
		if err != nil {
			xpiMeta.err = err
			return
		}
		defer f.Close()
		var m struct {
			Version  string `json:"version"`
			Settings struct {
				Gecko struct {
					ID string `json:"id"`
				} `json:"gecko"`
			} `json:"browser_specific_settings"`
		}
		if err := json.NewDecoder(f).Decode(&m); err != nil {
			xpiMeta.err = err
			return
		}
		xpiMeta.version, xpiMeta.id = m.Version, m.Settings.Gecko.ID
	})
	return xpiMeta.id, xpiMeta.version, xpiMeta.err
}

// XPIVersion — версия подписанного расширения.
func XPIVersion() (string, error) {
	_, v, err := xpiInfo()
	return v, err
}

// Updates собирает манифест обновлений Firefox: браузер периодически
// опрашивает update_url и ставит версию новее установленной сам.
func (a *Archives) Updates() ([]byte, error) {
	id, version, err := xpiInfo()
	if err != nil {
		return nil, err
	}
	type update struct {
		Version    string `json:"version"`
		UpdateLink string `json:"update_link"`
	}
	doc := map[string]any{
		"addons": map[string]any{
			id: map[string]any{
				"updates": []update{{
					Version:    version,
					UpdateLink: a.appURL + "ext/" + XPIFileName,
				}},
			},
		},
	}
	return json.MarshalIndent(doc, "", "  ")
}

// XPIURL — публичная ссылка на подписанный файл (для инструкций и логов).
func (a *Archives) XPIURL() string {
	return strings.TrimSuffix(a.appURL, "/") + "/ext/" + XPIFileName
}
