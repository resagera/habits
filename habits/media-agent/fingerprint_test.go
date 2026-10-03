package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand"
	"testing"
)

// pcm16 — звук в тот же формат, в котором его отдаёт ffmpeg.
func pcm16(samples []float64) []byte {
	buf := &bytes.Buffer{}
	for _, s := range samples {
		_ = binary.Write(buf, binary.LittleEndian, int16(max(-1, min(1, s))*32000))
	}
	return buf.Bytes()
}

// melody — «музыка»: ноты по 120 мс, одинаковые при одном и том же зерне.
func melody(seed int64, seconds float64) []float64 {
	rnd := rand.New(rand.NewSource(seed))
	out := make([]float64, int(seconds*fpRate))
	note, freq := 0, 220.0
	for i := range out {
		if i%(fpRate*12/100) == 0 {
			note++
			freq = 200 + 120*float64(rnd.Intn(12))
		}
		t := float64(i) / fpRate
		out[i] = 0.4*math.Sin(2*math.Pi*freq*t) + 0.2*math.Sin(2*math.Pi*freq*2.5*t)
	}
	return out
}

func noise(seed int64, seconds float64) []float64 {
	rnd := rand.New(rand.NewSource(seed))
	out := make([]float64, int(seconds*fpRate))
	for i := range out {
		out[i] = 0.3 * rnd.NormFloat64()
	}
	return out
}

func silence(seconds float64) []float64 { return make([]float64, int(seconds*fpRate)) }

func trackOf(t *testing.T, parts ...[]float64) *track {
	t.Helper()
	all := []float64{}
	for _, p := range parts {
		all = append(all, p...)
	}
	tr, err := printsFrom(bytes.NewReader(pcm16(all)))
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// Одна и та же «заставка» в двух сериях стоит на разных местах — находим её и
// в той, и в другой, а чужая музыка общей не считается.
func TestCommonRangesFindsIntro(t *testing.T) {
	// куски длинные не для красоты: сдвиги с малым наложением отбрасываются
	intro := melody(7, 30)
	a := trackOf(t, noise(1, 150), intro, noise(2, 150))
	b := trackOf(t, noise(3, 220), intro, noise(4, 80))
	ranges := commonRanges(a, b)
	found := false
	for _, r := range ranges {
		if math.Abs(r.start-220) < 2 && math.Abs(r.end-250) < 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("заставка на 220–250 с не нашлась: %v", ranges)
	}
	// у двух разных шумов общего быть не должно
	c := trackOf(t, noise(5, 300))
	d := trackOf(t, noise(6, 300))
	if rs := commonRanges(c, d); len(rs) > 0 {
		t.Fatalf("в шуме нашлось общее: %v", rs)
	}
}

// После заставки — пауза, а совпадение кончается раньше (в конце заставки
// объявляют название серии): отметка дотягивается до паузы, но не дальше.
func TestSnapForwardToPause(t *testing.T) {
	tr := trackOf(t, melody(8, 20), silence(1), noise(9, 20))
	// совпадение «кончилось» на 17-й секунде, хотя музыка играет до 20-й
	if got := snapForward(tr, 0, 17); math.Abs(got-20) > 1 {
		t.Fatalf("не дотянулось до паузы на 20 с: %.1f", got)
	}
	// паузы поблизости нет — конец остаётся прежним
	loud := trackOf(t, melody(8, 20), noise(9, 20))
	if got := snapForward(loud, 0, 17); math.Abs(got-17) > 0.2 {
		t.Fatalf("без паузы конец менять нельзя: %.1f", got)
	}
}

// Сезон размечается целиком: у каждой серии своя отметка, а титры, которые
// «нашлись» не там, подтягиваются к сезонным.
func TestMedianCredits(t *testing.T) {
	marks := []seriesMarks{
		{CreditsFromEnd: 68}, {CreditsFromEnd: 69}, {CreditsFromEnd: 68.5},
		{CreditsFromEnd: 237}, {},
	}
	mid, ok := medianCredits(marks)
	if !ok || math.Abs(mid-68.5) > 0.6 {
		t.Fatalf("медиана титров: %v %v", mid, ok)
	}
}
