package evaluator

import (
	"context"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// spentBySibling returns builtins that hold open the window between a
// sibling spending the scope's shared budget and its error cancelling the
// scope. The sibling spends inside an inner scope that also runs hold:
// when the sibling's charge fails, the inner scope is cancelled (so hold
// learns of it), but the inner scope then waits for hold, so the sibling
// has not returned and the outer scope is not yet cancelled. gate lets the
// caller go only once the budget is spent, and hold stays until effect has
// run or the outer scope is cancelled after all.
func spentBySibling() (map[string]value.BuiltinFunction, *atomic.Int64) {
	var calls atomic.Int64
	running, spent, ran := make(chan struct{}), make(chan struct{}), make(chan struct{})
	outer := make(chan context.Context, 1)
	var once sync.Once
	return map[string]value.BuiltinFunction{
		"hold": func(t *runtime.Task, _ ...value.Object) value.Object {
			close(running)
			<-t.Ctx.Done() // the sibling's charge failed: its inner scope ended
			close(spent)
			select {
			case <-ran:
			case <-(<-outer).Done():
			}
			return NIL
		},
		"started": func(*runtime.Task, ...value.Object) value.Object {
			<-running // spend only once hold runs, or it could fail before starting
			return NIL
		},
		"gate": func(t *runtime.Task, _ ...value.Object) value.Object {
			outer <- t.Ctx
			<-spent
			return NIL
		},
		"effect": func(*runtime.Task, ...value.Object) value.Object {
			calls.Add(1)
			once.Do(func() { close(ran) })
			return NIL
		},
	}, &calls
}

// A task with no steps or bytes of its own left to charge must still not
// start an effect once a sibling has spent the budget they share, even
// while the sibling's error has not cancelled the scope yet. gate returns
// with nothing left to charge, so only the check of the shared budget
// itself (StepsSpent) stops effect, both when called directly
// and when spawned.
func TestNoEffectAfterSiblingSpent(t *testing.T) {
	spend := map[string]struct{ prelude, limit, work, want string }{
		"steps": {"", "steps: 300", "for i in " + zeros(400) + " { i }", "budget exceeded: steps (limit 300)"},
	}
	for kind, s := range spend {
		for _, body := range []string{"effect(gate())", "spawn effect(gate())"} {
			t.Run(kind+"/"+body, func(t *testing.T) {
				before := goruntime.NumGoroutine()
				binds, calls := spentBySibling()
				src := s.prelude + "scope (" + s.limit + ") { spawn fn() { scope { spawn hold(); started(); " +
					s.work + " } }(); " + body + " }"
				got := runBoundedLimited(t, 2*time.Minute, src, binds)
				wantErrorHas(t, got, s.want)
				if n := calls.Load(); n != 0 {
					t.Fatalf("effect ran %d times after a sibling spent the budget", n)
				}
				settled(t, before)
			})
		}
	}
}

// runBoundedLimited is runLimitedEnv that fails, instead of hanging, if
// the program deadlocks.
func runBoundedLimited(t *testing.T, limit time.Duration, input string, binds map[string]value.BuiltinFunction) value.Object {
	t.Helper()
	done := make(chan value.Object, 1)
	go func() { done <- runLimitedEnv(t, input, binds, runtime.Limits{}) }()
	select {
	case got := <-done:
		return got
	case <-time.After(limit):
		t.Fatalf("program still running after %s (deadlock):\n%s", limit, input)
		return nil
	}
}
