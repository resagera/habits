package httpapi

import "testing"

// Папку разрешают из приложения, поэтому путь приходит из браузера: проверка
// пути — первый гейт (второй, с существованием папки, делает агент).
func TestValidAIDir(t *testing.T) {
	cases := []struct {
		in    string
		want  string
		isErr bool
	}{
		{in: "/home/user/project", want: "/home/user/project"},
		{in: "  /home/user/project/  ", want: "/home/user/project"},
		{in: "/home/user/project/../other", want: "/home/user/other"}, // .. сворачивается
		{in: "/home/user/..", want: "/home"},                          // свернулось в /home — это папка
		{in: "relative/path", isErr: true},
		{in: "", isErr: true},
		{in: "/", isErr: true},
		{in: "/proc", isErr: true},
		{in: "/proc/self/environ", isErr: true},
		{in: "/sys/kernel", isErr: true},
		{in: "/dev/sda", isErr: true},
	}
	for _, c := range cases {
		got, msg := validAIDir(c.in)
		if c.isErr {
			if msg == "" {
				t.Errorf("validAIDir(%q) = %q, ждали отказ", c.in, got)
			}
			continue
		}
		if msg != "" || got != c.want {
			t.Errorf("validAIDir(%q) = (%q, %q), ждали %q", c.in, got, msg, c.want)
		}
	}
}

// Папка задачи проверяется по объединённому списку (конфиг агента + папки из
// приложения) — одной и той же функцией для задач и расписаний.
func TestWorkdirAllowedWithAppDirs(t *testing.T) {
	dirs := []string{"/home/user/work", "/srv/app"} // вторая «из приложения»
	allowed := []string{"/home/user/work", "/home/user/work/sub/deep", "/srv/app"}
	denied := []string{"/home/user", "/home/user/work2", "/srv", "/etc", "/home/user/work/../other"}
	for _, wd := range allowed {
		if !workdirAllowed(wd, dirs) {
			t.Errorf("workdirAllowed(%q) = false, ждали true", wd)
		}
	}
	for _, wd := range denied {
		if workdirAllowed(wd, dirs) {
			t.Errorf("workdirAllowed(%q) = true, ждали false", wd)
		}
	}
}
