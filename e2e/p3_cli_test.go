package e2e

import (
	"testing"
	"time"
)

// Phase 3 CLI: --timeout, --steps, --tasks and --alloc-mb set the run's
// budget, the outermost level of every scope's (I6).

const (
	recurse = "let f = fn(n) { if (n == 0) { 0 } else { f(n - 1) } };\n"
	grow    = `let grow = fn(n, acc) { if (n == 0) { acc } else { grow(n - 1, push(acc, "x")) } };` + "\n"
	nap     = "let nap = fn() { sleep(600000) };\n"
)

func TestBudgetFlags(t *testing.T) {
	t.Parallel()
	runInline(t, []inline{
		{name: "--steps", flags: []string{"--steps", "1000"}, src: recurse + "f(5000)",
			err: "budget exceeded: steps (limit 1000)", at: "1:17"},
		{name: "--steps enough", flags: []string{"--steps", "100000"}, src: recurse + "puts(f(500))", stdout: "0\n"},
		{name: "--alloc-mb", flags: []string{"--alloc-mb", "1"}, src: grow + "len(grow(5000, []))",
			err: "budget exceeded: alloc (limit 1048576 bytes)", at: "1:64"},
		{name: "--alloc-mb enough", flags: []string{"--alloc-mb", "1024"}, src: grow + "puts(len(grow(5000, [])))",
			stdout: "5000\n"},
		{name: "--tasks", flags: []string{"--tasks", "3"}, src: nap + "for i in [1, 2, 3, 4] {\n  spawn nap()\n}",
			err: "budget exceeded: tasks (limit 3 live tasks)", at: "3:3"},
		// A finished task frees its place before its awaiter wakes, so a
		// loop that spawns and awaits one at a time never trips --tasks 1.
		{name: "--tasks 1 one at a time", flags: []string{"--tasks", "1"}, stdout: "[1, 2, 3, 4, 5, 6, 7, 8, 9, 10]\n",
			src: "let id = fn(x) { x };\nputs(for i in [1, 2, 3, 4, 5, 6, 7, 8, 9, 10] { await spawn id(i) })"},
		{name: "--timeout 0 is no limit", flags: []string{"--timeout", "0"}, src: "sleep(200); puts(1)", stdout: "1\n"},
		{name: "--timeout 0 keeps scope timeouts", flags: []string{"--timeout", "0"},
			src: "scope (timeout_ms: 50) { sleep(600000) }",
			err: "budget exceeded: timeout (limit 50 ms)", at: "1:26"},
		{name: "flags in any order", flags: []string{"--steps", "1000000", "--allow", "env=ARV_NONE", "--tasks", "5", "--timeout", "30s", "--alloc-mb", "64"},
			src: "puts(len(getenv(\"ARV_NONE\")))", stdout: "0\n"},
	})
}

// A run whose --timeout passes while it sleeps stops within budgetBound.
func TestTimeoutFlag(t *testing.T) {
	t.Parallel()
	took := timedFromMark(t, spec{
		script: "p3_timeout.arv", allow: loopback,
		vars: map[string]string{"OUTER": "600000", "INNER": "sleep(600000)"},
		args: []string{"run", "--timeout", "100ms", "--allow", "net=127.0.0.1", "{{SCRIPT}}"},
		code: 1, stderrHas: []string{"error: budget exceeded: timeout (limit 100 ms) (at ", "p3_timeout.arv:4:30)\n"},
	})
	if took > budgetBound {
		t.Fatalf("--timeout 100ms stopped the run after %s (bound %s)", took, budgetBound)
	}
}

// I6 across the CLI and the language: a scope's larger limit cannot raise
// the run's, and two nested limits that both pass in one charge report
// the smaller (this used to name the inner 150).
func TestFlagsOnlyShrink(t *testing.T) {
	t.Parallel()
	runInline(t, []inline{
		{name: "steps", flags: []string{"--steps", "100"}, src: recurse + "scope (steps: 150) { f(5000) }",
			err: "budget exceeded: steps (limit 100)", at: "1:21"},
		{name: "steps between scopes", src: recurse + "scope (steps: 100) { scope (steps: 150) { f(5000) } }",
			err: "budget exceeded: steps (limit 100)", at: "1:21"},
		{name: "steps far apart", flags: []string{"--steps", "1000"}, src: recurse + "scope (steps: 1000000) { f(5000) }",
			err: "budget exceeded: steps (limit 1000)", at: "1:26"},
		{name: "alloc", flags: []string{"--alloc-mb", "1"}, src: grow + "scope (alloc_mb: 1024) { len(grow(5000, [])) }",
			err: "budget exceeded: alloc (limit 1048576 bytes)", at: "1:64"},
		{name: "alloc between scopes", src: grow + "scope (alloc_mb: 1) { scope (alloc_mb: 2) { len(grow(5000, [])) } }",
			err: "budget exceeded: alloc (limit 1048576 bytes)", at: "1:64"},
		{name: "timeout", flags: []string{"--timeout", "100ms"}, src: "scope (timeout_ms: 600000) { sleep(600000) }",
			err: "budget exceeded: timeout (limit 100 ms)", at: "1:30"},
		{name: "tasks", flags: []string{"--tasks", "3"}, src: nap + "scope (tasks: 100) { for i in [1, 2, 3, 4] { spawn nap() } }",
			err: "budget exceeded: tasks (limit 3 live tasks)", at: "2:46"},
	})
}

func TestBadBudgetFlags(t *testing.T) {
	t.Parallel()
	const n = "want a whole number from 1 to "
	cases := []inline{
		{name: "steps 0", flags: []string{"--steps", "0"}, err: `--steps: ` + n + `9223372036854775807, got "0"`},
		{name: "steps negative", flags: []string{"--steps", "-1"}, err: `--steps: ` + n + `9223372036854775807, got "-1"`},
		{name: "steps word", flags: []string{"--steps", "lots"}, err: `--steps: ` + n + `9223372036854775807, got "lots"`},
		{name: "tasks too many", flags: []string{"--tasks", "2147483648"}, err: `--tasks: ` + n + `2147483647, got "2147483648"`},
		{name: "alloc 0", flags: []string{"--alloc-mb", "0"}, err: `--alloc-mb: ` + n + `8796093022207, got "0"`},
		{name: "alloc overflow", flags: []string{"--alloc-mb", "8796093022208"}, err: `--alloc-mb: ` + n + `8796093022207, got "8796093022208"`},
		{name: "timeout negative", flags: []string{"--timeout", "-1s"}, err: `--timeout: want a duration like 30s or 10m, got "-1s"`},
		{name: "timeout no unit", flags: []string{"--timeout", "5"}, err: `--timeout: want a duration like 30s or 10m, got "5"`},
		{name: "unknown flag", flags: []string{"--stepz", "5"}, err: `unknown flag "--stepz"`},
	}
	for i := range cases {
		cases[i].usage = true
		cases[i].src = `puts("must not run")`
	}
	runInline(t, cases)
}

// The default timeout is 10 minutes; a run that does not hit it is not
// slowed by it.
func TestDefaultTimeoutDoesNotFire(t *testing.T) {
	t.Parallel()
	start := time.Now()
	runInline(t, []inline{{name: "short sleep", src: "sleep(100); puts(1)", stdout: "1\n"}})
	if took := time.Since(start); took > time.Minute {
		t.Fatalf("took %s", took)
	}
}
