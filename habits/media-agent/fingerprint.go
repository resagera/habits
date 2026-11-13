package main

// Заставка и титры по звуку.
//
// У серий одного сезона заставка — один и тот же кусок звука. Значит, его можно
// найти, не размечая руками: берём две серии, ищем самый длинный одинаковый
// отрезок в начале — это и есть заставка, в конце — титры. Так же делает Intro
// Skipper у Jellyfin; общей базы таймингов для сериалов нет (AniSkip — только
// аниме, по id MyAnimeList), а звук есть всегда.
//
// Сравнивать сами звуковые волны нельзя: у разных серий разное сведение и
// разный битрейт. Сравниваем «отпечатки» — по биту на пару соседних полос
// спектра: бит говорит, стала ли эта полоса громче соседней по сравнению с
// предыдущим кадром. Громкость, кодек и лёгкий шум на такие биты почти не
// влияют, а музыка даёт одинаковую последовательность.
//
// Звук берём моно 8 кГц: музыку опознать хватает с запасом, а данных в 20 раз
// меньше, чем у оригинала, — на i3 сезон разбирается минуты.
//
// Окно (256 мс) вчетверо длиннее шага (64 мс) НЕ для красоты: заставка в разных
// сериях начинается не в круглое число кадров, и при коротком окне те же ноты
// попадают в разные его половины — отпечатки расходятся, и совпадение видно
// только там, где сетка кадров совпала случайно (в самом начале файла). С
// длинным окном и мелким шагом рассинхрон уже почти не мешает.

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"math/bits"
	"os/exec"
	"sort"
	"strconv"
)

const (
	fpRate  = 8000 // Гц, моно
	fpFrame = 2048 // окно 256 мс
	fpHop   = 512  // шаг 64 мс → 15,6 отпечатка в секунду
	fpBands = 21   // полос спектра, бит на каждую пару соседних
	fpBits  = fpBands - 1
	fpHead  = 1200 // сколько секунд слушаем с начала серии: бывает длинный пролог
	fpTail  = 900  // и с конца
	fpStep  = float64(fpHop) / float64(fpRate)
)

// track — разобранный кусок звука: отпечатки и громкость по тем же кадрам.
// Громкость нужна, чтобы дотянуть отметку до паузы (см. snapForward).
type track struct {
	prints []uint32
	power  []float64 // средний квадрат отсчётов в окне
}

func (t *track) len() int       { return len(t.prints) }
func (t *track) dur() float64   { return fpTime(len(t.prints) - 1) }
func fpTime(i int) float64      { return float64(i+1) * fpStep }
func fpFrameAt(sec float64) int { return int(math.Round(sec/fpStep)) - 1 }

// audioPrints — отпечатки начала (или конца) файла.
func audioPrints(ctx context.Context, path string, tail bool) (*track, error) {
	args := []string{"-v", "error"}
	if tail {
		args = append(args, "-sseof", "-"+strconv.Itoa(fpTail))
	}
	args = append(args, "-i", path, "-vn", "-map", "0:a:0", "-ac", "1",
		"-ar", strconv.Itoa(fpRate), "-f", "s16le")
	if !tail {
		args = append(args, "-t", strconv.Itoa(fpHead))
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", append(args, "-")...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	tr, perr := printsFrom(out)
	_, _ = io.Copy(io.Discard, out) // ffmpeg не должен упереться в закрытую трубу
	werr := cmd.Wait()
	if perr != nil {
		return nil, perr
	}
	if werr != nil && tr.len() == 0 {
		return nil, werr
	}
	if tr.len() < fpRate/fpHop {
		return nil, errors.New("в файле нет звука")
	}
	return tr, nil
}

// printsFrom читает поток 16-битных отсчётов и считает отпечатки на ходу:
// целиком звук в памяти не держим.
func printsFrom(r io.Reader) (*track, error) {
	br := bufio.NewReaderSize(r, 1<<16)
	win := make([]float64, fpFrame) // окно, сдвигается на fpHop
	hann := hannWindow()
	re := make([]float64, fpFrame)
	im := make([]float64, fpFrame)
	edges := bandEdges()
	cur := make([]float64, fpBands)
	prev := make([]float64, fpBands)
	first := true
	tr := &track{prints: []uint32{}, power: []float64{}}
	filled := 0
	raw := make([]byte, 2*fpFrame) // первое окно длиннее шага
	for {
		// первое окно набираем целиком, дальше дописываем по fpHop отсчётов
		need := fpHop
		if filled < fpFrame {
			need = fpFrame - filled
		}
		buf := raw[:2*need]
		if _, err := io.ReadFull(br, buf); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return tr, nil
			}
			return tr, err
		}
		if filled < fpFrame {
			for i := 0; i < need; i++ {
				win[filled+i] = float64(int16(binary.LittleEndian.Uint16(buf[2*i:]))) / 32768
			}
			filled += need
		} else {
			copy(win, win[fpHop:])
			for i := 0; i < fpHop; i++ {
				win[fpFrame-fpHop+i] = float64(int16(binary.LittleEndian.Uint16(buf[2*i:]))) / 32768
			}
		}
		power := 0.0
		for i := range win {
			power += win[i] * win[i]
			re[i], im[i] = win[i]*hann[i], 0
		}
		fft(re, im)
		for b := 0; b < fpBands; b++ {
			sum := 0.0
			for k := edges[b]; k < edges[b+1]; k++ {
				sum += re[k]*re[k] + im[k]*im[k]
			}
			cur[b] = math.Log1p(sum * 1e4)
		}
		if !first {
			var fp uint32
			for b := 0; b < fpBits; b++ {
				if (cur[b]-cur[b+1])-(prev[b]-prev[b+1]) > 0 {
					fp |= 1 << uint(b)
				}
			}
			tr.prints = append(tr.prints, fp)
			tr.power = append(tr.power, power/float64(fpFrame))
		}
		first = false
		copy(prev, cur)
	}
}

func hannWindow() []float64 {
	w := make([]float64, fpFrame)
	for i := range w {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(fpFrame-1))
	}
	return w
}

// bandEdges — границы полос, логарифмом: низкие частоты дробим мельче, там вся
// музыка и голос. Берём 100–3500 Гц: выше половины частоты дискретизации
// ничего нет, ниже 100 Гц гудит фон.
func bandEdges() []int {
	const lo, hi = 26, 896
	edges := make([]int, fpBands+1)
	for i := range edges {
		k := int(math.Round(lo * math.Pow(float64(hi)/lo, float64(i)/float64(fpBands))))
		if i > 0 && k <= edges[i-1] {
			k = edges[i-1] + 1
		}
		edges[i] = k
	}
	return edges
}

// fft — обычное разложение по основанию 2 на месте; длина всегда fpFrame.
func fft(re, im []float64) {
	n := len(re)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j |= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		ang := -2 * math.Pi / float64(length)
		wr, wi := math.Cos(ang), math.Sin(ang)
		for i := 0; i < n; i += length {
			cr, ci := 1.0, 0.0
			for j := 0; j < length/2; j++ {
				ur, ui := re[i+j], im[i+j]
				vr := re[i+j+length/2]*cr - im[i+j+length/2]*ci
				vi := re[i+j+length/2]*ci + im[i+j+length/2]*cr
				re[i+j], im[i+j] = ur+vr, ui+vi
				re[i+j+length/2], im[i+j+length/2] = ur-vr, ui-vi
				cr, ci = cr*wr-ci*wi, cr*wi+ci*wr
			}
		}
	}
}

// --- поиск общего куска ---

const (
	fpNear    = 6    // столько несовпавших бит из 20 ещё считаем совпадением
	fpSmooth  = 8    // окно сглаживания, ±0,5 с: отдельные промахи не в счёт
	fpCore    = 0.5  // доля совпавших кадров в окне: ядро совпадения
	fpEdge    = 0.15 // край совпадения; случайные созвучия дают около 0,07
	fpMinRun  = 10   // короче 10 с — не заставка, а случайное созвучие
	fpMaxRun  = 400  // длиннее — значит, совпали не музыкой, а тишиной
	fpOverlap = 2000 // сдвиги с наложением меньше двух минут не в счёт
	fpTries   = 5    // сколько лучших сдвигов проверяем подробно
	fpApart   = 32   // ±2 с вокруг выбранного сдвига — то же самое совпадение
)

// commonRanges — куски b, которые звучат так же, как какие-то куски a; секунды
// от начала куска b. Отдаём все найденные, а не один: в начале серии общих
// кусков обычно два — заставка студии-переводчика в самом начале и заставка
// сериала после пролога, и выбирать между ними должен тот, кто знает, зачем
// спрашивал.
//
// Сдвиг ищем перебором: для каждого считаем долю кадров, сошедшихся с точностью
// до fpNear бит. У случайного сдвига это ~0,07, у настоящего — втрое больше, а
// перебор 14 тысяч кадров на 14 тысяч занимает четверть секунды. Голосование по
// точному совпадению отпечатков было бы дешевле, но внутри заставки ровно те же
// 20 бит выпадают лишь у каждого двадцатого кадра — слишком тонкая ниточка.
func commonRanges(a, b *track) []scanRange {
	out := []scanRange{}
	for _, off := range bestOffsets(a, b) {
		if s, e, ok := runAt(a.prints, b.prints, off); ok {
			out = append(out, scanRange{s, e})
		}
	}
	return out
}

// bestOffsets — сдвиги, при которых куски похожи больше всего; соседние сдвиги
// (то же совпадение, сползшее на кадр) отбрасываем.
func bestOffsets(a, b *track) []int {
	na, nb := a.len(), b.len()
	hits := make([]int32, na+nb-1)
	for i := 0; i < na; i++ {
		ai, base := a.prints[i], i+nb-1
		for j, bj := range b.prints {
			if bits.OnesCount32(ai^bj) <= fpNear {
				hits[base-j]++
			}
		}
	}
	type cand struct {
		off   int
		ratio float64
	}
	all := make([]cand, 0, len(hits))
	for k, n := range hits {
		off := k - (nb - 1)
		overlap := min(na, nb+off) - max(0, off)
		if overlap < fpOverlap {
			continue // короткий хвост даёт случайную долю под единицу
		}
		all = append(all, cand{off, float64(n) / float64(overlap)})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ratio > all[j].ratio })
	out := []int{}
	for _, c := range all {
		near := false
		for _, o := range out {
			near = near || (c.off > o-fpApart && c.off < o+fpApart)
		}
		if near {
			continue
		}
		if out = append(out, c.off); len(out) >= fpTries {
			break
		}
	}
	return out
}

// runAt — самый длинный участок совпадения при заданном сдвиге.
func runAt(a, b []uint32, off int) (start, end float64, ok bool) {
	match := make([]bool, len(b))
	for j := range b {
		i := j + off
		if i < 0 || i >= len(a) {
			continue
		}
		match[j] = bits.OnesCount32(a[i]^b[j]) <= fpNear
	}
	// Сглаживание: смотрим не отдельный кадр, а долю совпавших в окне вокруг
	// него. Порогов два: по высокому ищем ядро совпадения, по низкому тянем
	// края — в конце заставки музыка уже смешана с началом сцены, и отпечатки
	// сходятся хуже.
	sum := make([]int, len(match)+1)
	for i, m := range match {
		sum[i+1] = sum[i]
		if m {
			sum[i+1]++
		}
	}
	ratio := func(j int) float64 {
		lo, hi := max(0, j-fpSmooth), min(len(match), j+fpSmooth+1)
		return float64(sum[hi]-sum[lo]) / float64(hi-lo)
	}
	bestS, bestE, curS := -1, -1, -1
	for j := range match {
		switch good := ratio(j) >= fpCore; {
		case good && curS < 0:
			curS = j
		case !good && curS >= 0:
			if bestS < 0 || j-curS > bestE-bestS {
				bestS, bestE = curS, j
			}
			curS = -1
		}
	}
	if curS >= 0 && (bestS < 0 || len(match)-curS > bestE-bestS) {
		bestS, bestE = curS, len(match)
	}
	if bestS < 0 {
		return 0, 0, false
	}
	for bestS > 0 && ratio(bestS-1) >= fpEdge {
		bestS--
	}
	for bestE < len(match) && ratio(bestE) >= fpEdge {
		bestE++
	}
	start, end = fpTime(bestS), fpTime(bestE)
	if end-start < fpMinRun || end-start > fpMaxRun {
		return 0, 0, false
	}
	return start, end, true
}

// --- дотягивание до паузы ---
//
// Последние секунды заставки в разных сериях звучат по-разному: поверх той же
// музыки называют эпизод. Совпадение там кончается, а заставка ещё идёт — и
// «Пропустить» бросало бы зрителя на последние титры. Зато между заставкой и
// первой сценой почти всегда есть тишина: до неё и дотягиваем.

const (
	fpSnapAhead = 20.0 // дальше этого паузу не ищем
	fpSnapQuiet = 0.3  // тишиной считаем паузу не короче 0,3 с
	fpSnapLevel = 0.02 // тихо — это сотая доля громкости найденного куска
)

// snapForward — конец отрезка, дотянутый до ближайшей паузы после него.
// Возвращает НАЧАЛО паузы: там смолкла музыка заставки. Конец паузы брать
// нельзя — за ней идёт тихий первый кадр сцены, и «Пропустить» съедало бы
// начало серии.
func snapForward(t *track, from, to float64) float64 {
	thr := fpSnapLevel * meanPower(t, from, to)
	if thr == 0 {
		return to
	}
	i, limit := max(0, fpFrameAt(to)), min(t.len(), fpFrameAt(to+fpSnapAhead))
	need := int(math.Round(fpSnapQuiet / fpStep))
	quiet := 0
	for ; i < limit; i++ {
		if t.power[i] > thr {
			quiet = 0
			continue
		}
		if quiet++; quiet >= need {
			return fpTime(i - quiet + 1)
		}
	}
	return to
}

func meanPower(t *track, from, to float64) float64 {
	lo, hi := max(0, fpFrameAt(from)), min(t.len(), fpFrameAt(to))
	if hi <= lo {
		return 0
	}
	sum := 0.0
	for _, p := range t.power[lo:hi] {
		sum += p
	}
	return sum / float64(hi-lo)
}
