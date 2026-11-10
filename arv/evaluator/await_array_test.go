package evaluator

import (
	"testing"
	"time"
)

func TestAwaitArray(t *testing.T) {
	tests := []struct{ input, want string }{
		{`let f = fn(n) { n * 10 }; await [spawn f(1), spawn f(2)]`, `[10, 20]`},
		{`await []`, `[]`},
		{`let f = fn(n) { n * 2 }; await for i in [1, 2, 3] { spawn f(i) }`, `[2, 4, 6]`},
		// results come back in array order, not finishing order
		{`let f = fn(ms) { sleep(ms); ms }; await [spawn f(60), spawn f(1), spawn f(30)]`, `[60, 1, 30]`},
		// a handle may be awaited again, alone or in an array
		{`let f = fn() { 7 }; let t = spawn f(); [await t, await [t, t]]`, `[7, [7, 7]]`},
	}
	for _, tt := range tests {
		got, _ := runWith(t, tt.input)
		if got.Inspect() != tt.want {
			t.Errorf("%s\n  = %s, want %s", tt.input, got.Inspect(), tt.want)
		}
	}
}

func TestAwaitArrayErrors(t *testing.T) {
	// the first error in array order is the result
	got, _ := runWith(t, "let f = fn(n) { 10 / n };\nawait [spawn f(1), spawn f(0)]")
	wantErrorAt(t, got, "division by zero", "1:17")

	// every element is checked before anything is awaited: a bad element
	// fails at once instead of after the 5 s sleep
	start := time.Now()
	got, _ = runWith(t, "let t = spawn sleep(5000);\n await [t, 5]")
	wantErrorAt(t, got, "await: want TASK, got INTEGER (element 1)", "2:2")
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %s: the bad element was found only after awaiting t", d)
	}

	// the ownership rule applies to each element
	got, _ = runWith(t, `
		let a = spawn sleep(10);
		let f = fn() { await [a] };
		await spawn f()`)
	wantErrorHas(t, got, "await: task root/0 was not spawned by task root/1 or its descendants")
}
