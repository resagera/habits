package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Разбор идёт по плиткам: у сериала — первая серия, у фильма — он сам.
func TestQualityScan(t *testing.T) {
	s, _ := adminServer(t)
	catalogOf(t, s) // пути плиток запомнены
	code, out := adminCall(t, s, "GET", "/api/admin/quality", testPin, nil)
	if code != http.StatusOK {
		t.Fatalf("разбор: %d %v", code, out)
	}
	var got struct {
		Items []qualityItem `json:"items"`
	}
	raw, _ := json.Marshal(out)
	_ = json.Unmarshal(raw, &got)

	// в тестовой библиотеке файлы пустые — ffprobe их не разберёт, и в список
	// они не попадут: это и проверяем, молчаливых паник быть не должно
	for _, it := range got.Items {
		if it.Info == nil {
			t.Fatalf("элемент без разбора: %+v", it)
		}
		if it.Kind == "series" && it.Episodes == 0 {
			t.Fatalf("у сериала не посчитаны серии: %+v", it)
		}
		if it.File == "" {
			t.Fatalf("не сказано, по какому файлу судим: %+v", it)
		}
	}
	// PIN обязателен: список показывает пути на диске
	if code, _ := adminCall(t, s, "GET", "/api/admin/quality", "0000", nil); code != http.StatusUnauthorized {
		t.Fatalf("разбор отдался без PIN: %d", code)
	}
}

// «720p» у широкоэкранного кино: кадр 2,35:1 не должен считаться низким.
func TestQuality16x9(t *testing.T) {
	cases := []struct {
		w, h, want int
	}{
		{1280, 720, 720},  // обычные 720p
		{1280, 546, 720},  // то же кино, но 2,35:1 — по высоте было бы «546p»
		{1920, 800, 1080}, // широкий Full HD
		{1920, 1080, 1080},
		{704, 384, 396}, // DVDRip — низкое и по длинной стороне
		{720, 304, 405},
		{624, 256, 351},
		{1080, 1920, 1920}, // вертикальное видео — по высоте
		{0, 0, 0},
	}
	for _, c := range cases {
		if got := quality16x9(c.w, c.h); got != c.want {
			t.Fatalf("quality16x9(%d, %d) = %d, хотели %d", c.w, c.h, got, c.want)
		}
	}
}
