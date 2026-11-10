package evaluator

import (
	goruntime "runtime"
	"testing"
)

// dup builds an array holding the same sub-array twice at every level: n
// levels cost 32 charged bytes each, but print as about 5 * 2^n bytes.
const dup = "let dup = fn(x, n) { if (n == 0) { x } else { dup([x, x], n - 1) } };\n"

// puts renders a value under the scope's budgets: the text it builds is
// charged to alloc_mb as it grows, and it stops once the scope's deadline
// has passed. It used to build the whole text first, charging nothing and
// checking nothing, so printing dup(1, 25) under a 1 MB limit and a 50 ms
// timeout took 2 s and 759 MB.
func TestPutsStaysWithinBudgets(t *testing.T) {
	for _, tt := range []struct{ name, src, want string }{
		{"alloc", `scope (alloc_mb: 1) { puts(dup(1, 22)) }`, "budget exceeded: alloc (limit 1048576 bytes)"},
		{"timeout", `scope (timeout_ms: 50) { puts(dup(1, 24)) }`, "budget exceeded: timeout (limit 50 ms)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := goruntime.NumGoroutine()
			got, out := runEnv(t, dup+tt.src, nil)
			wantErrorHas(t, got, tt.want)
			if out != "" {
				t.Fatalf("puts printed %d bytes past the budget", len(out))
			}
			settled(t, before)
		})
	}
}

// Within budget, puts prints as before, nested and shared values included.
func TestPutsWithinBudgetPrints(t *testing.T) {
	got, out := runEnv(t, dup+`scope (alloc_mb: 1, timeout_ms: 60000) { puts(dup(1, 2), "s", [1, "a", {"k": [2]}]); 3 }`, nil)
	if got.Inspect() != "3" {
		t.Fatalf("got %s", got.Inspect())
	}
	if want := "[[1, 1], [1, 1]]\ns\n[1, a, {k: [2]}]\n"; out != want {
		t.Fatalf("output %q, want %q", out, want)
	}
	// dup(1, 14) prints as 5 * 2^14 - 4 bytes: past one check of the
	// text's size, with no limits to stop it.
	got, out = runEnv(t, dup+`puts(dup(1, 14)); 1`, nil)
	if want := 5*(1<<14) - 4 + 1; got.Inspect() != "1" || len(out) != want {
		t.Fatalf("got %s and %d bytes, want 1 and %d bytes", got.Inspect(), len(out), want)
	}
}
