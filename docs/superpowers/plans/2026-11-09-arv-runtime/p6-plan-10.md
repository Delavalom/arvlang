# Phase 6, task p6.10: spawn as an effect; await, the cap, deadlines and backoffs through checks

> **For agentic workers:** this file is one task, p6.10. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p6.1 to p6.9 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`. `arv/runtime` stays stdlib-only.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `13-phase6-durable-replay.md`. Deviations from it, and why: `p6-plan-1.md`.

### Task p6.10: spawn as an effect; await, the cap, deadlines and backoffs through checks

- **`spawn ... with`** canonicalises its grants, reading the filesystem (lstat, readlink): OS input, so with a log it is the effect `spawn` (`attenuate` in `spawncaps.go`). Its request is the requested capabilities as text, its result the canonical grants as `[kind, scope]` pairs. Replayed, `loggedCaps` checks each logged grant with `runtime.Literal` (canonical) and `Caps.Allows` (held by the task): a forged log cannot widen I2.
- **`await`** waits through `Task.WaitFor`; **`spawn`** takes its slot through `Task.Acquire`; **`scope`** asks `Task.Expired` as it closes; **`restart`** waits through `SuperviseWith` and the failed attempt's `Task.Backoff`.

**Files:**
- Modify: `arv/evaluator/concurrency.go`
- Create: `arv/evaluator/spawncaps.go`
- Create (test): `arv/evaluator/spawn_replay_test.go`
- Create (test): `arv/replay_spawn_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/spawn_replay_test.go`:

```go
package evaluator

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// Canonicalising a spawn's grants reads the filesystem, so it is an
// effect: a replay takes the child's grants from the log, and checks each
// against the task's own, so a forged log cannot widen them (I2).
func TestReplayedSpawnGrants(t *testing.T) {
	a, _ := runtime.New(runtime.Env, "ARV_P6_A")
	src := `let f = fn() { 1 }; await spawn f() with [env("ARV_P6_A")]`
	_, res, log := recordLogged(t, src, a)
	if res.Inspect() != "1" || !bytes.Contains(log, []byte(`"kind":"spawn","req":["env(\"ARV_P6_A\")"],"res":[["env","ARV_P6_A"]]`)) {
		t.Fatalf("recording returned %s, logged\n%s", res.Inspect(), log)
	}
	if _, rres, err := replayLogged(t, src, log, a); err != nil || rres.Inspect() != "1" {
		t.Fatalf("replay: %s, %v", rres.Inspect(), err)
	}

	var forged bytes.Buffer
	l, _ := runtime.NewRecorder(&forged, runtime.Header{}, &bytes.Buffer{}, nil)
	root := runtime.NewRoot(l.Start(context.Background()), runtime.NewCapSet(a), &bytes.Buffer{})
	root.SetLog(l)
	root.Effect("spawn", []byte(`["env(\"ARV_P6_A\")"]`), func() runtime.Outcome {
		return runtime.Outcome{Res: []byte(`[["env","ARV_P6_B"]]`)}
	})
	l.Finish([]byte(`[1]`))
	_, rres, err := replayLogged(t, src, forged.Bytes(), a)
	e, ok := rres.(*value.Error)
	want := `the log's grants for a spawn include env("ARV_P6_B"), which the task does not hold`
	if !ok || e.ErrKind() != runtime.KindReplay || !strings.Contains(e.Message, want) || err == nil {
		t.Fatalf("a forged log: %s, %v", rres.Inspect(), err)
	}
}
```

Create `arv/replay_spawn_test.go`:

```go
package arv

import (
	"bytes"
	"fmt"
	"testing"
	"time"
)

// A failure that cancels its siblings replays the same: the sibling still
// prints what it printed before it was cancelled, and the run ends with
// the same error, though the replay runs at another speed.
func TestReplayOfFailFast(t *testing.T) {
	src := `let boom = fn() { sleep(200); error("boom") };
let busy = fn() { puts("b1"); sleep(100); puts("b2"); sleep(60000); puts("never") };
scope { spawn boom(); spawn busy(); puts("body"); sleep(60000) }`
	recorded, log, err := recordRun(t, src, Options{})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("recording: %v", err)
	}
	for i := 0; i < 20; i++ {
		replayed, err := replayRun(t, src, log)
		if err == nil || err.Error() != "boom" || replayed != recorded {
			t.Fatalf("replay %d: %v, printed %q, recorded %q", i, err, replayed, recorded)
		}
	}
}

// Restart attempts replay in order, without their backoffs.
func TestReplayOfRestarts(t *testing.T) {
	srv, hits := counted(t)
	src := fmt.Sprintf(`let f = fn() { let r = http_get("%s/flaky"); if (r["status"] == 200) { r["body"] } else { error("down") } };
scope (supervise: "restart", retries: 3, backoff_ms: 300, retry_on: ["user"]) { puts(await spawn f() with [net("127.0.0.1")]) }`, srv.URL)
	recorded, log, err := recordRun(t, src, Options{Caps: loopback})
	if err != nil || recorded != "body of /flaky\n" || hits.Load() != 3 {
		t.Fatalf("recording: %v, %q, %d requests", err, recorded, hits.Load())
	}
	for _, id := range []string{`"task":"root/0","seq":1`, `"task":"root/0#2","seq":1`, `"task":"root/0#3","seq":1`} {
		if !bytes.Contains(log, []byte(id)) {
			t.Errorf("log has no %s", id)
		}
	}
	start := time.Now()
	replayed, err := replayRun(t, src, log)
	if took := time.Since(start); took >= 900*time.Millisecond {
		t.Errorf("replay took %s: it waited out the backoffs", took)
	}
	if err != nil || replayed != recorded || hits.Load() != 3 {
		t.Fatalf("replay: %v, %q, %d requests", err, replayed, hits.Load())
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/ ./arv/evaluator/ -run 'TestReplayedSpawnGrants|TestReplayOfFailFast|TestReplayOfRestarts'`

Expected: `TestReplayedSpawnGrants` fails (the log has no `spawn` record) and `TestReplayOfRestarts` fails (`replay took ...: it waited out the backoffs`). `TestReplayOfFailFast` may already pass: it guards the wiring of `WaitFor` and the checks against regressions.

- [ ] **Step 3: Implement**

In `arv/evaluator/concurrency.go`, replace:

```go
		s.Fail(bodyErr)
	}
	switch err := s.Wait(); {
	case s.Expired() != nil:
		// The scope's own deadline passed before it closed: report the
		// timeout itself, even if everything in it succeeded late, where
		// the body was when time ran out (if it got that far).
		e := errorOf(s.Expired(), runtime.KindBudget)
		if bodyErr != nil {
			e.Pos = result.(*value.Error).Pos
```

with:

```go
		s.Fail(bodyErr)
	}
	err := s.Wait()
	expired := in.Task.Expired(s) // a check: a replay times out where the recording did
	switch {
	case expired != nil:
		// The scope's own deadline passed before it closed: report the
		// timeout itself, even if everything in it succeeded late, where
		// the body was when time ran out (if it got that far).
		e := errorOf(expired, runtime.KindBudget)
		if bodyErr != nil {
			e.Pos = result.(*value.Error).Pos
```

In `arv/evaluator/concurrency.go`, replace:

```go
		reqs = arr.Elements
	}
	// Each request is canonicalised (reading the filesystem, O(depth x
	// length) bytes) into a new scope the child keeps: charge both, and
	// stop once the task is cancelled, since a program can pass any
	// number of requests.
	var stop *value.Error
	req := func(i int) runtime.Capability { return reqs[i].(*value.Capability).Cap }
	charge := func(n int64) error {
		if stop = in.charge(n); stop != nil {
			return errors.New(stop.Message)
		}
		return nil
	}
	caps, err := in.Task.Caps.AttenuateEach(len(reqs), req, charge, func(c runtime.Capability) error {
		if err := in.Task.Err(); err != nil {
			stop = newErrorKind(runtime.KindCancelled, "spawn in task %s cancelled: %s", in.Task.ID, err)
		} else {
			stop = in.charge(capabilitySize + int64(len(c.Scope)))
		}
		if stop != nil {
			return errors.New(stop.Message)
		}
		return nil
	})
	if stop != nil {
		return stop
	}
	if err != nil { // denied, or an OS error canonicalising inside a grant
		return newErrorKind(runtime.ErrorKind(err, runtime.KindEffect), "spawn in task %s: %s", in.Task.ID, err)
	}

```

with:

```go
		reqs = arr.Elements
	}
	caps, errv := in.attenuate(reqs)
	if errv != nil {
		return errv
	}

```

In `arv/evaluator/concurrency.go`, replace:

```go
	// the wait. Past the run's live-task cap, spawn fails instead.
	top := in.top()
	if err := top.budget.Acquire(in.Task.Ctx); err != nil {
		var ex *runtime.ExceededError
		if errors.As(err, &ex) {
```

with:

```go
	// the wait. Past the run's live-task cap, spawn fails instead.
	top := in.top()
	if err := in.Task.Acquire(top.budget); err != nil {
		var ex *runtime.ExceededError
		if errors.As(err, &ex) {
```

In `arv/evaluator/concurrency.go`, replace:

```go
	// the same budgets: nothing is refunded between attempts.
	var result value.Object
	scope.Supervise(child.ID, func(n int) error {
		t := child.Attempt(n)
		result = newError("task %s did not finish", t.ID) // if runTask panics
		result = startTask(t, top.budget.Task(), fn, args)
```

with:

```go
	// the same budgets: nothing is refunded between attempts.
	var result value.Object
	var last *runtime.Task // the attempt that ran last
	scope.SuperviseWith(child.ID, func(n int) error {
		t := child.Attempt(n)
		last = t
		result = newError("task %s did not finish", t.ID) // if runTask panics
		result = startTask(t, top.budget.Task(), fn, args)
```

In `arv/evaluator/concurrency.go`, replace:

```go
		}
		return nil
	}, func() {
		// Runs even if runTask panics: no slot or live task leaks, and no
```

with:

```go
		}
		return nil
	}, func(n int, wait func() bool) bool {
		return last.Backoff(wait) // a check of the attempt that failed
	}, func() {
		// Runs even if runTask panics: no slot or live task leaks, and no
```

In `arv/evaluator/concurrency.go`, replace:

```go

// wait blocks until h finishes or the awaiting task is cancelled.
func (in *Interpreter) wait(h *value.TaskHandle) value.Object {
	select {
	case <-h.Done():
		h.MarkAwaited()
		return h.Result()
	case <-in.Task.Ctx.Done():
		return newErrorKind(runtime.KindCancelled, "task %s cancelled while awaiting %s: %s", in.Task.ID, h.ID, context.Cause(in.Task.Ctx))
	}
}
```

with:

```go

// wait blocks until h finishes or the awaiting task is cancelled.
// It is a check (runtime.Task.WaitFor): a replay ends it as the recorded
// run's did.
func (in *Interpreter) wait(h *value.TaskHandle) value.Object {
	if err := in.Task.WaitFor(h.Done()); err != nil {
		return newErrorKind(runtime.KindCancelled, "task %s cancelled while awaiting %s: %s", in.Task.ID, h.ID, err)
	}
	h.MarkAwaited()
	return h.Result()
}
```

Create `arv/evaluator/spawncaps.go`:

```go
package evaluator

import (
	"errors"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// attenuate is the child's grants for spawn ... with reqs (I2): each
// request canonicalised and checked against the task's own grants.
// Canonicalising reads the filesystem (lstat, readlink), which is OS input,
// so with an effect log it is an effect named "spawn" (I7): a replay gets
// the canonical grants, or the error, from the log, and checks that the
// task holds every grant the log gives the child, so a forged log cannot
// widen it.
func (in *Interpreter) attenuate(reqs []value.Object) (runtime.CapSet, *value.Error) {
	if in.Task.Log() == nil || len(reqs) == 0 {
		return in.attenuateLive(reqs, in.chargeBytes)
	}
	var caps runtime.CapSet
	ran := false
	names := make([]value.Object, len(reqs))
	for i, r := range reqs {
		names[i] = &value.String{Value: r.(*value.Capability).Cap.String()}
	}
	result := in.effect("spawn", names, func(charge func(int64) error) value.Object {
		ran = true
		c, errv := in.attenuateLive(reqs, charge)
		if errv != nil {
			return errv
		}
		caps = c
		list := c.List()
		els := make([]value.Object, len(list))
		for i, g := range list {
			els[i] = &value.Array{Elements: []value.Object{&value.String{Value: string(g.Kind)}, &value.String{Value: g.Scope}}}
		}
		return &value.Array{Elements: els}
	})
	if e, ok := result.(*value.Error); ok {
		return runtime.CapSet{}, e
	}
	if ran {
		return caps, nil
	}
	return in.loggedCaps(result)
}

// loggedCaps is the grants a replayed spawn's log gives the child, each
// checked: a valid capability, already canonical, held by the task.
func (in *Interpreter) loggedCaps(v value.Object) (runtime.CapSet, *value.Error) {
	bad := func(what string) (runtime.CapSet, *value.Error) {
		err := in.Task.Log().Diverge("at task %s: the log's grants for a spawn %s", in.Task.ID, what)
		return runtime.CapSet{}, errorOf(err, runtime.KindReplay)
	}
	arr, ok := v.(*value.Array)
	if !ok {
		return bad("are not a list")
	}
	caps := make([]runtime.Capability, 0, len(arr.Elements))
	for _, el := range arr.Elements {
		pair, ok := el.(*value.Array)
		if !ok || len(pair.Elements) != 2 {
			return bad("are malformed")
		}
		kind, ok1 := pair.Elements[0].(*value.String)
		scope, ok2 := pair.Elements[1].(*value.String)
		if !ok1 || !ok2 {
			return bad("are malformed")
		}
		c, err := runtime.Literal(runtime.Kind(kind.Value), scope.Value)
		if err != nil || c.Scope != scope.Value || !in.Task.Caps.Allows(c) {
			return bad("include " + runtime.Capability{Kind: runtime.Kind(kind.Value), Scope: scope.Value}.String() + ", which the task does not hold")
		}
		caps = append(caps, c)
	}
	return runtime.NewCapSet(caps...), nil
}

// attenuateLive canonicalises and checks reqs. Each request is
// canonicalised (reading the filesystem, O(depth x length) bytes) into a
// new scope the child keeps: charge both, and stop once the task is
// cancelled, since a program can pass any number of requests. It runs
// only live (inside the gate), so it looks at the task's context directly.
func (in *Interpreter) attenuateLive(reqs []value.Object, charge func(int64) error) (runtime.CapSet, *value.Error) {
	var stop *value.Error
	req := func(i int) runtime.Capability { return reqs[i].(*value.Capability).Cap }
	charged := func(n int64) error {
		if err := charge(n); err != nil {
			stop = errorOf(err, runtime.KindBudget)
			return errors.New(stop.Message)
		}
		return nil
	}
	caps, err := in.Task.Caps.AttenuateEach(len(reqs), req, charged, func(c runtime.Capability) error {
		if err := in.Task.LiveErr(); err != nil {
			stop = newErrorKind(runtime.KindCancelled, "spawn in task %s cancelled: %s", in.Task.ID, err)
			return errors.New(stop.Message)
		}
		return charged(capabilitySize + int64(len(c.Scope)))
	})
	if stop != nil {
		return runtime.CapSet{}, stop
	}
	if err != nil { // denied, or an OS error canonicalising inside a grant
		return runtime.CapSet{}, newErrorKind(runtime.ErrorKind(err, runtime.KindEffect), "spawn in task %s: %s", in.Task.ID, err)
	}
	return caps, nil
}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/ ./arv/evaluator/ -run 'TestReplayedSpawnGrants|TestReplayOfFailFast|TestReplayOfRestarts'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/evaluator/concurrency.go arv/evaluator/spawn_replay_test.go arv/evaluator/spawncaps.go arv/replay_spawn_test.go && git commit -F - <<'MSG'
feat(evaluator): spawn grants are an effect; await, cap, deadlines and backoffs are checks

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
