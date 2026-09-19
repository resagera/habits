package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// maxImageBytes — потолок на одну картинку общей галереи.
const maxImageBytes = 10 << 20

// saveUploadedImage сохраняет картинку из multipart-поля в каталог и
// возвращает сгенерированное имя файла.
//
// Тип определяется по содержимому, а не по расширению из запроса; имя —
// случайное (как у фонов пользователей), поэтому загрузка не может ни
// перезаписать чужой файл, ни выбраться из каталога.
func saveUploadedImage(r *http.Request, field, dir, prefix string) (string, error) {
	file, _, err := r.FormFile(field)
	if err != nil {
		return "", fmt.Errorf("поле %q обязательно", field)
	}
	defer file.Close()

	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", err
	}
	var ext string
	switch http.DetectContentType(head[:n]) {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	default:
		return "", errors.New("файл должен быть изображением jpeg/png/webp/gif")
	}

	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	name := prefix + hex.EncodeToString(buf) + ext

	dst, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	defer dst.Close()
	if _, err := dst.Write(head[:n]); err != nil {
		return "", err
	}
	if _, err := io.Copy(dst, io.LimitReader(file, maxImageBytes)); err != nil {
		return "", err
	}
	return name, nil
}
