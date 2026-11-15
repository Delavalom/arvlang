# Phase 4, task p4.7: Evaluate `match`

> **For agentic workers:** this file is one task, p4.7. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p4.1 to p4.6 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `11-phase4-supervision.md`. Deviations from it, and why: `p4-plan-1.md`.

### Task p4.7: Evaluate `match`

The subject is evaluated without propagating an error: the one place an error value does not abort the enclosing block.
- An error runs the `err` arm with `e` bound to `{"kind", "message", "task", "pos"}`. Any other value runs the `ok` arm. Each arm runs in a new environment enclosing the current one, so the name does not leak.
- The hash and the arm's environment are charged to `alloc_mb` before they are built. A budget error therefore cannot be swallowed inside the budget that ran out.
- A `return` in the subject passes through.
- **A cancelled task does not catch.** If the task running `match` has been cancelled, the error passes through: `match` must not let a cancelled task go on (I5). The consequence, under `fail_fast`, is pinned in p4.13.

**Files:**
- Modify: `arv/evaluator/evaluator.go`
- Create: `arv/evaluator/match.go`
- Create (test): `arv/evaluator/match_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/match_test.go`:

```go
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
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/evaluator/ -run 'TestMatch'`

Expected: FAIL. The tests fail: (the first lines; ports and times vary)

```text
--- FAIL: TestMatch (0.00s)
        match_test.go:33: got nil, want 6
--- FAIL: TestMatchArmErrorsPropagate (0.00s)
    match_test.go:42: got NIL nil, want a user error
--- FAIL: TestMatchAwaitedFailure (0.00s)
    match_test.go:57: got nil
--- FAIL: TestMatchDoesNotCatchInACancelledTask (0.00s)
    match_test.go:71: got nil, want the uncaught division error
```

- [ ] **Step 3: Implement**

In `arv/evaluator/evaluator.go`, replace:

```go
	case *ast.ForExpression:
		return in.evalFor(node, env)
	}
	// let statements and any node without a value evaluate to nil. Eval
```

with:

```go
	case *ast.ForExpression:
		return in.evalFor(node, env)
	case *ast.MatchExpression:
		return in.evalMatch(node, env)
	}
	// let statements and any node without a value evaluate to nil. Eval
```

Create `arv/evaluator/match.go`:

```go
package evaluator

import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/value"
)

// evalMatch evaluates match <subject> { ok(v) => ..., err(e) => ... }.
// The subject is the one place an error value does not stop evaluation:
// an error runs the err arm, with e bound to a hash of its "kind",
// "message", "task" and "pos"; any other value runs the ok arm, with v
// bound to it. Each arm runs in a new environment enclosing env, so the
// name does not leak out. A return in the subject passes through.
//
// An error is not caught once the matching task itself has been cancelled
// (its scope failed or timed out): match must not let a cancelled task go
// on. So in a fail_fast scope, match cannot rescue a task's failure; the
// scope fails with it anyway (I5). Use supervise: "collect" for that.
func (in *Interpreter) evalMatch(node *ast.MatchExpression, env *value.Environment) value.Object {
	subject := in.Eval(node.Subject, env)
	if subject.Type() == value.RETURN_VALUE_VAL {
		return subject
	}
	name, body := node.OkName, node.Ok
	if e, ok := subject.(*value.Error); ok {
		if in.Task.Err() != nil {
			return e
		}
		name, body = node.ErrName, node.Err
		h, errv := in.errorHash(e)
		if errv != nil {
			return errv
		}
		subject = h
	}
	if e := in.charge(envSize + bindingSize); e != nil {
		return e
	}
	armEnv := value.NewEnclosedEnvironment(env)
	armEnv.Set(name.Value, subject)
	return in.Eval(body, armEnv)
}

// errorHash is e as the hash the err arm sees, charged before it is
// built. Its strings share e's, so a user error's long message is not
// copied (it was charged when the program built it).
func (in *Interpreter) errorHash(e *value.Error) (*value.Hash, *value.Error) {
	fields := map[string]string{"kind": e.ErrKind(), "message": e.Message, "task": e.Task, "pos": e.Pos}
	n := int64(len(fields)) * hashPairSize
	for _, v := range fields {
		n += int64(len(v))
	}
	if errv := in.charge(n); errv != nil {
		return nil, errv
	}
	h := make(map[string]value.Object, len(fields))
	for k, v := range fields {
		h[k] = &value.String{Value: v}
	}
	return value.NewHash(h), nil
}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/evaluator/ -run 'TestMatch'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/evaluator/evaluator.go arv/evaluator/match.go arv/evaluator/match_test.go && git commit -F - <<'MSG'
feat(evaluator): match dispatches on errors

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
