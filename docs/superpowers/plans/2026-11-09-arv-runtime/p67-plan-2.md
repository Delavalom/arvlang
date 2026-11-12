# Phase 6x7, task p67.2: a replayed spawn keeps the run's host kinds

> **For agentic workers:** this file is one task, p67.2. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p67.1 to p67.1 are committed on `phase/p67`. Work in `/Users/delavalom/delavalom-labs/arvlang-wt/p67` on branch `phase/p67`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 &&`: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push, never touch remotes; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `min`, `max` and `t.Chdir` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report status "blocked", reason "disk" (only `go clean -cache` may be deleted). The machine is shared and heavily loaded: keep wall-clock bounds generous and order events with server gates, not sleeps. No test may reach the network or spend money: every test uses the fake provider or a local `httptest` server; never set `ARV_LIVE_ANTHROPIC`, never put a real API key anywhere. If a test exposes a real bug outside this task, mark it `t.Skip("BUG: ...")` with a reproduction and report it; never weaken a test to make it pass.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` at `b516ca2` (Phases 3 to 8.3 and 5E merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`); each "expected failure" was seen. Designs: `13-phase6-durable-replay.md`, `14-phase7-agents.md`; what was built overrides them (`16-implementation-notes.md`, `16o-phase-index.md`). Phase overview and deviations: `p67-plan-1.md`.

### Task p67.2: a replayed spawn keeps the run's host kinds

A `spawn ... with` is an effect: a replay takes the child's grants from the log and checks each with `runtime.Literal` and the parent's `Allows` (`loggedCaps`, `arv/evaluator/spawncaps.go`). `runtime.Literal` knows only the built-in kinds, so a logged `k8s("staging")` is "not a canonical capability", and the replay diverges: `the log's grants for a spawn include k8s("staging"), which the task does not hold`. And the child's set was built with `runtime.NewCapSet`, without the host's kinds, so even a child replayed in a resume could not call its host effect live. Use the run's kinds (`in.Task.Caps.Kinds()`, nil when the host declared none) for both, as `Attenuate` does for a live spawn. I2 still holds: the test forges a log that hands the child `k8s("prod")`, which the root does not hold, and the replay diverges. Test helpers used here already exist: `counted`, `applied`, `staging`, `nsScope` (`arv/hosteffect_test.go`), `countedServer`, `recordRun` (`arv/replay_test.go`), `cutAfter` (`arv/resume_checks_test.go`), `forge` (`arv/forge_test.go`).

**Files:**
- Create (test): `arv/replay_hostspawn_test.go`
- Modify: `arv/evaluator/spawncaps.go`

- [ ] **Step 1: Write the failing test**

Create `arv/replay_hostspawn_test.go`:

```go
package arv

import (
	"bytes"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// A spawn that hands a child a host kind's grant (Phase 8.2) is an
// effect, and a replay takes the child's grants from the log
// (loggedCaps). It must check them against the run's host kinds and give
// the child those kinds, as a live spawn does: otherwise k8s("staging")
// is "not held" and the replay diverges, and a resume that replays the
// spawn gives a child that cannot call its host effect.
func TestReplayOfASpawnWithAHostGrant(t *testing.T) {
	var n atomic.Int64
	effects := map[string]Effect{"k8s_apply": counted(&n, applied)}
	caps := []runtime.Capability{staging, {Kind: runtime.Net, Scope: "127.0.0.1"}}
	srv, hits := countedServer(t)
	src := `let deploy = fn(m) { k8s_apply("staging", m) };
let t = spawn deploy("web.yaml") with [k8s("staging")];
puts(await t);
puts(http_get("` + srv.URL + `/after")["body"]);
let u = spawn deploy("api.yaml") with [k8s("staging")];
puts(await u)`
	out, log, err := recordRun(t, src, Options{Caps: caps, Effects: effects})
	if err != nil || n.Load() != 2 || !strings.Contains(out, "applied to staging\n") {
		t.Fatalf("recording: %v, %d calls, printed %q", err, n.Load(), out)
	}
	t.Run("replay", func(t *testing.T) {
		l, err := runtime.ReadLog(bytes.NewReader(log))
		if err != nil {
			t.Fatal(err)
		}
		var got bytes.Buffer
		_, err = Run(src, Options{Replay: l, Out: &got, Effects: effects})
		if err != nil || got.String() != out || n.Load() != 2 || hits.Load() != 1 {
			t.Fatalf("replay: %v, %d calls, %d requests, printed %q, want %q", err, n.Load(), hits.Load(), got.String(), out)
		}
	})
	t.Run("resume", func(t *testing.T) {
		// Crashed during the request: the first spawn and its call are
		// replayed, the second spawn and its call run live.
		crashed := cutAfter(t, log, `"kind":"host:k8s_apply"`)
		l, err := runtime.ReadLog(bytes.NewReader(crashed))
		if err != nil || l.End != nil {
			t.Fatalf("crashed log: %v", err)
		}
		var got bytes.Buffer
		rec := bytes.NewBuffer(bytes.Clone(crashed))
		_, err = Run(src, Options{Caps: caps, Resume: l, Record: rec, Out: &got, Effects: effects})
		if err != nil || got.String() != out || n.Load() != 3 || hits.Load() != 2 {
			t.Fatalf("resume: %v, %d calls, %d requests, printed %q, want %q", err, n.Load(), hits.Load(), got.String(), out)
		}
	})
	t.Run("forged wider grant", func(t *testing.T) {
		// I2 still holds: a log that gives the child k8s("prod"), which the
		// root does not hold, diverges at the spawn.
		forged := forge(t, log, func(i int, b []byte) []byte {
			return bytes.Replace(b, []byte(`["k8s","staging"]`), []byte(`["k8s","prod"]`), 1)
		})
		l, err := runtime.ReadLog(bytes.NewReader(forged))
		if err != nil {
			t.Fatal(err)
		}
		_, err = Run(src, Options{Replay: l, Out: &bytes.Buffer{}, Effects: effects})
		if err == nil || !strings.Contains(err.Error(), `include k8s("prod"), which the task does not hold`) {
			t.Fatalf("forged replay: %v", err)
		}
	})
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test ./arv -run TestReplayOfASpawnWithAHostGrant -count=1`

Expected: FAIL. The `replay` and `resume` subtests fail with `replay diverged at task root: the log's grants for a spawn include k8s("staging"), which the task does not hold`; `forged wider grant` passes already.

- [ ] **Step 3: Check logged grants with the run's kinds**

In `arv/evaluator/spawncaps.go` (edit 1 of 4), replace:

```go
// loggedCaps is the grants a replayed spawn's log gives the child, each
// checked: a valid capability, already canonical, held by the task.
func (in *Interpreter) loggedCaps(v value.Object) (runtime.CapSet, *value.Error) {
```

with:

```go
// loggedCaps is the grants a replayed spawn's log gives the child, each
// checked: a valid capability, already canonical, held by the task. A
// grant of a host kind (Phase 8.2) is checked by the run's kinds, and the
// child keeps them, as Attenuate's sets do: otherwise a replayed spawn
// with k8s("staging") diverges, and a child replayed in a resume could
// not call its host effect live.
func (in *Interpreter) loggedCaps(v value.Object) (runtime.CapSet, *value.Error) {
```

In `arv/evaluator/spawncaps.go` (edit 2 of 4), replace:

```go
	}
	caps := make([]runtime.Capability, 0, len(arr.Elements))
```

with:

```go
	}
	kinds := in.Task.Caps.Kinds()
	caps := make([]runtime.Capability, 0, len(arr.Elements))
```

In `arv/evaluator/spawncaps.go` (edit 3 of 4), replace:

```go
		}
		c, err := runtime.Literal(runtime.Kind(kind.Value), scope.Value)
		if err != nil || c.Scope != scope.Value || !in.Task.Caps.Allows(c) {
```

with:

```go
		}
		c, err := kinds.Literal(runtime.Kind(kind.Value), scope.Value)
		if err != nil || c.Scope != scope.Value || !in.Task.Caps.Allows(c) {
```

In `arv/evaluator/spawncaps.go` (edit 4 of 4), replace:

```go
	}
	return runtime.NewCapSet(caps...), nil
}
```

with:

```go
	}
	return runtime.NewCapSetKinds(kinds, caps...), nil
}
```

- [ ] **Step 4: Run it and see it pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test ./arv -run TestReplayOfASpawnWithAHostGrant -count=1 && go test ./arv/evaluator -count=1 -run 'Spawn|Replay'`

Expected: PASS.

- [ ] **Step 5: Check and commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && gofmt -l . && go vet ./... && go test -race ./...
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && git add -A && git commit -m "fix(evaluator): a replayed spawn keeps the run's host kinds

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
