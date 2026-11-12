package evaluator

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// A steps error that lands on the tick of the block or statement the
// parser wraps around the right side of && or || is reported where the
// right side starts (3:4), never at the operator (2:21 or 2:22): the operator's
// line holds only the constant side, which never runs here. Leading 1;
// statements shift where the steps limit falls, so some runs land on
// exactly those ticks (with 0 to 60 of them: 7, 20, 34, 47).
func TestAndOrStepsErrorAtTheRightSide(t *testing.T) {
	for _, tt := range []struct{ op, at string }{{"true &&", "2:21"}, {"false ||", "2:22"}} {
		op := tt.op
		body := "let f = fn() { " + op + "\n   g() };\n" +
			"let g = fn() { 7 };\n" +
			"let h = fn(n) { if (n == 0) { 0 } else { f(); h(n - 1) } };\n" +
			"h(100)"
		atRight := 0
		for n := 0; n <= 60; n++ {
			input := strings.Repeat("1;", n) + "\n" + body
			got := runLimited(t, input, runtime.Limits{Steps: 200})
			e, ok := got.(*value.Error)
			if !ok || e.Message != "budget exceeded: steps (limit 200)" {
				t.Fatalf("%q with %d leading: got %s, want a steps error", op, n, got.Inspect())
			}
			if e.Pos == tt.at {
				t.Errorf("%q with %d leading: steps error at the operator %s, want the right side 3:4", op, n, tt.at)
			}
			if e.Pos == "3:4" {
				atRight++
			}
		}
		if atRight == 0 {
			t.Errorf("%q: no run landed on the right side (3:4); the test no longer exercises it", op)
		}
	}
}
