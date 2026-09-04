// habits-media-agent — домашний медиасервер для ТВ-плеера.
//
// Отдаёт файлы из явно разрешённых папок по локальной сети и страницу плеера,
// которую открывают на ТВ-приставке. Управление придёт с пульта в Telegram
// мини-аппе через прод (следующий этап) — сейчас агент самодостаточен, чтобы
// его можно было проверить на приставке до всей серверной обвязки.
//
// Слушает 127.0.0.1: наружу его выставляет nginx на 80-м порту (на роутере
// нестандартные порты закрыты). Отсюда MEDIA_BASE — префикс, под которым
// агент виден снаружи, иначе ссылки внутри страницы уедут в корень.
//
// Переменные окружения:
//
//	MEDIA_ROOTS   папки через «;», каждая «путь|Название|вид»,
//	              вид: video | music | photo (по умолчанию video)
//	              MEDIA_ROOTS='/mnt/serials|Сериалы|video;/mnt/music|Музыка|music'
//	MEDIA_ADDR    что слушать, по умолчанию 127.0.0.1:8410
//	MEDIA_BASE    внешний префикс, по умолчанию /tv
//	MEDIA_CACHE   кэш разбора и перекодированных файлов,
//	              по умолчанию ~/.cache/habits-media
package main

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type config struct {
	addr  string
	base  string // внешний префикс, всегда без хвостового слэша
	cache string
	roots []library
	// room — как приставка и пульт находят друг друга: строка, по умолчанию
	// адрес этой машины в локальной сети. Пульту достаточно назвать её.
	room string
	// hub — шина на проде. Страница берёт его отсюда, а не зашивает в себя:
	// в отладке удобно указать локальный бэкенд.
	hub string
	// roomOverride — имя комнаты, заданное руками. Пусто — берём постоянное
	// имя из кэша.
	roomOverride string
}

func loadConfig() config {
	file := loadFile(configPath())
	c := config{
		addr:  pick(file, "MEDIA_ADDR", "addr", "127.0.0.1:8410"),
		base:  strings.TrimSuffix(pick(file, "MEDIA_BASE", "base", "/tv"), "/"),
		cache: pick(file, "MEDIA_CACHE", "cache", defaultCache()),
		roots: parseRoots(pick(file, "MEDIA_ROOTS", "roots", "")),
		hub: pick(file, "MEDIA_HUB", "hub",
			"wss://telegram.resager.ru/app/habits/api/v1/tv/socket"),
		roomOverride: pick(file, "MEDIA_ROOM", "room", ""),
	}
	if c.base != "" && !strings.HasPrefix(c.base, "/") {
		c.base = "/" + c.base
	}
	return c
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func defaultCache() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".cache", "habits-media")
	}
	return filepath.Join(os.TempDir(), "habits-media")
}

// parseRoots разбирает MEDIA_ROOTS. Разделитель полей — «|»: в путях он не
// встречается, в отличие от двоеточия (диски, тома) и запятой (названия).
func parseRoots(spec string) []library {
	var out []library
	for _, part := range strings.Split(spec, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Split(part, "|")
		path := strings.TrimSpace(fields[0])
		title, kind := "", "video"
		if len(fields) > 1 {
			title = strings.TrimSpace(fields[1])
		}
		if len(fields) > 2 {
			if k := strings.TrimSpace(fields[2]); k == "music" || k == "photo" || k == "video" {
				kind = k
			}
		}
		abs, err := filepath.Abs(filepath.Clean(expandHome(path)))
		if err != nil {
			log.Printf("папка %q пропущена: %v", path, err)
			continue
		}
		if st, err := os.Stat(abs); err != nil || !st.IsDir() {
			log.Printf("папка %q пропущена: не каталог", abs)
			continue
		}
		if title == "" {
			title = filepath.Base(abs)
		}
		out = append(out, library{ID: shortID(abs), Path: abs, Title: title, Kind: kind})
	}
	return out
}

// roomKey — постоянное имя комнаты, по которому приставка и пульт встречаются.
//
// Нарочно НЕ адрес в локальной сети: его раздаёт DHCP, и после перезагрузки
// роутера комната стала бы другой, а сохранённая на телефоне приставка —
// мёртвой. Имя машины плюс случайный хвост: постоянно, ни с кем не совпадёт.
// Лежит в кэше, поэтому переживает перезапуск и обновление агента.
func roomKey(cacheDir string) string {
	path := filepath.Join(cacheDir, "room")
	if data, err := os.ReadFile(path); err == nil {
		if key := strings.TrimSpace(string(data)); key != "" {
			return key
		}
	}
	host, _ := os.Hostname()
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		host = "media"
	}
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return host
	}
	key := host + "-" + hex.EncodeToString(b)
	if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
		log.Printf("имя комнаты не сохранилось (%v) — после перезапуска придётся подключиться заново", err)
	}
	return key
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func main() {
	log.SetFlags(log.Ltime)
	cfg := loadConfig()
	if len(cfg.roots) == 0 {
		log.Fatal("не задано ни одной папки: MEDIA_ROOTS='/путь|Название|video;…'")
	}
	if err := os.MkdirAll(cfg.cache, 0o700); err != nil {
		log.Fatalf("кэш %s: %v", cfg.cache, err)
	}
	// имя комнаты лежит в кэше, поэтому считается после его создания
	cfg.room = strings.ToLower(cfg.roomOverride)
	if cfg.room == "" {
		cfg.room = roomKey(cfg.cache)
	}

	srv := newServer(cfg)
	log.Printf("папки:")
	for _, r := range cfg.roots {
		log.Printf("  %s [%s] %s", r.Title, r.Kind, r.Path)
	}
	log.Printf("слушаю %s, внешний префикс %s", cfg.addr, cfg.base+"/")
	log.Printf("комната для пульта: %s (шина %s)", cfg.room, cfg.hub)
	log.Printf("на приставке открыть: http://<адрес-компьютера>%s/", cfg.base)
	if err := http.ListenAndServe(cfg.addr, srv); err != nil {
		log.Fatal(err)
	}
}
