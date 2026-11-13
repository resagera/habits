package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// jpegWithOrientation — маленький JPEG с EXIF-меткой поворота, как пишет камера.
func jpegWithOrientation(t *testing.T, w, h, o int, bo binary.ByteOrder) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	tiff := make([]byte, 8+2+12+4)
	if bo == binary.LittleEndian {
		copy(tiff, "II")
	} else {
		copy(tiff, "MM")
	}
	bo.PutUint16(tiff[2:], 42)
	bo.PutUint32(tiff[4:], 8)
	bo.PutUint16(tiff[8:], 1)
	bo.PutUint16(tiff[10:], 0x0112)
	bo.PutUint16(tiff[12:], 3)
	bo.PutUint32(tiff[14:], 1)
	bo.PutUint16(tiff[18:], uint16(o))
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(app1[2:], uint16(len(seg)+2))
	src := buf.Bytes()
	out := append([]byte{}, src[:2]...)
	out = append(out, app1...)
	out = append(out, seg...)
	return append(out, src[2:]...)
}

func TestExifOrientation(t *testing.T) {
	for _, bo := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for o := 1; o <= 8; o++ {
			data := jpegWithOrientation(t, 8, 4, o, bo)
			if got := exifOrientation(bytes.NewReader(data)); got != o {
				t.Errorf("%v: метка %d прочиталась как %d", bo, o, got)
			}
		}
	}
	var plain bytes.Buffer
	_ = jpeg.Encode(&plain, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil)
	if got := exifOrientation(&plain); got != 1 {
		t.Errorf("JPEG без EXIF: %d, ожидалось 1", got)
	}
	if got := exifOrientation(bytes.NewReader([]byte("not a jpeg"))); got != 1 {
		t.Errorf("не JPEG: %d, ожидалось 1", got)
	}
}

// Картинка 3×2 с пикселями A..F: после поворота они должны встать туда, где
// их увидит человек, — сверено с тем, как метки EXIF описаны в стандарте.
func TestOrient(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for i, name := range "ABCDEF" {
		src.Pix[i*4] = byte(name)
	}
	cases := map[int][]string{
		1: {"ABC", "DEF"},
		2: {"CBA", "FED"},
		3: {"FED", "CBA"},
		4: {"DEF", "ABC"},
		5: {"AD", "BE", "CF"},
		6: {"DA", "EB", "FC"},
		7: {"FC", "EB", "DA"},
		8: {"CF", "BE", "AD"},
	}
	for o, want := range cases {
		out := orient(src, o)
		var rows []string
		for y := 0; y < out.Rect.Dy(); y++ {
			row := ""
			for x := 0; x < out.Rect.Dx(); x++ {
				row += string(rune(out.Pix[(y*out.Rect.Dx()+x)*4]))
			}
			rows = append(rows, row)
		}
		if len(rows) != len(want) {
			t.Errorf("метка %d: %v, ожидалось %v", o, rows, want)
			continue
		}
		for i := range rows {
			if rows[i] != want[i] {
				t.Errorf("метка %d: %v, ожидалось %v", o, rows, want)
				break
			}
		}
	}
}

func TestFitBox(t *testing.T) {
	cases := []struct{ w, h, mw, mh, ew, eh int }{
		{6000, 4000, 1920, 1080, 1620, 1080},
		{4000, 6000, 1920, 1080, 720, 1080},
		{800, 600, 1920, 1080, 800, 600}, // мелкое не увеличиваем
		{6000, 4000, 480, 480, 480, 320},
	}
	for _, c := range cases {
		if w, h := fitBox(c.w, c.h, c.mw, c.mh); w != c.ew || h != c.eh {
			t.Errorf("fitBox(%d×%d в %d×%d) = %d×%d, ожидалось %d×%d", c.w, c.h, c.mw, c.mh, w, h, c.ew, c.eh)
		}
	}
}

// Вертикальный кадр, записанный лёжа (6000×4000 с меткой 6), должен дать
// экранную копию 720×1080, а не 1620×1080, повёрнутую набок.
func TestPhotoCopiesRotated(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "IMG.jpg")
	if err := os.WriteFile(src, jpegWithOrientation(t, 600, 400, 6, binary.LittleEndian), 0o644); err != nil {
		t.Fatal(err)
	}
	thumb, view := filepath.Join(dir, "t.jpg"), filepath.Join(dir, "v.jpg")
	if err := makePhotoCopies(src, thumb, view); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		path string
		w, h int
	}{{view, 400, 600}, {thumb, 320, 480}} {
		f, _ := os.Open(c.path)
		cfg, err := jpeg.DecodeConfig(f)
		f.Close()
		if err != nil || cfg.Width != c.w || cfg.Height != c.h {
			t.Errorf("%s: %d×%d (%v), ожидалось %d×%d", filepath.Base(c.path), cfg.Width, cfg.Height, err, c.w, c.h)
		}
	}
}

// Уменьшение не должно сдвигать цвет: однотонная картинка остаётся той же.
func TestResizeKeepsColor(t *testing.T) {
	img := image.NewYCbCr(image.Rect(0, 0, 301, 199), image.YCbCrSubsampleRatio420)
	y, cb, cr := color.RGBToYCbCr(200, 60, 30)
	for i := range img.Y {
		img.Y[i] = y
	}
	for i := range img.Cb {
		img.Cb[i], img.Cr[i] = cb, cr
	}
	out := fitImage(img, 50, 50, 1)
	if out.Rect.Dx() != 50 || out.Rect.Dy() != 33 {
		t.Fatalf("размер %v", out.Rect)
	}
	r, g, b := out.Pix[0], out.Pix[1], out.Pix[2]
	if absDiff(r, 200) > 3 || absDiff(g, 60) > 3 || absDiff(b, 30) > 3 {
		t.Errorf("цвет уехал: %d,%d,%d", r, g, b)
	}
}

func absDiff(a, b byte) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// PHOTO_BENCH=<папка со снимками> — сколько стоит сборка копий на этой машине.
func TestPhotoBench(t *testing.T) {
	dir := os.Getenv("PHOTO_BENCH")
	if dir == "" {
		t.Skip("PHOTO_BENCH не задан")
	}
	_, files := photoDir(dir)
	if len(files) > 5 {
		files = files[:5]
	}
	out := t.TempDir()
	for _, f := range files {
		start := time.Now()
		thumb, view := filepath.Join(out, "t.jpg"), filepath.Join(out, "v.jpg")
		if err := makePhotoCopies(f, thumb, view); err != nil {
			t.Fatal(err)
		}
		st1, _ := os.Stat(thumb)
		st2, _ := os.Stat(view)
		t.Logf("%s: %s, превью %d КБ, экранная %d КБ", filepath.Base(f),
			time.Since(start).Round(time.Millisecond), st1.Size()>>10, st2.Size()>>10)
	}
}
