# Phase 4, task p4.5: `error(msg)` and string equality

> **For agentic workers:** this file is one task, p4.5. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p4.1 to p4.4 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `11-phase4-supervision.md`. Deviations from it, and why: `p4-plan-1.md`.

### Task p4.5: `error(msg)` and string equality

`error("message")` returns an error of kind `user`: the program's own failure, which `restart` retries only with `retry_on: ["effect", "user"]`.

Strings now compare with `==` and `!=`. The design's own `match` example tests `e["kind"] == "capability"`, which was `unknown operator: STRING == STRING` before. Comparing costs nothing against `alloc_mb`, as before (`TestStringOperatorOtherThanPlusCostsNothing` changes from "unknown" to "true/false, still free").

**Files:**
- Modify: `arv/evaluator/builtins.go`
- Modify: `arv/evaluator/eval_functions.go`
- Modify (test): `arv/evaluator/p3gaps_test.go`
- Create (test): `arv/evaluator/usererror_test.go`

- [ ] **Step 1: Write the failing tests**

In `arv/evaluator/p3gaps_test.go`, replace:

```go
// Only + is charged as a concatenation: another operator on two strings
// builds nothing and fails as unknown, even with no room left for what +
// would have built.
func TestStringOperatorOtherThanPlusCostsNothing(t *testing.T) {
	caps := runtime.NewCapSet()
	pre := fmt.Sprintf(`let s = %q; `, strings.Repeat("x", 1000))
	free := cheapest(t, caps, pre+`true`)
	for _, op := range []string{"==", "-"} {
		got := runCapsIn(t, free, caps, pre+"s "+op+" s")
		if e, ok := got.(*value.Error); !ok || !strings.HasPrefix(e.Message, "unknown operator: STRING "+op) {
			t.Errorf("s %s s under %d bytes: got %.200s, want unknown operator", op, free, got.Inspect())
		}
	}
```

with:

```go
// Only + is charged as a concatenation: another operator on two strings
// builds nothing, even with no room left for what + would have built. ==
// and != compare (Phase 4, for match); the rest fail as unknown.
func TestStringOperatorOtherThanPlusCostsNothing(t *testing.T) {
	caps := runtime.NewCapSet()
	pre := fmt.Sprintf(`let s = %q; `, strings.Repeat("x", 1000))
	free := cheapest(t, caps, pre+`true`)
	got := runCapsIn(t, free, caps, pre+"s - s")
	if e, ok := got.(*value.Error); !ok || !strings.HasPrefix(e.Message, "unknown operator: STRING -") {
		t.Errorf("s - s under %d bytes: got %.200s, want unknown operator", free, got.Inspect())
	}
	for op, want := range map[string]string{"==": "true", "!=": "false"} {
		if got := runCapsIn(t, free, caps, pre+"s "+op+" s"); got.Inspect() != want {
			t.Errorf("s %s s under %d bytes: got %.200s, want %s", op, free, got.Inspect(), want)
		}
	}
```

Create `arv/evaluator/usererror_test.go`:

```go
package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// error(msg) fails with msg, as a user error, positioned where it is called.
func TestUserError(t *testing.T) {
	got, _ := runWith(t, "let f = fn() {\n  error(\"boom\") };\nf()")
	e := wantKind(t, got, runtime.KindUser)
	if e.Message != "boom" || e.Pos != "2:3" || e.Task != "root" {
		t.Fatalf("got %+v", *e)
	}
	// It stops the block like any error: nothing after it runs.
	_, out := runWith(t, `error("stop"); puts("after")`)
	if out != "" {
		t.Fatalf("printed %q after error()", out)
	}
	// From a task, through await, it is still a user error.
	got, _ = runWith(t, `let f = fn() { error("in task") }; let t = spawn f(); await t`)
	if e := wantKind(t, got, runtime.KindUser); e.Message != "in task" || e.Task != "root/0" {
		t.Fatalf("got %+v", *e)
	}
}

func TestUserErrorBadArguments(t *testing.T) {
	for src, msg := range map[string]string{
		`error()`:         "error: want 1 argument, got 0",
		`error("a", "b")`: "error: want 1 argument, got 2",
		`error(1)`:        "error: message must be STRING, got INTEGER",
	} {
		got, _ := runWith(t, src)
		if e := wantKind(t, got, runtime.KindRuntime); e.Message != msg {
			t.Errorf("%s: got %q, want %q", src, e.Message, msg)
		}
	}
}

// Strings compare with == and !=, so a program can test an error's kind.
func TestStringEquality(t *testing.T) {
	for src, want := range map[string]string{
		`"effect" == "effect"`: "true",
		`"effect" == "user"`:   "false",
		`"effect" != "user"`:   "true",
		`"a" != "a"`:           "false",
		`"a" + "b" == "ab"`:    "true",
	} {
		if got, _ := runWith(t, src); got.Inspect() != want {
			t.Errorf("%s = %s, want %s", src, got.Inspect(), want)
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/evaluator/ -run 'TestUserError|TestStringEquality|TestStringOperatorOtherThanPlusCostsNothing'`

Expected: FAIL. The tests fail: (the first lines; ports and times vary)

```text
--- FAIL: TestStringOperatorOtherThanPlusCostsNothing (0.00s)
    p3gaps_test.go:132: s == s under 48 bytes: got ERROR: unknown operator: STRING == STRING, want true
--- FAIL: TestUserError (0.00s)
    usererror_test.go:12: error "identifier not found: error" has kind "runtime", want "user"
--- FAIL: TestUserErrorBadArguments (0.00s)
    usererror_test.go:36: error(): got "identifier not found: error", want "error: want 1 argument, got 0"
--- FAIL: TestStringEquality (0.00s)
    usererror_test.go:51: "effect" == "user" = ERROR: unknown operator: STRING == STRING, want false
```

- [ ] **Step 3: Implement**

In `arv/evaluator/builtins.go`, replace:

```go
		},
	},
	"puts": {
		// Under an interpreter, applyFunction calls Interpreter.puts
```

with:

```go
		},
	},
	"error": {
		// error(msg) is the program's own failure, of kind user. Like any
		// error it stops the block it is in, unless match catches it. The
		// message is msg itself, not a copy: it is already charged.
		Fn: func(t *runtime.Task, args ...value.Object) value.Object {
			if len(args) != 1 {
				return newError("error: want 1 argument, got %d", len(args))
			}
			msg, ok := args[0].(*value.String)
			if !ok {
				return newError("error: message must be STRING, got %s", args[0].Type())
			}
			return &value.Error{Message: msg.Value, Kind: runtime.KindUser}
		},
	},
	"puts": {
		// Under an interpreter, applyFunction calls Interpreter.puts
```

In `arv/evaluator/eval_functions.go`, replace:

```go
	left, right value.Object,
) value.Object {
	if operator != "+" {
		return newError("unknown operator: %s %s %s", left.Type(), operator, right.Type())
	}
	leftVal := left.(*value.String).Value
	rightVal := right.(*value.String).Value
	return &value.String{Value: leftVal + rightVal}
}
```

with:

```go
	left, right value.Object,
) value.Object {
	leftVal := left.(*value.String).Value
	rightVal := right.(*value.String).Value
	switch operator {
	case "+":
		return &value.String{Value: leftVal + rightVal}
	case "==": // so match can test e["kind"] == "effect"
		return nativeBoolToBooleanObject(leftVal == rightVal)
	case "!=":
		return nativeBoolToBooleanObject(leftVal != rightVal)
	default:
		return newError("unknown operator: %s %s %s", left.Type(), operator, right.Type())
	}
}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/evaluator/ -run 'TestUserError|TestStringEquality|TestStringOperatorOtherThanPlusCostsNothing'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/evaluator/builtins.go arv/evaluator/eval_functions.go arv/evaluator/p3gaps_test.go arv/evaluator/usererror_test.go && git commit -F - <<'MSG'
feat(evaluator): error(msg) builtin and string equality

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
