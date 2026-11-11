package evaluator

import (
	"bytes"
	"context"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func TestMatch(t *testing.T) {
	t.Setenv("ARV_MATCH_X", "x")
	for _, tt := range []struct{ name, src, want string }{
		{"a value passes through", `match 5 { ok(v) => v + 1, err(e) => 0 }`, "6"},
		{"arms in either order", `match 5 { err(e) => 0, ok(v) => v * 2 }`, "10"},
		{"an error runs the err arm", `match 1 / 0 { ok(v) => v, err(e) => e["message"] }`, "division by zero"},
		{"the err arm sees kind, message, task and pos",
			"match error(\"boom\") { ok(v) => v, err(e) => [e[\"kind\"], e[\"message\"], e[\"task\"], e[\"pos\"]] }",
			"[user, boom, root, 1:7]"},
		{"dispatch on the kind",
			`match getenv("ARV_MATCH_X") { ok(v) => v, err(e) => if (e["kind"] == "capability") { "denied" } else { e["message"] } }`,
			"denied"},
		{"an inner scope's timeout", `match scope (timeout_ms: 10) { sleep(5000) } { ok(v) => v, err(e) => e["kind"] }`, "budget"},
		{"the name does not leak", `let v = 1; match 5 { ok(v) => v, err(e) => 0 }; v`, "1"},
		{"arms see the enclosing environment", `let k = 3; match 5 { ok(v) => v + k, err(e) => 0 }`, "8"},
		{"nested", `match match 1 / 0 { ok(v) => v, err(e) => error("again") } { ok(v) => v, err(e) => e["message"] }`, "again"},
		{"a return in the subject passes through",
			`let f = fn() { match if (true) { return 7 } { ok(v) => 1, err(e) => 2 }; 3 }; f()`, "7"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, _ := runWith(t, tt.src); got.Inspect() != tt.want {
				t.Fatalf("got %s, want %s", got.Inspect(), tt.want)
			}
		})
	}
}

// An error in an arm is not caught by the same match.
func TestMatchArmErrorsPropagate(t *testing.T) {
	got, _ := runWith(t, `match 1 { ok(v) => error("in arm"), err(e) => 0 }`)
	if e := wantKind(t, got, runtime.KindUser); e.Message != "in arm" {
		t.Fatalf("got %q", e.Message)
	}
	got, _ = runWith(t, `match 1 / 0 { ok(v) => v, err(e) => e["nope"] + 1 }`)
	wantKind(t, got, runtime.KindRuntime)
}

// match reads a failed task's error, with the task and position where it
// arose, without that failure stopping the program. (A session awaits a
// task whose scope has already ended, so the failure cancels nothing.)
func TestMatchAwaitedFailure(t *testing.T) {
	s := newSession(nil)
	failedHandles(t, s)
	got := s.run(t, `match await bad { ok(v) => v, err(e) => [e["kind"], e["message"], e["task"], e["pos"]] }`)
	if got.Inspect() != "[runtime, division by zero, root/1, 1:17]" {
		t.Fatalf("got %s", got.Inspect())
	}
	if got := s.run(t, `match await ok { ok(v) => v, err(e) => 0 }`); got.Inspect() != "10" {
		t.Fatalf("got %s", got.Inspect())
	}
}

// A cancelled task does not catch errors: match must not let it go on.
func TestMatchDoesNotCatchInACancelledTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := runtime.NewRoot(ctx, runtime.NewCapSet(), &bytes.Buffer{})
	got := New(root).Run(parse(t, `match 1 / 0 { ok(v) => v, err(e) => "caught" }`), value.NewEnvironment())
	if e, ok := got.(*value.Error); !ok || e.Message != "division by zero" {
		t.Fatalf("got %s, want the uncaught division error", got.Inspect())
	}
}

// The err arm's hash is charged: once the bytes budget is spent, matching
// the budget error itself fails with it, so it cannot be swallowed inside
// the budget that ran out.
func TestMatchChargesTheErrorHash(t *testing.T) {
	got, _ := runWith(t, `let d = fn(s, n) { if (n == 0) { s } else { d(s + s, n - 1) } };
		scope (alloc_mb: 1) { match d("aaaaaaaaaaaaaaaa", 20) { ok(v) => 1, err(e) => e["kind"] } }`)
	wantKind(t, got, runtime.KindBudget)
	// Outside that scope, the same error can be matched.
	got, _ = runWith(t, `let d = fn(s, n) { if (n == 0) { s } else { d(s + s, n - 1) } };
		match scope (alloc_mb: 1) { d("aaaaaaaaaaaaaaaa", 20) } { ok(v) => 1, err(e) => e["kind"] }`)
	if got.Inspect() != "budget" {
		t.Fatalf("got %s, want budget", got.Inspect())
	}
}
