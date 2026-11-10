package evaluator

import (
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/value"
)

// testMaxStack caps every goroutine's stack while a test runs, so a
// recursion the evaluator fails to bound dies fast instead of growing a
// stack towards Go's 1 GiB default. It must stay above what evaluation
// legitimately uses at runtime.MaxEvalDepth: 64 MiB measured, 128 MiB
// under -race (see runtime.MaxEvalDepth). Concurrent deep tasks in these
// tests are kept to two, so their stacks fit in memory too.
const testMaxStack = 256 << 20

func withMaxStack(t *testing.T, n int) {
	t.Helper()
	old := debug.SetMaxStack(n)
	t.Cleanup(func() { debug.SetMaxStack(old) })
}

// recurseWith is f(n) recursing n times, each call level wrapped in k
// copies of open/close: Arv call depth and expression nesting multiply.
func recurseWith(open, close string, k, n int) string {
	return "let f = fn(n) { if (n == 0) { 0 } else { " + strings.Repeat(open, k) + "f(n - 1)" +
		strings.Repeat(close, k) + " } }; f(" + itoa(n) + ")"
}

// Call depth times nesting depth used to overflow the Go stack (a fatal
// error, not even a panic), at the root and in a spawned task alike. The
// evaluator bounds its total recursion and fails with an Arv error.
func TestCallsTimesNestingIsAnArvError(t *testing.T) {
	withMaxStack(t, testMaxStack)
	for name, src := range map[string]string{
		"prefix ops":    recurseWith("-", "", 200, 9999),
		"right infix":   recurseWith("1 + (", ")", 200, 9999),
		"arrays":        recurseWith("[", "]", 200, 9999),
		"scope blocks":  recurseWith("scope { ", " }", 50, 9999),
		"if blocks":     recurseWith("if (true) { ", " }", 50, 9999),
		"hashes":        recurseWith(`{"k": `, "}", 100, 9999),
		"call args":     "let g = fn(x) { x }; " + recurseWith("g(", ")", 100, 9999),
		"plain calls":   recurseWith("", "", 0, 50_000),
		"spawned":       "let t = fn() { " + recurseWith("-", "", 200, 9999) + " }; await spawn t()",
		"nested spawns": "let t = fn() { " + recurseWith("[", "]", 300, 9999) + " }; [await spawn t(), await spawn t()]",
	} {
		start := time.Now()
		got, _ := runWith(t, src)
		if e, ok := got.(*value.Error); !ok || !strings.Contains(e.Message, "stack overflow") {
			t.Errorf("%s = %.100s, want a stack overflow error", name, got.Inspect())
		}
		if d := time.Since(start); d > 20*time.Second {
			t.Errorf("%s took %s", name, d)
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
