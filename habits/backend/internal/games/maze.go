// Package games — генераторы для страницы «Игры».
package games

import (
	"math/rand"
	"strings"
)

// Биты открытых сторон клетки. Лабиринт отдаётся клиенту строкой на строку,
// по одному hex-символу на клетку: сорок тысяч клеток «гигантского» размера —
// это 40 КБ текста вместо мегабайта JSON-массивов.
const (
	North = 1 << iota
	East
	South
	West
)

// Maze — готовый лабиринт. Cells[r] — строка hex-символов, по одному на клетку.
//
// Shortest — длина кратчайшего пути от старта до выхода в клетках. Считаем
// здесь: у клиента нет причин знать алгоритмы на графах, а игроку приятно
// увидеть в конце «прошли 180 клеток при кратчайших 96».
type Maze struct {
	Seed     int64    `json:"seed"`
	Cols     int      `json:"cols"`
	Rows     int      `json:"rows"`
	Cells    []string `json:"cells"`
	Start    [2]int   `json:"start"`
	Goal     [2]int   `json:"goal"`
	Shortest int      `json:"shortest"`
}

// Пределы: меньше пяти клеток — не лабиринт, больше ста двадцати тысяч — уже
// не игра, а подвисший телефон.
const (
	MinSide  = 5
	MaxSide  = 400
	MaxCells = 120_000
)

// Generate строит лабиринт «раскопкой» (recursive backtracker, итеративно): из
// текущей клетки идём в случайного непосещённого соседа, упёрлись — шаг назад
// по стеку. Такой лабиринт идеальный: между любыми двумя клетками ровно один
// путь, тупики длинные, проходы извилистые — то, что нужно для игры.
func Generate(cols, rows int, seed int64) Maze {
	cols = clampSide(cols)
	rows = clampSide(rows)
	for cols*rows > MaxCells {
		// режем длинную сторону: форма остаётся похожей на запрошенную
		if cols > rows {
			cols--
		} else {
			rows--
		}
	}

	rnd := rand.New(rand.NewSource(seed))
	open := make([]byte, cols*rows)
	seen := make([]bool, cols*rows)
	idx := func(x, y int) int { return y*cols + x }

	type point struct{ x, y int }
	stack := []point{{0, 0}}
	seen[0] = true

	dirs := []struct {
		dx, dy   int
		bit      byte
		opposite byte
	}{
		{0, -1, North, South},
		{1, 0, East, West},
		{0, 1, South, North},
		{-1, 0, West, East},
	}

	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		moved := false
		// случайный порядок сторон — иначе лабиринт выходит «причёсанным»
		for _, o := range rnd.Perm(4) {
			d := dirs[o]
			nx, ny := cur.x+d.dx, cur.y+d.dy
			if nx < 0 || ny < 0 || nx >= cols || ny >= rows || seen[idx(nx, ny)] {
				continue
			}
			open[idx(cur.x, cur.y)] |= d.bit
			open[idx(nx, ny)] |= d.opposite
			seen[idx(nx, ny)] = true
			stack = append(stack, point{nx, ny})
			moved = true
			break
		}
		if !moved {
			stack = stack[:len(stack)-1]
		}
	}

	const hex = "0123456789abcdef"
	cells := make([]string, rows)
	var b strings.Builder
	for y := 0; y < rows; y++ {
		b.Reset()
		b.Grow(cols)
		for x := 0; x < cols; x++ {
			b.WriteByte(hex[open[idx(x, y)]])
		}
		cells[y] = b.String()
	}

	m := Maze{
		Seed: seed, Cols: cols, Rows: rows, Cells: cells,
		Start: [2]int{0, 0}, Goal: [2]int{cols - 1, rows - 1},
	}
	m.Shortest = shortestPath(open, cols, rows, m.Start, m.Goal)
	return m
}

// shortestPath — обход в ширину по открытым проходам. Лабиринт идеальный,
// поэтому путь единственный, но BFS не делает про это предположений.
func shortestPath(open []byte, cols, rows int, from, to [2]int) int {
	dist := make([]int, cols*rows)
	for i := range dist {
		dist[i] = -1
	}
	idx := func(x, y int) int { return y*cols + x }
	dist[idx(from[0], from[1])] = 0
	queue := [][2]int{from}
	steps := []struct {
		dx, dy int
		bit    byte
	}{{0, -1, North}, {1, 0, East}, {0, 1, South}, {-1, 0, West}}

	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == to {
			return dist[idx(c[0], c[1])] + 1 // в клетках пути, вместе со стартовой
		}
		for _, s := range steps {
			if open[idx(c[0], c[1])]&s.bit == 0 {
				continue
			}
			nx, ny := c[0]+s.dx, c[1]+s.dy
			if nx < 0 || ny < 0 || nx >= cols || ny >= rows || dist[idx(nx, ny)] >= 0 {
				continue
			}
			dist[idx(nx, ny)] = dist[idx(c[0], c[1])] + 1
			queue = append(queue, [2]int{nx, ny})
		}
	}
	return 0
}

func clampSide(v int) int {
	if v < MinSide {
		return MinSide
	}
	if v > MaxSide {
		return MaxSide
	}
	return v
}
