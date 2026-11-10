package arv

import (
	"errors"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// Leak rule: no goroutine outlives Run, on any of the Phase 3 paths that
// end tasks early. Each program leaves tasks blocked (sleeping, or waiting
// for a task slot) when its budget runs out, so the run must cancel them
// and wait for them (I4) before it returns.
func TestNoGoroutineOutlivesRun(t *testing.T) {
	const sleeper = "let nap = fn() { sleep(600000) };\n"
	const spinner = "let spin = fn(n) { spin(n + 1) };\n"
	const grower = `let grow = fn(acc) { grow(push(acc, "xxxxxxxxxxxxxxxx")) };` + "\n"
	cases := []struct {
		name, src string
		limits    runtime.Limits
		want      string // the error's message; "" for success
	}{
		{"scope timeout with sleeping tasks",
			sleeper + "scope (timeout_ms: 50) { for i in [1, 2, 3] { spawn nap() }; sleep(600000) }",
			runtime.Limits{}, "budget exceeded: timeout (limit 50 ms)"},
		{"nested timeouts",
			sleeper + "scope (timeout_ms: 50) { scope (timeout_ms: 600000) { spawn nap(); nap() } }",
			runtime.Limits{}, "budget exceeded: timeout (limit 50 ms)"},
		{"run timeout",
			sleeper + "for i in [1, 2, 3] { spawn nap() }; nap()",
			runtime.Limits{Timeout: 50 * time.Millisecond}, "budget exceeded: timeout (limit 50 ms)"},
		{"spawn blocked on a full scope when the timeout fires",
			sleeper + "scope (timeout_ms: 50, tasks: 2) { for i in [1, 2, 3, 4, 5] { spawn nap() } }",
			runtime.Limits{}, "budget exceeded: timeout (limit 50 ms)"},
		{"steps spent by one task while siblings sleep",
			sleeper + spinner + "scope (steps: 5000) { spawn nap(); spawn nap(); await spawn spin(0) }",
			runtime.Limits{}, "budget exceeded: steps (limit 5000)"},
		{"run steps spent by one task while siblings sleep",
			sleeper + spinner + "spawn nap(); spawn nap(); await spawn spin(0)",
			runtime.Limits{Steps: 5000}, "budget exceeded: steps (limit 5000)"},
		{"alloc spent by one task while siblings sleep",
			sleeper + grower + "scope (alloc_mb: 1) { spawn nap(); await spawn grow([]) }",
			runtime.Limits{}, "budget exceeded: alloc (limit 1048576 bytes)"},
		{"live-task cap with sleeping tasks",
			sleeper + "for i in [1, 2, 3, 4, 5, 6] { spawn nap() }",
			runtime.Limits{LiveTasks: 3}, "budget exceeded: tasks (limit 3 live tasks)"},
		{"await on an array with a failing task",
			sleeper + "let bad = fn() { 1 / 0 };\nawait [spawn nap(), spawn bad(), spawn nap()]",
			runtime.Limits{}, "division by zero"},
		{"await on an array with a non-task",
			sleeper + "await [spawn nap(), 7]",
			runtime.Limits{}, "await: want TASK, got INTEGER (element 1)"},
		{"for ... in fan-out under a tasks limit",
			"let w = fn(x) { x * 2 };\nscope (tasks: 3) { await for x in [1, 2, 3, 4, 5, 6, 7, 8] { spawn w(x) } }",
			runtime.Limits{}, ""},
		{"error in a for body while tasks sleep",
			sleeper + "for x in [1, 2, 0] { spawn nap(); 10 / x }",
			runtime.Limits{}, "division by zero"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := goruntime.NumGoroutine()
			start := time.Now()
			_, err := Run(c.src, Options{Limits: c.limits})
			if took := time.Since(start); took > 60*time.Second {
				t.Errorf("run took %s", took)
			}
			var ae *Error
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("err = %v, want success", err)
			case c.want != "" && (!errors.As(err, &ae) || ae.Msg != c.want):
				t.Fatalf("err = %v, want %q", err, c.want)
			case c.want != "" && !strings.Contains(ae.Pos, ":"):
				t.Fatalf("error %q has no position (%q)", ae.Msg, ae.Pos)
			}
			noLeak(t, before, c.src)
		})
	}
}
