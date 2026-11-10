package evaluator

import (
	"sync"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// gate returns wait() and open() builtins: wait blocks until open has been
// called (or the task is cancelled), so a script can order its tasks
// without sleeps.
func gate() map[string]value.BuiltinFunction {
	ch := make(chan struct{})
	var once sync.Once
	return map[string]value.BuiltinFunction{
		"open": func(*runtime.Task, ...value.Object) value.Object {
			once.Do(func() { close(ch) })
			return NIL
		},
		"wait": func(t *runtime.Task, _ ...value.Object) value.Object {
			select {
			case <-ch:
				return NIL
			case <-t.Ctx.Done():
				return newError("wait cancelled")
			}
		},
	}
}

// runBounded is runEnv that fails the test, instead of hanging it, if the
// program has not finished within limit.
func runBounded(t *testing.T, limit time.Duration, input string, binds map[string]value.BuiltinFunction) value.Object {
	t.Helper()
	done := make(chan value.Object, 1)
	go func() {
		got, _ := runEnv(t, input, binds)
		done <- got
	}()
	select {
	case got := <-done:
		return got
	case <-time.After(limit):
		t.Fatalf("program still running after %s (deadlock):\n%s", limit, input)
		return nil
	}
}

// Only the task that spawned a handle, or an ancestor of that task, may
// await it. Anything else (itself, a sibling, an ancestor) can form a
// cycle of tasks waiting on each other that no scope would ever end.
func TestAwaitOnlyBySpawner(t *testing.T) {
	for _, tt := range []struct{ name, src, want string }{
		{"self through a captured binding",
			`let f = fn() { wait(); await h }; let h = spawn f(); open()`,
			"await: task root/0 cannot await itself"},
		{"siblings awaiting each other",
			`let fa = fn() { wait(); await b }; let fb = fn() { wait(); await a };
			 let a = spawn fa(); let b = spawn fb(); open()`,
			"or its descendants"},
		{"grandchild awaiting its parent",
			`let g = fn() { wait(); await p }; let parent = fn() { spawn g(); 1 };
			 let p = spawn parent(); open()`,
			"await: task root/0 was not spawned by task root/0/0 or its descendants"},
		{"handle passed down to a child",
			`let f = fn(h) { await h }; let s = fn() { wait(); 1 };
			 let h = spawn s(); let c = spawn f(h); open()`,
			"await: task root/0 was not spawned by task root/1 or its descendants"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			wantErrorHas(t, runBounded(t, 5*time.Second, tt.src, gate()), tt.want)
		})
	}
}

// A task may also await a handle spawned by one of its descendants: the
// wait still points down the tree, so it cannot close a cycle.
func TestAwaitDescendantHandles(t *testing.T) {
	for _, tt := range []struct{ src, want string }{
		{`let f = fn(x) { x * 2 }; let mk = fn() { spawn f(4) }; await (await spawn mk())`, "8"},
		{`let f = fn(x) { x * 2 }; let mk = fn() { spawn f(4) }; let mk2 = fn() { spawn mk() };
		  await (await (await spawn mk2()))`, "8"},
		{`let f = fn(x) { x }; let p = fn() { let c = spawn f(7); fn() { await c } };
		  let k = await spawn p(); k()`, "7"},
		{`let f = fn(x) { x }; let mk = fn() { spawn f(3) };
		  let mid = fn() { await (await spawn mk()) }; await spawn mid()`, "3"},
	} {
		got := runBounded(t, 5*time.Second, tt.src, nil)
		if got.Inspect() != tt.want {
			t.Errorf("%s = %s, want %s", tt.src, got.Inspect(), tt.want)
		}
	}
}

// The spawner may await a handle any number of times, also after the scope
// it was spawned into has ended.
func TestSpawnerAwaitsRepeatedly(t *testing.T) {
	got := runBounded(t, 5*time.Second, `
		let f = fn(n) { n * 2 };
		let h = spawn f(1);
		let late = scope { spawn f(5) };
		[await h, await h, await late, await late]`, nil)
	if got.Inspect() != "[2, 2, 10, 10]" {
		t.Fatalf("got %s", got.Inspect())
	}
}
