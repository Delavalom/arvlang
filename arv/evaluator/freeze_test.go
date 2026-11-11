package evaluator

import (
	goruntime "runtime"
	"testing"
	"time"
)

// I8: a spawned task never sees a name its parent binds after the spawn,
// however it reaches the environment: directly, through a closure defined
// earlier, through a closure passed as an argument, or from a task it
// spawns later. The gate (wait/open) makes the child read only after the
// parent has rebound.
func TestChildNeverSeesALaterRebinding(t *testing.T) {
	for _, tt := range []struct{ name, src, want string }{
		{"directly",
			`let x = 1; let f = fn() { wait(); x };
			 let t = spawn f(); let x = 2; open(); [await t, x]`, "[1, 2]"},
		{"through a closure defined before",
			`let x = 1; let g = fn() { x }; let f = fn() { wait(); g() };
			 let t = spawn f(); let x = 2; open(); await t`, "1"},
		{"through a closure in an argument",
			`let x = 1; let fs = [fn() { x }]; let f = fn(a) { wait(); a[0]() };
			 let t = spawn f(fs); let x = 2; open(); await t`, "1"},
		{"from a grandchild spawned after the rebinding",
			`let x = 1; let inner = fn() { x }; let f = fn() { wait(); await spawn inner() };
			 let t = spawn f(); let x = 2; open(); await t`, "1"},
		{"the child's own environment, frozen for the grandchild",
			`let f = fn() { let y = 1; let h = fn() { wait(); y }; let t = spawn h(); let y = 2; open(); [y, await t] };
			 await spawn f()`, "[2, 1]"},
		{"the child rebinds its own names",
			`let f = fn() { let y = 1; let y = y + 1; y }; await spawn f()`, "2"},
		{"recursion bound before the spawn",
			`let fib = fn(n) { if (n < 2) { n } else { fib(n - 1) + fib(n - 2) } }; await spawn fib(10)`, "55"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := runBounded(t, 10*time.Second, tt.src, gate()); got.Inspect() != tt.want {
				t.Fatalf("got %s, want %s", got.Inspect(), tt.want)
			}
		})
	}
}

// A name bound only after the spawn does not exist for the child.
func TestChildCannotSeeANameBoundAfterSpawn(t *testing.T) {
	got := runBounded(t, 10*time.Second,
		`let f = fn() { wait(); later }; let t = spawn f(); let later = 1; open(); await t`, gate())
	wantErrorHas(t, got, "identifier not found: later")
}

// Freezing keeps no goroutine and needs no lock held across tasks: a
// parent rebinding while many children read finishes and leaks nothing.
func TestFreezeUnderConcurrentRebinding(t *testing.T) {
	before := goruntime.NumGoroutine()
	got := runBounded(t, 30*time.Second, `
		let x = 0;
		let f = fn(n) { wait(); x + n };
		let ts = for n in [1, 2, 3, 4, 5, 6, 7, 8] { spawn f(n) };
		let x = 100; let x = 200;
		open();
		await ts`, gate())
	if got.Inspect() != "[1, 2, 3, 4, 5, 6, 7, 8]" {
		t.Fatalf("got %s", got.Inspect())
	}
	settled(t, before)
}
