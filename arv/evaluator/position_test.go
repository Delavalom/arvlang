package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/value"
)

// wantErrorAt fails unless got is an error whose message is msg and whose
// position is pos ("line:col").
func wantErrorAt(t *testing.T, got value.Object, msg, pos string) {
	t.Helper()
	e, ok := got.(*value.Error)
	if !ok {
		t.Fatalf("got %s, want error %q at %s", got.Inspect(), msg, pos)
	}
	if e.Message != msg || e.Pos != pos {
		t.Fatalf("got error %q at %q, want %q at %q", e.Message, e.Pos, msg, pos)
	}
}

// Errors carry the line:col of the innermost node they arose in.
func TestErrorPositions(t *testing.T) {
	tests := []struct{ name, input, msg, pos string }{
		{"identifier", "let x = 1;\nlet y = x + zz;", "identifier not found: zz", "2:13"},
		{"infix points at its left operand", "let a = 1;\n  a + \"s\"", "type mismatch: INTEGER + STRING", "2:3"},
		{"builtin call inside a function", "let f = fn() {\n  1;\n    len(1)\n};\nf()",
			"argument to `len` not supported, got INTEGER", "3:5"},
		{"division", "\n\n   10 / 0", "division by zero", "3:4"},
		{"call of a non-function", "let n = 5;\n n(1)", "not a function: INTEGER", "2:2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := runWith(t, tt.input)
			wantErrorAt(t, got, tt.msg, tt.pos)
		})
	}
}

// A task's error keeps its position through await and through the scopes
// it fails; a builtin spawned directly gets the spawn's position.
func TestTaskErrorPositions(t *testing.T) {
	got, _ := runWith(t, "let f = fn() {\n    len(1) };\nscope { spawn f() }")
	wantErrorAt(t, got, "argument to `len` not supported, got INTEGER", "2:5")

	got, _ = runWith(t, "let f = fn() {\n    len(1) };\nlet t = spawn f();\nawait t")
	wantErrorAt(t, got, "argument to `len` not supported, got INTEGER", "2:5")

	got, _ = runWith(t, "1;\nlet t = spawn len(1);\nawait t")
	wantErrorAt(t, got, "argument to `len` not supported, got INTEGER", "2:9")
}
