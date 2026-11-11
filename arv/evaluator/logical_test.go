package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// a && b is if (a) { b } else { false }; a || b is if (a) { true } else
// { b }. As in if, only true counts as true.
func TestAndOr(t *testing.T) {
	tests := []struct{ input, want string }{
		{`true && true`, "true"}, {`true && false`, "false"},
		{`false && true`, "false"}, {`false && false`, "false"},
		{`true || false`, "true"}, {`false || true`, "true"},
		{`false || false`, "false"}, {`true || true`, "true"},
		{`let s = 250; s >= 200 && s < 300`, "true"},
		{`let s = 404; s >= 200 && s < 300`, "false"},
		{`1 < 2 || 1 > 2 && false`, "true"}, // && binds tighter than ||
		{`true && 5`, "5"},                  // the right side's value
		{`1 && true`, "false"},              // 1 is not true
		{`let n = if (false) { 1 }; n || "default"`, "default"},
		{`5 || 6`, "6"},
		{`let n = if (false) { 1 }; !false && !n`, "true"},
	}
	for _, tt := range tests {
		if got, _ := runWith(t, tt.input); got.Inspect() != tt.want {
			t.Errorf("%s = %s, want %s", tt.input, got.Inspect(), tt.want)
		}
	}
}

// The right side never runs when the left decides: no call, no output,
// no effect, and no steps or bytes for it.
func TestAndOrShortCircuit(t *testing.T) {
	calls := 0
	hit := map[string]value.BuiltinFunction{"hit": func(*runtime.Task, ...value.Object) value.Object {
		calls++
		return TRUE
	}}
	for _, src := range []string{`false && hit()`, `true || hit()`, `[] && hit()`, `false && (true || hit())`} {
		if got := runLimitedEnv(t, src, hit, runtime.Limits{}); isError(got) {
			t.Fatalf("%s: %s", src, got.Inspect())
		}
	}
	if calls != 0 {
		t.Fatalf("the right side ran %d times", calls)
	}
	if got := runLimitedEnv(t, `true && hit()`, hit, runtime.Limits{}); got != TRUE || calls != 1 {
		t.Fatalf("true && hit() = %s after %d calls, want true after 1", got.Inspect(), calls)
	}
	// An effect on the right is not attempted: no denial, no output.
	got, out := runWith(t, `false && http_get("http://example.invalid/")`)
	if got != FALSE || out != "" {
		t.Fatalf("got %s, printed %q", got.Inspect(), out)
	}
	got, out = runWith(t, `true || puts("ran")`)
	if got != TRUE || out != "" {
		t.Fatalf("got %s, printed %q", got.Inspect(), out)
	}
	// However big the skipped side, it costs nothing.
	big := `[1, 2, 3, 4, 5, 6, 7, 8, 9, 10] == [{"a": 1}, fn(x) { x }]`
	sameCost(t, `false && `+big, `false && 1`)
	sameCost(t, `true || `+big, `true || 1`)
}

// Each form costs what the if it means costs, in steps and bytes.
func TestAndOrCostWhatIfCosts(t *testing.T) {
	const pre = `let a = 250; `
	for _, c := range [][2]string{
		{`a > 1 && a < 300`, `if (a > 1) { a < 300 } else { false }`},
		{`a < 1 && a < 300`, `if (a < 1) { a < 300 } else { false }`},
		{`a > 1 || [a, a]`, `if (a > 1) { true } else { [a, a] }`},
		{`a < 1 || [a, a]`, `if (a < 1) { true } else { [a, a] }`},
	} {
		sameCost(t, pre+c[0], pre+c[1])
	}
}

// An error on either side is placed where that side's failing node is.
func TestAndOrErrorPositions(t *testing.T) {
	got, _ := runWith(t, "true &&\n  nope")
	wantErrorAt(t, got, "identifier not found: nope", "2:3")
	got, _ = runWith(t, "false ||   1 / 0")
	wantErrorAt(t, got, "division by zero", "1:12")
	got, _ = runWith(t, " nope || true")
	wantErrorAt(t, got, "identifier not found: nope", "1:2")
}
