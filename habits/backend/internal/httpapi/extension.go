package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"streaks-backend/internal/extension"
)

// extensionHandler отдаёт файлы расширения браузера. Публично и без
// авторизации — внутри только клиент, доступ к данным всё равно по токену.
//
//	/ext/habits-chrome.zip   — распаковать и загрузить в режиме разработчика
//	/ext/habits-firefox.xpi  — подписанный Mozilla, ставится постоянно
//	/ext/updates.json        — манифест обновлений, на него ссылается сам xpi
//	/ext/habits-firefox.zip  — неподписанный, для разработки (временная установка)
func extensionHandler(exts *extension.Archives) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		head := strings.EqualFold(r.Method, http.MethodHead)

		switch name {
		case extension.XPIFileName:
			data, err := extension.XPI()
			if err != nil {
				http.NotFound(w, r)
				return
			}
			// тип из спецификации установки дополнений Firefox
			w.Header().Set("Content-Type", "application/x-xpinstall")
			w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
			w.Header().Set("Cache-Control", "public, max-age=3600")
			if !head {
				w.Write(data)
			}
			return
		case extension.UpdatesFileName:
			data, err := exts.Updates()
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			// обновления проверяются браузером сами — кэш короче
			w.Header().Set("Cache-Control", "public, max-age=600")
			if !head {
				w.Write(data)
			}
			return
		}

		var browser extension.Browser
		switch name {
		case extension.FileName(extension.Chrome):
			browser = extension.Chrome
		case extension.FileName(extension.Firefox):
			browser = extension.Firefox
		default:
			http.NotFound(w, r)
			return
		}
		data, err := exts.Zip(browser)
		if errors.Is(err, extension.ErrUnknownBrowser) {
			http.NotFound(w, r)
			return
		} else if err != nil {
			internalError(w)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		// архив меняется только вместе с версией бэкенда
		w.Header().Set("Cache-Control", "public, max-age=3600")
		if head {
			return
		}
		w.Write(data)
	}
}
