package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

// Config настраивает поведение логгера
type Config struct {
	Level   slog.Level // уровень логирования (например, slog.LevelInfo)
	File    string     // путь к файлу лога (если пустой — только консоль)
	Console bool       // выводить ли в консоль
	JSON    bool       // использовать JSON-формат (иначе текстовый)
}

// New создаёт новый *slog.Logger по заданной конфигурации
func New(cfg Config) (*slog.Logger, error) {
	var handlers []slog.Handler

	var opts = &slog.HandlerOptions{
		Level: cfg.Level,
	}

	// Создаём обработчик для файла, если указан путь
	if cfg.File != "" {
		file, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, err
		}

		var fileHandler slog.Handler
		if cfg.JSON {
			fileHandler = slog.NewJSONHandler(file, opts)
		} else {
			fileHandler = slog.NewTextHandler(file, opts)
		}
		handlers = append(handlers, fileHandler)
	}

	// Добавляем консольный вывод, если нужно
	if cfg.Console {
		var consoleHandler slog.Handler
		if cfg.JSON {
			consoleHandler = slog.NewJSONHandler(os.Stdout, opts)
		} else {
			consoleHandler = slog.NewTextHandler(os.Stdout, opts)
		}
		handlers = append(handlers, consoleHandler)
	}

	if len(handlers) == 0 {
		// Если ничего не указано — хотя бы консоль включим по умолчанию
		handlers = append(handlers, slog.NewTextHandler(os.Stdout, opts))
	}

	// Объединяем все обработчики в один
	multiHandler := &multiHandler{handlers: handlers}
	return slog.New(multiHandler), nil
}

// multiHandler — простой объединитель нескольких slog.Handler'ов
type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, record slog.Record) error {
	for _, h := range m.handlers {
		if h.Enabled(ctx, record.Level) {
			if err := h.Handle(ctx, record); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		newHandlers[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: newHandlers}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		newHandlers[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: newHandlers}
}

// Fatal логирует сообщение с уровнем Error и завершает программу с кодом 1.
func Fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

// Fatalf — форматированная версия Fatal
func Fatalf(msg string, args ...any) {
	slog.Error(fmt.Sprintf(msg, args...))
	os.Exit(1)
}
