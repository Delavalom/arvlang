package evaluator

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// fifty is the Arv array [0, 1, ..., 49].
func fifty() string {
	xs := make([]string, 50)
	for i := range xs {
		xs[i] = "0"
	}
	return "[" + strings.Join(xs, ", ") + "]"
}

// runUnder runs input with l as the root budget and returns the result and
// the root's level, so a test can ask, after the run, whether its pools
// were spent.
func runUnder(t *testing.T, l runtime.Limits, input string) (value.Object, *runtime.Budget) {
	t.Helper()
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), &bytes.Buffer{})
	in := New(root)
	s, b := newScope(root.Ctx, l), runtime.Root(l)
	got := in.withScope(frame{s, b}, func() value.Object { return in.Eval(parse(t, input), value.NewEnvironment()) })
	in.releaseDepth(b, 0)
	return got, b
}

// A spent steps pool is not a different pool from the one match charges
// the err hash to, so before the fix match caught the steps error, the arm
// and the loop around it ran up to stepChunk more nodes, and every later
// iteration did the same: 2500 iterations of about 256 nodes each, with a
// limit of 1000. Inside the scope that ran out, match must pass the error
// through, as a cancelled task does; the root's 200000 steps (which the
// buggy loop spends) must stay unspent.
func TestMatchDoesNotSwallowASpentStepsBudget(t *testing.T) {
	src := `let spin = fn(n) { if (n == 0) { 0 } else { spin(n - 1) } };
		let a = ` + fifty() + `;
		match scope (steps: 1000) {
			for i in a { for j in a { match spin(1000000) { ok(v) => 1, err(e) => 0 } } }
		} { ok(v) => "finished", err(e) => e["message"] }`
	got, b := runUnder(t, runtime.Limits{Steps: 200000}, src)
	if got.Inspect() != "budget exceeded: steps (limit 1000)" {
		t.Fatalf("got %s, want the inner scope's steps error", got.Inspect())
	}
	if err := b.StepsSpent(); err != nil {
		t.Fatalf("the root's steps are spent (%v): match kept the loop running past the inner limit", err)
	}
}

// The same inside a task: its body's level shares the scope's steps pool.
func TestMatchInATaskDoesNotSwallowASpentStepsBudget(t *testing.T) {
	src := `let spin = fn(n) { if (n == 0) { 0 } else { spin(n - 1) } };
		let a = ` + fifty() + `;
		let work = fn() { for i in a { for j in a { match spin(1000000) { ok(v) => 1, err(e) => 0 } } } };
		match scope (steps: 1000) { await spawn work() } { ok(v) => "finished", err(e) => e["kind"] }`
	got, b := runUnder(t, runtime.Limits{Steps: 200000}, src)
	if got.Inspect() != "budget" {
		t.Fatalf("got %s, want budget", got.Inspect())
	}
	if err := b.StepsSpent(); err != nil {
		t.Fatalf("the root's steps are spent (%v)", err)
	}
}

// Under a spent pool no error is caught, not only the budget one: a
// runtime error raised before the next charge must not buy the arm
// another stepChunk nodes. Match returns the budget error in its place,
// so the kind says why the task stopped. Here the pool is spent before
// match runs (as a sibling task may spend it) and the task has nothing
// left to charge.
func TestMatchCatchesNoErrorOnceStepsAreSpent(t *testing.T) {
	for _, l := range []runtime.Limits{{Steps: 1000}, {AllocBytes: 1 << 20}} {
		root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), &bytes.Buffer{})
		in := New(root)
		s, b := newScope(root.Ctx, l), runtime.Root(l)
		b.Steps(5000)
		b.Alloc(5 << 20)
		// Not withScope: closing the scope would charge the rest and
		// replace "caught" with the steps error, hiding what match did.
		in.scopes = append(in.scopes, frame{s, b})
		got := in.Eval(parse(t, `match 1 / 0 { ok(v) => 1, err(e) => "caught" }`), value.NewEnvironment())
		in.releaseDepth(b, 0)
		s.Wait()
		if e, ok := got.(*value.Error); !ok || e.ErrKind() != runtime.KindBudget {
			t.Fatalf("limits %+v: got %s, want the budget error in place of the division error", l, got.Inspect())
		}
	}
}

// Outside the scope that ran out, its steps error is matched as usual, and
// so is any error under a pool that still has steps left.
func TestMatchStillCatchesOutsideTheSpentScope(t *testing.T) {
	got, _ := runUnder(t, runtime.Limits{Steps: 100000},
		`match scope (steps: 10) { for x in [1, 2, 3, 4, 5, 6] { x } } { ok(v) => 1, err(e) => e["kind"] }`)
	if got.Inspect() != "budget" {
		t.Fatalf("got %s, want budget", got.Inspect())
	}
	got, _ = runUnder(t, runtime.Limits{Steps: 100000}, `match 1 / 0 { ok(v) => 1, err(e) => e["kind"] }`)
	if got.Inspect() != "runtime" {
		t.Fatalf("got %s, want runtime", got.Inspect())
	}
}

// When settleSteps is the call that finds the pool spent, match must
// report the budget error it produced, not the subject's own error: the
// scope went past its steps limit, and the program must see kind budget
// (which restart's retry_on cannot mistake for a retryable kind), the
// same as with a subject that is not an error.
func TestMatchUnderASpentPoolReportsTheBudgetError(t *testing.T) {
	ones := strings.Repeat("1, ", 29) + "1"
	for _, subject := range []string{"1 / 0", "7"} {
		for _, limit := range []string{"20", "60", "120"} {
			src := `match scope (steps: ` + limit + `) { for i in [` + ones + `] { i }; match ` + subject +
				` { ok(v) => 1, err(e) => 2 } } { ok(v) => v, err(e) => e["kind"] }`
			got, _ := runUnder(t, runtime.Limits{Steps: 100000}, src)
			if got.Inspect() != "budget" {
				t.Errorf("subject %s, steps %s: got %s, want budget", subject, limit, got.Inspect())
			}
		}
	}
}
