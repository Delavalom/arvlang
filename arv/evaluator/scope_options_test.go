package evaluator

import "testing"

func TestValidScopeOptions(t *testing.T) {
	tests := []struct{ input, want string }{
		{`scope (timeout_ms: 1000, tasks: 2, steps: 100000, alloc_mb: 64) { 1 + 1 }`, `2`},
		{`scope () { 3 }`, `3`},
		// option values are expressions, evaluated in the enclosing scope
		{`let n = 3; scope (tasks: n, steps: n * 1000) { n }`, `3`},
		{`let f = fn(x) { x * 2 }; scope (tasks: 5) { await for i in [1, 2] { spawn f(i) } }`, `[2, 4]`},
	}
	for _, tt := range tests {
		got, _ := runWith(t, tt.input)
		if got.Inspect() != tt.want {
			t.Errorf("%s\n  = %s, want %s", tt.input, got.Inspect(), tt.want)
		}
	}
}

// Bad options fail before the body runs, positioned at the option's name.
func TestScopeOptionErrors(t *testing.T) {
	tests := []struct{ input, msg, pos string }{
		{"scope (\n  timout_ms: 100) { 1 }", `unknown scope option "timout_ms"`, "2:3"},
		{"scope (tasks: 2, tasks: 3) { 1 }", `duplicate scope option "tasks"`, "1:18"},
		{`scope (tasks: "2") { 1 }`, "scope option tasks: want a positive INTEGER, got STRING", "1:8"},
		{`scope (steps: 0) { 1 }`, "scope option steps: want a positive INTEGER, got 0", "1:8"},
		{`scope (alloc_mb: -1) { 1 }`, "scope option alloc_mb: want a positive INTEGER, got -1", "1:8"},
		{`scope (timeout_ms: 9223372036855) { 1 }`,
			"scope option timeout_ms: 9223372036855 is too large (max 9223372036854)", "1:8"},
		{`scope (alloc_mb: 8796093022208) { 1 }`,
			"scope option alloc_mb: 8796093022208 is too large (max 8796093022207)", "1:8"},
		{`scope (tasks: 2147483648) { 1 }`, "scope option tasks: 2147483648 is too large (max 2147483647)", "1:8"},
		// an error in the value is the scope's error
		{"scope (steps: nope) { 1 }", "identifier not found: nope", "1:15"},
	}
	for _, tt := range tests {
		got, out := runWith(t, tt.input+`; puts("after")`)
		wantErrorAt(t, got, tt.msg, tt.pos)
		if out != "" {
			t.Errorf("%s: printed %q; the program went on after a bad option", tt.input, out)
		}
	}
}

// The body never runs when an option is bad.
func TestBadOptionSkipsBody(t *testing.T) {
	_, out := runWith(t, `scope (bogus: 1) { puts("body ran") }`)
	if out != "" {
		t.Fatalf("output = %q, want the body skipped", out)
	}
}
