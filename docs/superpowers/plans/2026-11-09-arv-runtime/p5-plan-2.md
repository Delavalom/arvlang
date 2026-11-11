# Phase 5, task p5.2: `clock_ms()`

> **For agentic workers:** this file is one task, p5.2. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p5.1 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3 and 4 merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`), and `bench/uptime-go`'s own `go vet` and `go test -race`. Design: `12-phase5-gate.md`. Deviations from it, and why: `p5-plan-1.md`. **Do not measure or write the gate verdict**: a separate step does that.

### Task p5.2: `clock_ms()`

`clock_ms()` is the milliseconds since the process started, read from the monotonic clock (`time.Since`), so only differences mean anything: the uptime checker measures latency with it. Like `sleep` it needs no capability. It is a new nondeterministic input (I7), which Phase 6 must record; p5.11 notes this.

**Files:**
- Create: `arv/evaluator/clock.go`
- Create (test): `arv/evaluator/clock_test.go`

- [ ] **Step 1: Write the failing test**

Create `arv/evaluator/clock_test.go`:

```go
package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/value"
)

// clock_ms measures the time between two readings.
func TestClockMSMeasuresElapsedTime(t *testing.T) {
	got, _ := runWith(t, `let a = clock_ms(); sleep(30); let b = clock_ms(); [a, b - a]`)
	arr, ok := got.(*value.Array)
	if !ok || len(arr.Elements) != 2 {
		t.Fatalf("got %s, want [a, b - a]", got.Inspect())
	}
	a := arr.Elements[0].(*value.Integer).Value
	d := arr.Elements[1].(*value.Integer).Value
	if a < 0 {
		t.Errorf("clock_ms() = %d, want >= 0", a)
	}
	// sleep(30) waits at least 30 ms; the upper bound is generous for a
	// loaded machine.
	if d < 30 || d > 30000 {
		t.Errorf("clock_ms() after sleep(30) advanced %d ms, want 30 to 30000", d)
	}
}

func TestClockMSTakesNoArguments(t *testing.T) {
	got, _ := runWith(t, `clock_ms(1)`)
	if !isError(got) || got.(*value.Error).Message != "clock_ms: want 0 arguments, got 1" {
		t.Errorf("clock_ms(1) = %s", got.Inspect())
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run TestClockMS ./arv/evaluator/`
Expected: FAIL: `got ERROR: identifier not found: clock_ms, want [a, b - a]` and `clock_ms(1) = ERROR: identifier not found: clock_ms`.

- [ ] **Step 3: Implement**

Create `arv/evaluator/clock.go`:

```go
package evaluator

import (
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// clockStart is the zero of clock_ms. time.Since reads the monotonic
// clock, so a change to the wall clock never moves clock_ms.
var clockStart = time.Now()

func init() {
	builtins["clock_ms"] = &value.Builtin{Fn: clockMS}
}

// clock_ms() is the milliseconds since the process started, on a
// monotonic clock: only the difference of two readings means anything,
// as a latency. Like sleep it needs no capability. It is an effect (I7):
// Phase 6 must record it.
func clockMS(t *runtime.Task, args ...value.Object) value.Object {
	if len(args) != 0 {
		return newError("clock_ms: want 0 arguments, got %d", len(args))
	}
	return &value.Integer{Value: time.Since(clockStart).Milliseconds()}
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run TestClockMS ./arv/evaluator/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add arv/evaluator/clock.go arv/evaluator/clock_test.go
git commit -m "feat(evaluator): clock_ms builtin

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
