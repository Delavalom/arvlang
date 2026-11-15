package evaluator

import (
	"context"
	"fmt"
	"io"
	goruntime "runtime"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// countdown recurses n times; each level costs 13 steps.
const countdown = `let f = fn(n) { if (n == 0) { 0 } else { f(n - 1) } };`

func TestStepsLimit(t *testing.T) {
	got, _ := runWith(t, countdown+"\nscope (steps: 1000) { f(5000) }")
	wantErrorHas(t, got, "budget exceeded: steps (limit 1000)")
	if e := got.(*value.Error); e.Message != "budget exceeded: steps (limit 1000)" || e.Pos == "" {
		t.Fatalf("got %q at %q, want the bare budget error with a position", e.Message, e.Pos)
	}
	got, _ = runWith(t, countdown+"scope (steps: 1000000) { f(5000) }") // f(5000) takes 65016
	if got.Inspect() != "0" {
		t.Fatalf("under a large enough limit: got %s", got.Inspect())
	}
}

// Steps count every task under the scope: four tasks of about 400 steps
// each pass 1000 together, but fit in 5000.
func TestStepsSharedByTasks(t *testing.T) {
	prog := countdown + `scope (steps: %d) { await for i in [1, 2, 3, 4] { spawn f(30) } }`
	got, _ := runWith(t, fmt.Sprintf(prog, 1000))
	wantErrorHas(t, got, "budget exceeded: steps (limit 1000)")
	got, _ = runWith(t, fmt.Sprintf(prog, 5000))
	if got.Inspect() != "[0, 0, 0, 0]" {
		t.Fatalf("under steps: 5000: got %s", got.Inspect())
	}
}

// I6: the smaller limit wins, whichever scope sets it.
func TestNestedStepsShrink(t *testing.T) {
	got, _ := runWith(t, countdown+`scope (steps: 1000) { scope (steps: 1000000) { f(5000) } }`)
	wantErrorHas(t, got, "budget exceeded: steps (limit 1000)")
	got, _ = runWith(t, countdown+`scope (steps: 100000) { scope (steps: 2000) { f(5000) } }`)
	wantErrorHas(t, got, "budget exceeded: steps (limit 2000)")
	// a task spawned inside counts against the scope it was spawned in
	got, _ = runWith(t, countdown+`scope (steps: 2000) { await spawn f(5000) }`)
	wantErrorHas(t, got, "budget exceeded: steps (limit 2000)")
}

// Closing a scope charges its last partial chunk, so one task's total is
// exact: the smallest limit a body fits in passes, one less fails.
func TestStepsExactAtScopeExit(t *testing.T) {
	body := `let a = [1, 2, 3]; a[0] + a[1] + a[2]`
	fits := 0
	for n := 1; n < 200 && fits == 0; n++ {
		if got, _ := runWith(t, fmt.Sprintf("scope (steps: %d) { %s }", n, body)); got.Inspect() == "6" {
			fits = n
		}
	}
	if fits < 5 {
		t.Fatalf("body fit in %d steps; it needs more than that", fits)
	}
	got, _ := runWith(t, fmt.Sprintf("scope (steps: %d) { %s }", fits-1, body))
	wantErrorHas(t, got, fmt.Sprintf("budget exceeded: steps (limit %d)", fits-1))
}

// Opening and closing scopes charges what was used, never a whole chunk,
// so many small scopes fit in a small limit.
func TestManySmallScopesFit(t *testing.T) {
	got, _ := runWith(t, `scope (steps: 600) { len(for i in [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20] { scope { i } }) }`)
	if got.Inspect() != "20" {
		t.Fatalf("got %s", got.Inspect())
	}
}

// The run's own steps limit (Limits.Steps at the root).
func TestRunLimitedSteps(t *testing.T) {
	p := parser.New(lexer.New(countdown + "f(5000)"))
	program := p.ParseProgram()
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	got := New(root).RunLimited(program, value.NewEnvironment(), runtime.Limits{Steps: 500})
	wantErrorHas(t, got, "budget exceeded: steps (limit 500)")
}

// I4: running out of steps fails the scope, which cancels and waits for
// the other tasks, so nothing outlives the run.
func TestStepsExhaustionCancelsTasks(t *testing.T) {
	before := goruntime.NumGoroutine()
	got, _ := runWith(t, countdown+`scope (steps: 3000) { spawn sleep(10000); spawn sleep(10000); f(5000) }`)
	wantErrorHas(t, got, "budget exceeded: steps (limit 3000)")
	settled(t, before)
}
