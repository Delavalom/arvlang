package evaluator

import (
	goruntime "runtime"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// A task gives back its live-task place before its awaiter wakes, so
// spawning and awaiting one task at a time never has two alive, even
// under a cap of one. When the place was given back after the handle was
// finished, the next spawn could see the finished task still counted and
// fail: about half of 2000-iteration runs did.
func TestLiveCapSequentialSpawnAwait(t *testing.T) {
	before := goruntime.NumGoroutine()
	defer settled(t, before)
	src := `let f = fn() { 1 }; let r = for i in ` + zeros(2000) + ` { await spawn f() }; len(r)`
	for run := 0; run < 5; run++ {
		got := runIn(t, runtime.Root(runtime.Limits{LiveTasks: 1}), src, nil)
		if i, ok := got.(*value.Integer); !ok || i.Value != 2000 {
			t.Fatalf("run %d: got %s, want 2000", run, got.Inspect())
		}
	}
}
