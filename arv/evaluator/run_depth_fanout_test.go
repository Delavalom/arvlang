package evaluator

import (
	"fmt"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// The run's depth pool keeps runtime.LiveTaskDepth levels for every task
// the live-task cap allows, so raising the cap never turns shallow fan-out
// into a stack overflow. With a fixed pool of MaxRunEvalDepth, 2100 tasks
// each about 25 calls deep (three or four 64-level chunks) overflowed it
// although none nested deeper than a few hundred levels.
func TestRunDepthPoolScalesWithLiveTasks(t *testing.T) {
	const n = 2100
	before := goruntime.NumGoroutine()
	defer settled(t, before)
	src := fmt.Sprintf(deep+`len(await for i in %s { spawn deep(%d) })`, zeros(n), fanoutCalls)
	got := runInBounded(t, 2*time.Minute, runtime.Root(runtime.Limits{LiveTasks: n}), src, meet(n))
	if i, ok := got.(*value.Integer); !ok || i.Value != n {
		t.Fatalf("got %s, want %d", got.Inspect(), n)
	}
}

// fanoutCalls is how deep each fan-out task recurses: deep enough to hold
// three chunks of the pool, shallow enough to stay within LiveTaskDepth.
const fanoutCalls = 22

// runInBounded is runIn with a deadlock guard.
func runInBounded(t *testing.T, limit time.Duration, b *runtime.Budget, src string, binds map[string]value.BuiltinFunction) value.Object {
	t.Helper()
	done := make(chan value.Object, 1)
	go func() { done <- runIn(t, b, src, binds) }()
	select {
	case got := <-done:
		return got
	case <-time.After(limit):
		t.Fatalf("still running after %s", limit)
		return nil
	}
}
