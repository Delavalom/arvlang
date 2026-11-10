package evaluator

import (
	"context"
	"io"
	goruntime "runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// timed runs input and fails if it takes longer than limit. Bounds are far
// above the timeouts under test (a 5 s sleep must not run out), so they
// hold on a heavily loaded machine.
func timed(t *testing.T, limit time.Duration, input string) value.Object {
	t.Helper()
	start := time.Now()
	got, _ := runWith(t, input)
	if d := time.Since(start); d > limit {
		t.Fatalf("took %s, want under %s:\n%s", d, limit, input)
	}
	return got
}

func TestScopeTimeout(t *testing.T) {
	before := goruntime.NumGoroutine()
	defer settled(t, before)
	got := timed(t, 3*time.Second, "scope (timeout_ms: 100) {\n  sleep(5000) }")
	wantErrorAt(t, got, "budget exceeded: timeout (limit 100 ms)", "2:3")
}

// I6: the earlier deadline wins, whichever scope sets it.
func TestNestedTimeoutsShrink(t *testing.T) {
	before := goruntime.NumGoroutine()
	defer settled(t, before)
	got := timed(t, 3*time.Second, `scope (timeout_ms: 100) { scope (timeout_ms: 10000) { sleep(5000) } }`)
	wantErrorHas(t, got, "budget exceeded: timeout (limit 100 ms)")

	got = timed(t, 3*time.Second, `scope (timeout_ms: 10000) { scope (timeout_ms: 100) { sleep(5000) } }`)
	wantErrorHas(t, got, "budget exceeded: timeout (limit 100 ms)")
}

// I4: a timeout cancels the scope's tasks and still waits for them, even a
// task that ignores cancellation. The body waits until stubborn is running:
// a task calls no builtin once its deadline has passed, so a stubborn task
// started late would never run. The timeout leaves time for it to start.
func TestTimeoutWaitsForTasks(t *testing.T) {
	var done atomic.Bool
	before := goruntime.NumGoroutine()
	got, _ := runEnv(t, `scope (timeout_ms: 1000) { spawn stubborn(); running(); spawn sleep(5000); 1 }`,
		startedStubborn(1500*time.Millisecond, &done))
	wantErrorHas(t, got, "budget exceeded: timeout (limit 1000 ms)")
	if !done.Load() {
		t.Fatal("the scope returned before its stubborn task finished")
	}
	settled(t, before)
}

func TestFastScopeBeatsItsTimeout(t *testing.T) {
	got := timed(t, 3*time.Second, `let f = fn() { 21 }; scope (timeout_ms: 5000) { let t = spawn f(); await t * 2 }`)
	if got.Inspect() != "42" {
		t.Fatalf("got %s", got.Inspect())
	}
}

// I5: a failure before the deadline is the scope's error, not the timeout.
func TestFailureBeforeTimeoutWins(t *testing.T) {
	got := timed(t, 3*time.Second, `scope (timeout_ms: 2000) { spawn len(1); sleep(5000) }`)
	wantErrorHas(t, got, "argument to `len` not supported")
}

// The run's own timeout (Limits.Timeout at the root) ends the program the
// same way.
func TestRunLimitedTimeout(t *testing.T) {
	p := parser.New(lexer.New("let f = fn() { sleep(5000) };\nspawn f();\nsleep(5000)"))
	program := p.ParseProgram()
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	start := time.Now()
	got := New(root).RunLimited(program, value.NewEnvironment(), runtime.Limits{Timeout: 100 * time.Millisecond})
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %s", d)
	}
	wantErrorAt(t, got, "budget exceeded: timeout (limit 100 ms)", "3:1")
}
