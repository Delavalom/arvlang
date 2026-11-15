package evaluator

import (
	"context"
	"fmt"
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
// task that ignores cancellation.
func TestTimeoutWaitsForTasks(t *testing.T) {
	timeoutWaitsForStubborn(t, time.Second, 4*time.Second, 16*time.Second)
}

// A task that starts after its scope's deadline never runs stubborn, so
// that attempt shows nothing about I4; timeoutWaitsForStubborn tries again
// with a longer timeout. Here the first task starts late on purpose, as it
// might on a starved machine.
func TestTimeoutWaitsForLateStartingTask(t *testing.T) {
	old := startTask
	defer func() { startTask = old }()
	var first atomic.Bool
	startTask = func(t *runtime.Task, b *runtime.Budget, fn value.Object, args []value.Object) value.Object {
		if first.CompareAndSwap(false, true) {
			<-t.Ctx.Done() // start only once the scope has timed out
		}
		return runTask(t, b, fn, args)
	}
	timeoutWaitsForStubborn(t, 100*time.Millisecond, time.Second)
	if !first.Load() {
		t.Fatal("no task was delayed")
	}
}

// timeoutWaitsForStubborn runs a scope whose body waits until its stubborn
// task is running and then outlives the scope's timeout. Each attempt uses
// the next timeout. A task calls no builtin once its deadline has passed,
// so if stubborn did not start in time, the attempt is repeated; once it
// starts, the scope must not return before it finishes.
func timeoutWaitsForStubborn(t *testing.T, timeouts ...time.Duration) {
	t.Helper()
	before := goruntime.NumGoroutine()
	for _, d := range timeouts {
		var began, done atomic.Bool
		binds := startedStubborn(d+500*time.Millisecond, &done)
		inner := binds["stubborn"]
		binds["stubborn"] = func(t *runtime.Task, args ...value.Object) value.Object {
			began.Store(true)
			return inner(t, args...)
		}
		ms := d.Milliseconds()
		got, _ := runEnv(t, fmt.Sprintf(`scope (timeout_ms: %d) { spawn stubborn(); running(); spawn sleep(%d); 1 }`, ms, 5*ms), binds)
		wantErrorHas(t, got, fmt.Sprintf("budget exceeded: timeout (limit %d ms)", ms))
		if !began.Load() {
			t.Logf("stubborn did not start within %s; trying a longer timeout", d)
			continue
		}
		if !done.Load() {
			t.Fatal("the scope returned before its stubborn task finished")
		}
		settled(t, before)
		return
	}
	t.Fatalf("stubborn never started within %s", timeouts[len(timeouts)-1])
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
