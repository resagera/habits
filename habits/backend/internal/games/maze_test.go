package games

import (
	"strings"
	"testing"
)

// Лабиринт должен быть связным и без «висячих» проходов в стену: по этим двум
// свойствам сразу видно, что генератор не развалился.
func TestGenerateIsPerfectMaze(t *testing.T) {
	m := Generate(20, 14, 42)
	if m.Cols != 20 || m.Rows != 14 || len(m.Cells) != 14 {
		t.Fatalf("размер %dx%d, строк %d", m.Cols, m.Rows, len(m.Cells))
	}

	open := func(x, y int) int {
		v := strings.IndexByte("0123456789abcdef", m.Cells[y][x])
		if v < 0 {
			t.Fatalf("не hex в строке %d: %q", y, m.Cells[y])
		}
		return v
	}

	// проходы согласованы с обеих сторон и не ведут за край
	for y := 0; y < m.Rows; y++ {
		for x := 0; x < m.Cols; x++ {
			v := open(x, y)
			if v&North != 0 && (y == 0 || open(x, y-1)&South == 0) {
				t.Fatalf("клетка %d,%d: проход на север в никуда", x, y)
			}
			if v&West != 0 && (x == 0 || open(x-1, y)&East == 0) {
				t.Fatalf("клетка %d,%d: проход на запад в никуда", x, y)
			}
		}
	}

	// связность: обход в ширину от старта должен накрыть все клетки
	seen := make([]bool, m.Cols*m.Rows)
	queue := [][2]int{m.Start}
	seen[m.Start[1]*m.Cols+m.Start[0]] = true
	count := 1
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		v := open(c[0], c[1])
		steps := []struct {
			dx, dy int
			bit    int
		}{{0, -1, North}, {1, 0, East}, {0, 1, South}, {-1, 0, West}}
		for _, s := range steps {
			if v&s.bit == 0 {
				continue
			}
			nx, ny := c[0]+s.dx, c[1]+s.dy
			if seen[ny*m.Cols+nx] {
				continue
			}
			seen[ny*m.Cols+nx] = true
			count++
			queue = append(queue, [2]int{nx, ny})
		}
	}
	if count != m.Cols*m.Rows {
		t.Fatalf("достижимо %d клеток из %d — лабиринт распался", count, m.Cols*m.Rows)
	}
}

// Один и тот же seed обязан давать один и тот же лабиринт: иначе «переиграть
// тот же» и шаринг ссылкой станут невозможны.
func TestGenerateIsRepeatable(t *testing.T) {
	a := Generate(30, 20, 7)
	b := Generate(30, 20, 7)
	for i := range a.Cells {
		if a.Cells[i] != b.Cells[i] {
			t.Fatalf("строка %d отличается при том же seed", i)
		}
	}
	if c := Generate(30, 20, 8); c.Cells[0] == a.Cells[0] && c.Cells[1] == a.Cells[1] {
		t.Fatal("разные seed дали одинаковое начало — случайность не работает")
	}
}

func TestGenerateClampsSize(t *testing.T) {
	small := Generate(1, 1, 1)
	if small.Cols != MinSide || small.Rows != MinSide {
		t.Fatalf("малый размер не поднят до минимума: %dx%d", small.Cols, small.Rows)
	}
	big := Generate(400, 400, 1)
	if big.Cols*big.Rows > MaxCells {
		t.Fatalf("клеток %d — больше потолка", big.Cols*big.Rows)
	}
}

// Кратчайший путь не короче «по прямой» и не длиннее всех клеток разом.
func TestShortestPath(t *testing.T) {
	m := Generate(20, 14, 42)
	minimal := m.Cols + m.Rows - 1 // путь по краю, если бы стен не было
	if m.Shortest < minimal {
		t.Fatalf("кратчайший путь %d короче прямого %d", m.Shortest, minimal)
	}
	if m.Shortest > m.Cols*m.Rows {
		t.Fatalf("кратчайший путь %d длиннее всего лабиринта", m.Shortest)
	}
	// путь должен существовать при любом размере (лабиринт связный)
	for _, size := range [][2]int{{5, 5}, {40, 7}, {9, 33}} {
		if g := Generate(size[0], size[1], 3); g.Shortest <= 0 {
			t.Fatalf("%dx%d: путь не найден", size[0], size[1])
		}
	}
}
