package evaluator

import (
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// inflight returns enter() and leave() builtins that count the tasks
// between them, and a func reporting the peak count.
func inflight() (map[string]value.BuiltinFunction, func() int) {
	var mu sync.Mutex
	n, peak := 0, 0
	binds := map[string]value.BuiltinFunction{
		"enter": func(*runtime.Task, ...value.Object) value.Object {
			mu.Lock()
			defer mu.Unlock()
			if n++; n > peak {
				peak = n
			}
			return NIL
		},
		"leave": func(*runtime.Task, ...value.Object) value.Object {
			mu.Lock()
			defer mu.Unlock()
			n--
			return NIL
		},
	}
	return binds, func() int { mu.Lock(); defer mu.Unlock(); return peak }
}

// zeros is an Arv array literal of n zeros, to loop over.
func zeros(n int) string { return "[" + strings.TrimSuffix(strings.Repeat("0, ", n), ", ") + "]" }

// 50 tasks of 100 ms under tasks: 5 run at most five at a time, so they
// take at least ten rounds. Both bounds hold however loaded the machine
// is: sleeps never end early, and the peak is counted, not timed.
func TestTasksBackpressure(t *testing.T) {
	before := goruntime.NumGoroutine()
	defer settled(t, before)
	binds, peak := inflight()
	start := time.Now()
	got := runBounded(t, 30*time.Second, `
		let work = fn() { enter(); sleep(100); leave() };
		scope (tasks: 5) { for i in `+zeros(50)+` { spawn work() } }; "done"`, binds)
	if got.Inspect() != "done" {
		t.Fatalf("got %s", got.Inspect())
	}
	if p := peak(); p > 5 {
		t.Fatalf("peak %d tasks in flight, limit 5", p)
	}
	if d := time.Since(start); d < time.Second {
		t.Fatalf("50 x 100 ms at 5 at a time took %s, want at least 1 s", d)
	}
}

// The limit is reached, not just respected: five tasks that each wait for
// all five to arrive can only finish if all five run at once.
func TestTasksRunUpToTheLimit(t *testing.T) {
	before := goruntime.NumGoroutine()
	defer settled(t, before)
	binds, peak := inflight()
	var mu sync.Mutex
	arrived, all := 0, make(chan struct{})
	binds["rendezvous"] = func(t *runtime.Task, _ ...value.Object) value.Object {
		mu.Lock()
		if arrived++; arrived == 5 {
			close(all)
		}
		mu.Unlock()
		select {
		case <-all:
			return NIL
		case <-t.Ctx.Done():
			return newError("rendezvous cancelled")
		}
	}
	got := runBounded(t, 30*time.Second, `
		let work = fn() { enter(); rendezvous(); leave() };
		scope (tasks: 5) { for i in `+zeros(6)+` { spawn work() } }; "done"`, binds)
	if got.Inspect() != "done" || peak() != 5 {
		t.Fatalf("got %s with peak %d, want done with peak 5", got.Inspect(), peak())
	}
}

// tasks counts direct children: two children holding both slots can still
// spawn and await grandchildren.
func TestTasksNoDeadlock(t *testing.T) {
	before := goruntime.NumGoroutine()
	defer settled(t, before)
	got := runBounded(t, 10*time.Second, `
		let leaf = fn(n) { n * 10 };
		let mid = fn(n) { await spawn leaf(n) };
		scope (tasks: 2) { await [spawn mid(1), spawn mid(2)] }`, nil)
	if got.Inspect() != "[10, 20]" {
		t.Fatalf("got %s", got.Inspect())
	}
}

// I6 within a task: a nested scope's tasks also take the enclosing
// scope's slots, so tasks: 10 inside tasks: 2 runs two at a time.
func TestNestedScopeTasksShrink(t *testing.T) {
	before := goruntime.NumGoroutine()
	defer settled(t, before)
	binds, peak := inflight()
	runBounded(t, 30*time.Second, `
		let work = fn() { enter(); sleep(20); leave() };
		scope (tasks: 2) { scope (tasks: 10) { for i in `+zeros(10)+` { spawn work() } } }`, binds)
	if p := peak(); p > 2 {
		t.Fatalf("peak %d tasks in flight, outer limit 2", p)
	}
}
