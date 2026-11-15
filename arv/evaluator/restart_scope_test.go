package evaluator

import (
	"context"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// secondAttempt binds flaky(), which fails with an effect error the first
// time and succeeds after, and second(), which blocks until flaky has been
// called twice: the restarted attempt is running. second ends early, with
// an error, if its task is cancelled first.
func secondAttempt() (map[string]value.BuiltinFunction, *atomic.Int32) {
	var calls atomic.Int32
	started := make(chan struct{})
	var once sync.Once
	return map[string]value.BuiltinFunction{
		"flaky": func(*runtime.Task, ...value.Object) value.Object {
			if calls.Add(1) == 1 {
				return &value.Error{Message: "flaky", Kind: runtime.KindEffect}
			}
			once.Do(func() { close(started) })
			return NIL
		},
		"second": func(t *runtime.Task, _ ...value.Object) value.Object {
			select {
			case <-started:
				return NIL
			case <-t.Ctx.Done():
				return newError("second cancelled")
			}
		},
	}, &calls
}

// A restarted attempt is cancelled with its scope (I5): when a sibling
// fails with an error that is not restarted, the attempt in progress
// stops at once instead of finishing its 30 s sleep.
func TestRestartedAttemptStopsWithItsScope(t *testing.T) {
	before := goruntime.NumGoroutine()
	binds, calls := secondAttempt()
	start := time.Now()
	got, _ := runPolicy(t, restartOn(3, time.Millisecond, "effect"), runtime.Limits{},
		`let f = fn() { flaky(); sleep(30000) };
		let g = fn() { second(); getenv("ARV_RESTART_X") };
		spawn f(); spawn g(); 1`, binds)
	wantKind(t, got, runtime.KindCapability)
	if calls.Load() != 2 {
		t.Fatalf("flaky called %d times, want 2", calls.Load())
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Fatalf("took %s: the second attempt outlived its failed scope", elapsed)
	}
	settled(t, before)
}

// The host cancelling the run cancels a restarted attempt too.
func TestRestartedAttemptStopsWhenTheHostCancels(t *testing.T) {
	before := goruntime.NumGoroutine()
	binds, calls := secondAttempt()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	binds["cancel_run"] = func(*runtime.Task, ...value.Object) value.Object { cancel(); return NIL }
	program := parse(t, `let f = fn() { flaky(); sleep(30000) };
		let t = spawn f(); second(); cancel_run(); await t`)
	env := value.NewEnvironment()
	for name, fn := range binds {
		env.Set(name, &value.Builtin{Fn: fn})
	}
	root := runtime.NewRoot(ctx, runtime.NewCapSet(), nil)
	in := New(root)
	s, b := newScope(root, runtime.Limits{}, true), runtime.Root(runtime.Limits{})
	s.SetPolicy(restartOn(3, time.Millisecond, "effect"))
	start := time.Now()
	got := in.withScope(frame{s, b}, func() value.Object { return in.Eval(program, env) })
	in.releaseDepth(b, 0)
	wantKind(t, got, runtime.KindCancelled)
	if calls.Load() != 2 {
		t.Fatalf("flaky called %d times, want 2", calls.Load())
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Fatalf("took %s: the second attempt outlived the cancelled run", elapsed)
	}
	settled(t, before)
}

// A restarted attempt is bound by its scope's timeout (I6). The probe
// that found the gap: timeout 1000 ms, the second attempt sleeps 30 s.
func TestRestartedAttemptStopsAtTheTimeout(t *testing.T) {
	before := goruntime.NumGoroutine()
	binds, calls := secondAttempt()
	start := time.Now()
	got, _ := runPolicy(t, restartOn(3, time.Millisecond, "effect"), runtime.Limits{Timeout: 2 * time.Second},
		`let f = fn() { flaky(); sleep(30000) }; await spawn f()`, binds)
	elapsed := time.Since(start)
	if calls.Load() < 2 {
		t.Skipf("the timeout passed before the second attempt began (load); nothing to check")
	}
	if e := wantKind(t, got, runtime.KindBudget); !strings.Contains(e.Message, "timeout") {
		t.Fatalf("got %q, want the timeout", e.Message)
	}
	if elapsed > 20*time.Second {
		t.Fatalf("took %s: the second attempt outlived the 2 s timeout", elapsed)
	}
	settled(t, before)
}
