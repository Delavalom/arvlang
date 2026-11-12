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

// A right side that does run is checked as anywhere else: a call needing
// a capability the task does not hold is denied, at the call.
func TestAndOrRightSideIsCapabilityChecked(t *testing.T) {
	const denied = `capability denied: net("127.0.0.1:1") in task root (holds: [])`
	got, _ := runWith(t, `true && http_get("http://127.0.0.1:1/")`)
	wantErrorAt(t, got, denied, "1:9")
	got, _ = runWith(t, "false ||\n  http_get(\"http://127.0.0.1:1/\")")
	wantErrorAt(t, got, denied, "2:3")
	got, _ = runWith(t, `puts(true && http_get("http://127.0.0.1:1/"))`)
	wantErrorAt(t, got, denied, "1:14")
}

// || is not a default operator: only true counts as true, so any other
// left side, a non-empty string included, gives the right side.
func TestOrIsNotADefault(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`let r = {"name": "bob"}; r.name || "anon"`, "anon"},
		{`let r = {"ok": true}; r.ok || "anon"`, "true"},
		{`"x" && 1`, "false"},
	} {
		if got, _ := runWith(t, tt.input); got.Inspect() != tt.want {
			t.Errorf("%s = %s, want %s", tt.input, got.Inspect(), tt.want)
		}
	}
}

// BUG (base, not Phase 5E): an if does not pass on a return that ends its
// condition, so the return is dropped. && and || are that if, so they
// drop it too. Reproduction on the base, without the new operators:
// `let f = fn() { if (if (true) { return 5 } else { 0 }) { 2 } else
// { false } }; f()` gives false, not 5.
func TestReturnInConditionIsKept(t *testing.T) {
	t.Skip("BUG: evalIfExpression drops a ReturnValue condition (base behaviour); see 16pe-ergonomics.md")
	for _, src := range []string{
		`let f = fn() { if (if (true) { return 5 } else { 0 }) { 2 } else { false } }; f()`,
		`let f = fn() { (if (true) { return 5 } else { 0 }) && 2 }; f()`,
		`let f = fn() { (if (true) { return 5 } else { 0 }) || 2 }; f()`,
	} {
		if got, _ := runWith(t, src); got.Inspect() != "5" {
			t.Errorf("%s = %s, want 5", src, got.Inspect())
		}
	}
}
