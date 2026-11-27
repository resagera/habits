package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Папки от сервера проходят ту же проверку, что и свои: сервер может
// расширять белый список, но не обходить его правила.
func TestCheckDir(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, why := checkDir(tmp + "/"); why != "" || got != tmp {
		t.Errorf("checkDir(%q) = (%q, %q), ждали %q", tmp, got, why, tmp)
	}
	for _, bad := range []string{"", "relative", "/", "/proc", "/sys/kernel", "/dev/null",
		filepath.Join(tmp, "nope"), file} {
		if _, why := checkDir(bad); why == "" {
			t.Errorf("checkDir(%q) прошла, ждали отказ", bad)
		}
	}
}

// setRemoteDirs заменяет список целиком (удаление папки на странице — это
// список без неё) и молчит, когда приём папок выключен.
func TestSetRemoteDirsAndAllowed(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "project")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		setRemoteDirs(nil)
		allowRemoteDirs = true
		dirs = nil
	})
	dirs = nil

	setRemoteDirs([]string{sub, "/proc/self", filepath.Join(tmp, "missing")})
	if !dirAllowed(sub) || !dirAllowed(filepath.Join(sub, "inner")) {
		t.Error("папка из приложения должна быть разрешена вместе с вложенными")
	}
	if dirAllowed(tmp) {
		t.Error("родительская папка разрешённой не разрешена")
	}
	if dirAllowed("/proc/self") || dirAllowed(filepath.Join(tmp, "missing")) {
		t.Error("негодные папки от сервера приняты")
	}

	setRemoteDirs([]string{}) // убрали на странице
	if dirAllowed(sub) {
		t.Error("после пустого списка папка должна перестать быть разрешённой")
	}

	allowRemoteDirs = false
	setRemoteDirs([]string{sub})
	if dirAllowed(sub) {
		t.Error("при AI_AGENT_REMOTE_DIRS=0 папки от сервера не принимаются")
	}
}

// id сессии приходит с сервера и подставляется в glob и в --resume.
func TestFindSessionFileRejectsPaths(t *testing.T) {
	for _, bad := range []string{"../../etc/passwd", "a/b", "*", "", "id with space"} {
		if p := findSessionFile("claude", bad); p != "" {
			t.Errorf("findSessionFile(%q) = %q, ждали пусто", bad, p)
		}
	}
	if p := findSessionFile("unknown-tool", "0123456789abcdef"); p != "" {
		t.Errorf("неизвестный инструмент дал путь %q", p)
	}
}
