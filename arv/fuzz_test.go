package arv

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// runSeeds exercise every Phase 3 path that can end a run: each budget
// kind, nested options (I6), slots under cancellation, for ... in and
// await on arrays, bad options and runtime errors.
var runSeeds = []string{
	`scope (timeout_ms: 20) { sleep(5000) }`,
	`scope (timeout_ms: 20) { scope (timeout_ms: 10000) { sleep(5000) } }`,
	`let f = fn(n) { if (n == 0) { 0 } else { f(n - 1) } }; scope (steps: 1000) { f(5000) }`,
	`let w = fn() { let f = fn(n) { if (n == 0) { 0 } else { f(n - 1) } }; f(100) };
	 scope (steps: 1000) { await for i in [1, 2, 3, 4] { spawn w() } }`,
	`let w = fn() { sleep(1) }; scope (tasks: 2) { for i in [1, 2, 3, 4, 5, 6] { spawn w() } }`,
	`let g = fn() { 1 }; let c = fn() { await spawn g() };
	 scope (tasks: 2) { await [spawn c(), spawn c()] }`,
	`let grow = fn(n, acc) { if (n == 0) { acc } else { grow(n - 1, push(acc, "x")) } };
	 scope (alloc_mb: 1) { len(grow(5000, [])) }`,
	`let f = fn(n) { if (n == 0) { 0 } else { await spawn f(n - 1) } }; f(100)`,
	`let s = fn() { sleep(5000) }; scope (timeout_ms: 30, tasks: 1) { for i in [1, 2, 3] { spawn s() } }`,
	`await for x in [1, 2, 3] { spawn len("abc") }`,
	`for x in [1, 2] { x / 0 }`,
	`for x in 5 { x }`,
	`await [1, 2]`,
	`await []`,
	`scope (timout_ms: 1) { 1 }`,
	`scope (steps: 0) { 1 }`,
	`scope (steps: "x") { 1 }`,
	`scope (steps: 1, steps: 2) { 1 }`,
	`scope (timeout_ms: 9223372036854775807) { 1 }`,
	`scope (alloc_mb: 9223372036854775807) { 1 }`,
	`scope (tasks: 2147483648) { 1 }`,
	`scope (steps: 10) { puts([1, 2, 3]) }`,
	`let d = fn(n, a) { if (n == 0) { a } else { d(n - 1, [a, a]) } }; puts(d(30, 1))`,
	`getenv("HOME")`,
	`let r = {"up": true, "n": 3}; if (r.up && r.n >= 3 || false) { "n=${r.n} ${[r]}" }`,
	`let d = fn(n, a) { if (n == 0) { a } else { d(n - 1, [a, a]) } }; "${d(30, 1)}"`,
	`false && http_get("http://127.0.0.1:1/") || "${1 / 0}"`,
}

var position = regexp.MustCompile(`^\d+:\d+$`)

// FuzzRun runs any program under small budgets. The run must end in time,
// never panic into the host, report every Arv error with a line:col
// position, and leave no goroutine behind.
func FuzzRun(f *testing.F) {
	for _, s := range runSeeds {
		f.Add(s)
	}
	files, _ := filepath.Glob("../examples/*.arv")
	for _, name := range files {
		if b, err := os.ReadFile(name); err == nil {
			f.Add(string(b))
		}
	}
	limits := runtime.Limits{Timeout: 200 * time.Millisecond, Steps: 50000, AllocBytes: 4 << 20, LiveTasks: 20}
	f.Fuzz(func(t *testing.T, src string) {
		before := goruntime.NumGoroutine()
		start := time.Now()
		_, err := Run(src, Options{Limits: limits})
		if took := time.Since(start); took > 20*time.Second {
			t.Fatalf("run took %s under a 200 ms timeout: %q", took, src)
		}
		var ae *Error
		var pe *ParseError
		switch {
		case err == nil, errors.As(err, &pe):
		case errors.As(err, &ae):
			if !position.MatchString(ae.Pos) {
				t.Fatalf("error %q has position %q for %q", ae.Msg, ae.Pos, src)
			}
		default:
			t.Fatalf("run of %q returned %T %v, want an *Error or *ParseError", src, err, err)
		}
		noLeak(t, before, src)
	})
}

// noLeak fails unless the goroutine count comes back to before within 10 s.
func noLeak(t *testing.T, before int, src string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for goruntime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("%d goroutines outlived the run of %q (%d before):\n%s",
				goruntime.NumGoroutine(), src, before, buf[:goruntime.Stack(buf, true)])
		}
		time.Sleep(time.Millisecond)
	}
}
