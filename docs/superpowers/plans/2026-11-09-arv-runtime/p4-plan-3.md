# Phase 4, task p4.3: Errors name their task; the host sees the kind

> **For agentic workers:** this file is one task, p4.3. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p4.1 to p4.2 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `11-phase4-supervision.md`. Deviations from it, and why: `p4-plan-1.md`.

### Task p4.3: Errors name their task; the host sees the kind

An error records the ID of the task it arose in, set once like its position. `match` shows it to the program as `e["task"]` (p4.7), and it is how a supervisor's error says which attempt failed (`root/0#3`, p4.11).
- `at(e, node, task)` fills in only what `e` lacks, and returns `e` itself when it lacks nothing. `Eval` stamps an error that lacks a position or a task, so the task is the innermost one the error passed through: the one where it arose.
- A spawned task whose error has no position (no Arv node ran, as in `spawn getenv("X")`) is placed at the `spawn`, in the child's name.
- `arv.Error` gets `Kind`, so the host can tell a denied capability from a bug.

Together with p4.2 this is the guarantee "errors keep Kind and position across task boundaries".

**Files:**
- Modify: `arv/arv.go`
- Modify: `arv/evaluator/concurrency.go`
- Modify: `arv/evaluator/evaluator.go`
- Modify: `arv/value/value.go`
- Create (test): `arv/errkind_test.go`
- Create (test): `arv/evaluator/errpos_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/errkind_test.go`:

```go
package arv

import (
	"errors"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// The host sees what caused an error that ended the run.
func TestRunErrorKind(t *testing.T) {
	for _, tt := range []struct{ src, kind string }{
		{`1 / 0`, runtime.KindRuntime},
		{`scope (timeout_ms: 10) { sleep(5000) }`, runtime.KindBudget},
		{`let f = fn() { 1 }; spawn f() with [env("ARV_KIND_X")]`, runtime.KindCapability},
	} {
		_, err := Run(tt.src, Options{})
		var ae *Error
		if !errors.As(err, &ae) || ae.Kind != tt.kind {
			t.Errorf("%s: got %#v, want an *Error of kind %q", tt.src, err, tt.kind)
		}
	}
}
```

Create `arv/evaluator/errpos_test.go`:

```go
package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// A task's error crosses into the scope that ran it with its kind, its
// position and the ID of the task it arose in.
func TestChildErrorKeepsKindPosAndTask(t *testing.T) {
	got := runLimited(t, "let f = fn() {\n  scope (timeout_ms: 10) { sleep(5000) } };\nlet t = spawn f();\nawait t", runtime.Limits{})
	e := wantKind(t, got, runtime.KindBudget)
	if e.Pos != "2:28" || e.Task != "root/0" {
		t.Fatalf("pos %q task %q, want 2:28 in root/0", e.Pos, e.Task)
	}
	// No Arv node runs in spawn f() with [...]: placed at the spawn.
	e = wantKind(t, runLimited(t, "let f = fn() { 1 };\n spawn f() with [env(\"ARV_KIND_X\")]", runtime.Limits{}), runtime.KindCapability)
	if e.Pos != "2:2" || e.Task != "root" {
		t.Fatalf("pos %q task %q, want 2:2 in root", e.Pos, e.Task)
	}
	// The root's own errors name the root.
	e = wantKind(t, runLimited(t, `1 / 0`, runtime.Limits{}), runtime.KindRuntime)
	if e.Task != "root" {
		t.Fatalf("task %q, want root", e.Task)
	}
}

// at fills only what an error lacks, and keeps its kind.
func TestAtKeepsKind(t *testing.T) {
	node := parse(t, "\n  x").Statements[0]
	e := at(&value.Error{Message: "m", Kind: runtime.KindEffect}, node, "root/3")
	if e.Kind != runtime.KindEffect || e.Pos != "2:3" || e.Task != "root/3" {
		t.Fatalf("got %+v", *e)
	}
	placed := &value.Error{Message: "m", Pos: "9:9", Task: "root/1", Kind: runtime.KindUser}
	if got := at(placed, node, "root/3"); got != placed {
		t.Fatalf("at copied an error that lacked nothing: %+v", *got)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/ ./arv/evaluator/ -run 'TestRunErrorKind|TestChildErrorKeepsKindPosAndTask|TestAtKeepsKind'`

Expected: FAIL. The build fails: (the first lines; ports and times vary)

```text
arv/errkind_test.go:19:33: ae.Kind undefined (type *Error has no field or method Kind)
arv/evaluator/errpos_test.go:15:26: e.Task undefined (type *value.Error has no field or method Task)
arv/evaluator/errpos_test.go:16:60: e.Task undefined (type *value.Error has no field or method Task)
arv/evaluator/errpos_test.go:20:25: e.Task undefined (type *value.Error has no field or method Task)
```

- [ ] **Step 3: Implement**

In `arv/arv.go`, replace:

```go
	result = evaluator.New(root).RunLimited(program, value.NewEnvironment(), opts.Limits)
	if e, ok := result.(*value.Error); ok {
		return result, &Error{Msg: e.Message, Pos: e.Pos}
	}
	return result, nil
```

with:

```go
	result = evaluator.New(root).RunLimited(program, value.NewEnvironment(), opts.Limits)
	if e, ok := result.(*value.Error); ok {
		return result, &Error{Msg: e.Message, Pos: e.Pos, Kind: e.ErrKind()}
	}
	return result, nil
```

In `arv/arv.go`, replace:

```go
// Error is an Arv error that reached the top of a run. Error() is the
// message alone; Pos ("line:col", or "" if unknown) says where in the
// source it arose, for the host to report with the file name.
type Error struct {
	Msg string
	Pos string
}
```

with:

```go
// Error is an Arv error that reached the top of a run. Error() is the
// message alone; Pos ("line:col", or "" if unknown) says where in the
// source it arose, for the host to report with the file name. Kind is
// what caused it, one of the runtime.Kind* constants.
type Error struct {
	Msg  string
	Pos  string
	Kind string
}
```

In `arv/evaluator/concurrency.go`, replace:

```go
	})
	in.releaseDepth(root.budget, 0) // the next run (in the REPL) has a new pool
	if e, ok := result.(*value.Error); ok && e.Pos == "" {
		return placeAtEnd(e, node)
	}
	return result
}

// placeAtEnd stamps e with the position of node's last statement if node
// is a program, else of node; "1:1" for an empty program. Like at, it
// copies and keeps the kind.
func placeAtEnd(e *value.Error, node ast.Node) *value.Error {
	if p, ok := node.(*ast.Program); ok && len(p.Statements) > 0 {
		node = p.Statements[len(p.Statements)-1]
	}
	if line, _ := ast.Pos(node); line == 0 {
		c := *e
		c.Pos = "1:1"
		return &c
	}
	return at(e, node)
}
```

with:

```go
	})
	in.releaseDepth(root.budget, 0) // the next run (in the REPL) has a new pool
	if e, ok := result.(*value.Error); ok && (e.Pos == "" || e.Task == "") {
		return at(placeAtEnd(e, node), nil, in.Task.ID)
	}
	return result
}

// placeAtEnd stamps e, unless it has a position, with the position of
// node's last statement if node is a program, else of node; "1:1" for an
// empty program.
func placeAtEnd(e *value.Error, node ast.Node) *value.Error {
	if p, ok := node.(*ast.Program); ok && len(p.Statements) > 0 {
		node = p.Statements[len(p.Statements)-1]
	}
	if line, _ := ast.Pos(node); line == 0 && e.Pos == "" {
		c := *e
		c.Pos = "1:1"
		return &c
	}
	return at(e, node, e.Task)
}
```

In `arv/evaluator/concurrency.go`, replace:

```go
// failure carries an Arv error value through runtime.Scope, which stores
// Go errors, so the value (with its position and kind) comes out the other
// side. It tells the runtime the value's kind (runtime.Kinded), so a
// supervisor can decide whether to restart.
type failure struct{ v *value.Error }
```

with:

```go
// failure carries an Arv error value through runtime.Scope, which stores
// Go errors, so the value (with its position, task and kind) comes out the
// other side. It tells the runtime the value's kind (runtime.Kinded), so a
// supervisor can decide whether to restart.
type failure struct{ v *value.Error }
```

In `arv/evaluator/concurrency.go`, replace:

```go
		result = startTask(child, top.budget.Task(), fn, args)
		if e, ok := result.(*value.Error); ok {
			if e.Pos == "" { // e.g. spawn getenv("X"): no Arv node ran
				e = at(e, node)
				result = e
			}
```

with:

```go
		result = startTask(child, top.budget.Task(), fn, args)
		if e, ok := result.(*value.Error); ok {
			if e.Pos == "" || e.Task == "" { // e.g. spawn getenv("X"): no Arv node ran
				e = at(e, node, child.ID)
				result = e
			}
```

In `arv/evaluator/evaluator.go`, replace:

```go
		in.releaseDepth(in.top().budget, in.held-depthChunk)
	}
	if e, ok := result.(*value.Error); ok && e.Pos == "" {
		return at(e, node)
	}
	return result
```

with:

```go
		in.releaseDepth(in.top().budget, in.held-depthChunk)
	}
	if e, ok := result.(*value.Error); ok && (e.Pos == "" || e.Task == "") {
		return at(e, node, in.Task.ID)
	}
	return result
```

In `arv/evaluator/evaluator.go`, replace:

```go
}

// at returns e stamped with where node starts, or e itself if node has no
// position. It copies, keeping every other field (the kind among them):
// an error value may already be shared (a task's result is read by every
// task that awaits it), so it is never mutated.
func at(e *value.Error, node ast.Node) *value.Error {
	line, col := ast.Pos(node)
	if line == 0 {
		return e
	}
	c := *e
	c.Pos = fmt.Sprintf("%d:%d", line, col)
	return &c
}
```

with:

```go
}

// at returns e with what it lacks filled in: the position where node
// starts (if node has one) and task, the ID of the task it arose in. It
// copies, keeping every other field (the kind among them): an error value
// may already be shared (a task's result is read by every task that
// awaits it), so it is never mutated. e itself comes back if it lacks
// neither.
func at(e *value.Error, node ast.Node, task string) *value.Error {
	if e.Pos != "" && e.Task != "" {
		return e
	}
	c := *e
	if c.Task == "" {
		c.Task = task
	}
	if line, col := ast.Pos(node); c.Pos == "" && line != 0 {
		c.Pos = fmt.Sprintf("%d:%d", line, col)
	}
	return &c
}
```

In `arv/value/value.go`, replace:

```go
// Error is an Arv error value. Pos is "line:col" of the innermost node it
// arose in or passed through ("" if none); the evaluator sets it once, on
// a copy, and never changes it after. Kind is one of the runtime.Kind*
// constants ("" reads as runtime.KindRuntime, see ErrKind).
type Error struct {
	Message    string
```

with:

```go
// Error is an Arv error value. Pos is "line:col" of the innermost node it
// arose in or passed through ("" if none), and Task the ID of the task it
// arose in; the evaluator sets each once, on a copy, and never changes it
// after. Kind is one of the runtime.Kind* constants ("" reads as
// runtime.KindRuntime, see ErrKind).
type Error struct {
	Message    string
```

In `arv/value/value.go`, replace:

```go
	Pos        string
	Kind       string
}
```

with:

```go
	Pos        string
	Kind       string
	Task       string
}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/ ./arv/evaluator/ -run 'TestRunErrorKind|TestChildErrorKeepsKindPosAndTask|TestAtKeepsKind'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/arv.go arv/errkind_test.go arv/evaluator/concurrency.go arv/evaluator/errpos_test.go arv/evaluator/evaluator.go arv/value/value.go && git commit -F - <<'MSG'
feat(evaluator): errors name their task; arv.Error reports the kind

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
