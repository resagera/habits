package main

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Файл настроек агента.
//
// Переменные окружения в systemd-юните быстро становятся нечитаемыми: список
// папок — это длинная строка с разделителями, и править её в одну строку
// неудобно. Файл позволяет писать по папке на строку и оставлять комментарии.
//
// Порядок: значения по умолчанию → файл → переменные окружения. Окружение
// главнее файла, чтобы одноразовый запуск с другой папкой не требовал правки
// настроек.

// configPath — где искать файл. MEDIA_CONFIG задаёт его явно.
func configPath() string {
	if p := strings.TrimSpace(os.Getenv("MEDIA_CONFIG")); p != "" {
		return expandHome(p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "habits-media-agent.conf")
	}
	return ""
}

// loadFile читает настройки. Отсутствие файла — не ошибка: агент прекрасно
// работает на одних переменных окружения.
func loadFile(path string) map[string]string {
	out := map[string]string{}
	if path == "" {
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	// строка с папками бывает длинной, стандартного буфера ей мало
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	roots := []string{}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		// «roots» можно повторять — по папке на строку, так читаемее
		if key == "roots" || key == "root" {
			if value != "" {
				roots = append(roots, value)
			}
			continue
		}
		out[key] = value
	}
	if err := sc.Err(); err != nil {
		log.Printf("настройки %s читаются с ошибкой: %v", path, err)
	}
	if len(roots) > 0 {
		out["roots"] = strings.Join(roots, ";")
	}
	log.Printf("настройки: %s", path)
	return out
}

// pick — значение по ключу с учётом порядка: окружение → файл → умолчание.
func pick(file map[string]string, envKey, fileKey, def string) string {
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		return v
	}
	if v := strings.TrimSpace(file[fileKey]); v != "" {
		return v
	}
	return def
}
