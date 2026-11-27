package main

import (
	"strings"
	"testing"
)

func line(args []string) string { return strings.Join(args, " ") }

// Продолжение дописывает ту же сессию, форк отделяет новую: разница — ровно в
// одном флаге, и перепутать их нельзя (исходную сессию форк не трогает).
func TestClaudeArgsFork(t *testing.T) {
	bypass = true
	t.Cleanup(func() { bypass = true })

	plain := line(claudeArgs(frame{}))
	if strings.Contains(plain, "--resume") || strings.Contains(plain, "--fork-session") {
		t.Errorf("новая сессия не должна ничего возобновлять: %s", plain)
	}
	if !strings.Contains(plain, "--dangerously-skip-permissions") {
		t.Errorf("при bypass=true ждали флаг прав: %s", plain)
	}

	cont := line(claudeArgs(frame{SessionID: "sess-1"}))
	if !strings.Contains(cont, "--resume sess-1") {
		t.Errorf("продолжение идёт с --resume: %s", cont)
	}
	if strings.Contains(cont, "--fork-session") {
		t.Errorf("продолжение не форк: %s", cont)
	}

	fork := line(claudeArgs(frame{SessionID: "sess-1", Fork: true}))
	if !strings.Contains(fork, "--resume sess-1 --fork-session") {
		t.Errorf("форк идёт с --resume <id> --fork-session: %s", fork)
	}

	// форк без сессии форкать нечего — флага быть не должно
	if got := line(claudeArgs(frame{Fork: true})); strings.Contains(got, "--fork-session") {
		t.Errorf("форк без id сессии: %s", got)
	}

	// план остаётся планом и в форке: правок файлов нет
	plan := line(claudeArgs(frame{SessionID: "s", Fork: true, Mode: "plan"}))
	if !strings.Contains(plan, "--permission-mode plan") ||
		strings.Contains(plan, "--dangerously-skip-permissions") {
		t.Errorf("план + форк: %s", plan)
	}

	// свой --fork-session в доп. параметрах отфильтрован (ставим его сами)
	if got := line(claudeArgs(frame{Params: "--fork-session --max-turns 3"})); strings.Contains(got, "--fork-session") {
		t.Errorf("--fork-session из параметров должен отфильтроваться: %s", got)
	}
}

func TestCodexArgsFork(t *testing.T) {
	bypass = true
	t.Cleanup(func() { bypass = true })

	cont := line(codexArgs(frame{SessionID: "th-1"}))
	if !strings.Contains(cont, "exec resume th-1") {
		t.Errorf("продолжение codex — exec resume: %s", cont)
	}

	fork := line(codexArgs(frame{SessionID: "th-1", Fork: true}))
	if !strings.Contains(fork, "exec fork th-1") {
		t.Errorf("форк codex — exec fork <id>: %s", fork)
	}
	if strings.Contains(fork, "--worktree") {
		t.Errorf("worktree не просили: %s", fork)
	}

	wt := line(codexArgs(frame{SessionID: "th-1", Fork: true, Worktree: true}))
	if !strings.Contains(wt, "exec fork th-1") || !strings.Contains(wt, "--worktree") {
		t.Errorf("форк в worktree: %s", wt)
	}
	if !strings.HasSuffix(wt, " -") {
		t.Errorf("промпт идёт через stdin, последним аргументом ждали «-»: %s", wt)
	}

	// --worktree, заданный руками, не дублируется
	dup := codexArgs(frame{SessionID: "th-1", Fork: true, Worktree: true, Params: "--worktree"})
	if n := strings.Count(line(dup), "--worktree"); n != 1 {
		t.Errorf("--worktree повторился %d раз: %s", n, line(dup))
	}

	// без сессии форкать нечего: обычный exec, без worktree
	plain := line(codexArgs(frame{Fork: true, Worktree: true}))
	if strings.Contains(plain, "fork") || strings.Contains(plain, "--worktree") {
		t.Errorf("форк без id сессии: %s", plain)
	}
}
