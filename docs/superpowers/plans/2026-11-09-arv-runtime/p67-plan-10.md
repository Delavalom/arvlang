# Phase 6x7, task p67.10: `examples/host` records and replays

> **For agentic workers:** this file is one task, p67.10. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p67.1 to p67.9 are committed on `phase/p67`. Work in `/Users/delavalom/delavalom-labs/arvlang-wt/p67` on branch `phase/p67`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 &&`: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push, never touch remotes; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `min`, `max` and `t.Chdir` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report status "blocked", reason "disk" (only `go clean -cache` may be deleted). The machine is shared and heavily loaded: keep wall-clock bounds generous and order events with server gates, not sleeps. No test may reach the network or spend money: every test uses the fake provider or a local `httptest` server; never set `ARV_LIVE_ANTHROPIC`, never put a real API key anywhere. If a test exposes a real bug outside this task, mark it `t.Skip("BUG: ...")` with a reproduction and report it; never weaken a test to make it pass.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` at `b516ca2` (Phases 3 to 8.3 and 5E merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`); each "expected failure" was seen. Designs: `13-phase6-durable-replay.md`, `14-phase7-agents.md`; what was built overrides them (`16-implementation-notes.md`, `16o-phase-index.md`). Phase overview and deviations: `p67-plan-1.md`.

### Task p67.10: `examples/host` records and replays

The last part of §8.2's acceptance test ("denied without the grant, allowed with it, recorded and replayed"). `examples/host` takes `-record FILE` and `-replay FILE`. A replay takes its grants and limits from the log, so `-allow` with `-replay` is an error, and the limits are not set. Its script spawns a child with `k8s("staging")`, so this needs p67.2. The replay prints the same lines and an empty cluster: `k8s_apply` never ran. `run` gains a `logs` argument.

**Files:**
- Modify (test): `examples/host/main_test.go`
- Modify: `examples/host/main.go`

- [ ] **Step 1: Write the failing test**

Replace the whole of `examples/host/main_test.go` with:

```go
package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// The acceptance test of 15-phase8-tooling.md §8.2 (its recording and
// replay are TestHostExampleRecordsAndReplays): k8s_apply is denied
// without the grant and allowed with it, in the root and in a child the
// root hands k8s("staging"); prod stays out of reach.
func TestHostExample(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, logs{}, &out, &errOut); code != 1 {
		t.Fatalf("no grant: exit %d, want 1; stdout %q", code, out.String())
	}
	if want := `error: capability denied: k8s("staging") in task root (holds: [])`; !strings.Contains(errOut.String(), want) {
		t.Fatalf("no grant: stderr %q, want %q", errOut.String(), want)
	}
	if out.Len() != 0 {
		t.Fatalf("no grant: stdout %q, want nothing", out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"staging"}, logs{}, &out, &errOut); code != 0 {
		t.Fatalf("staging: exit %d; stderr %q", code, errOut.String())
	}
	want := "web.yaml\napi.yaml\nprod: capability\ncluster: staging=[web.yaml api.yaml]\n"
	if out.String() != want {
		t.Fatalf("staging: stdout %q, want %q", out.String(), want)
	}
}

// The rest of §8.2's acceptance test: the run is recorded, and its replay
// prints the same lines without calling k8s_apply (the cluster stays
// empty), with no -allow, its grants taken from the log.
func TestHostExampleRecordsAndReplays(t *testing.T) {
	log := filepath.Join(t.TempDir(), "run.jsonl")
	var out, errOut bytes.Buffer
	if code := run([]string{"staging"}, logs{record: log}, &out, &errOut); code != 0 {
		t.Fatalf("record: exit %d; stderr %q", code, errOut.String())
	}
	lines := "web.yaml\napi.yaml\nprod: capability\n"
	if out.String() != lines+"cluster: staging=[web.yaml api.yaml]\n" {
		t.Fatalf("record: stdout %q", out.String())
	}
	out.Reset()
	if code := run(nil, logs{replay: log}, &out, &errOut); code != 0 || out.String() != lines+"cluster: \n" {
		t.Fatalf("replay: exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
	if code := run([]string{"staging"}, logs{replay: log}, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "drop -allow") {
		t.Fatalf("replay with -allow: exit %d, stderr %q", code, errOut.String())
	}
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test ./examples/host -count=1`

Expected: FAIL. The test does not build: `too many arguments in call to run` and `undefined: logs`.

- [ ] **Step 3: Add the flags**

In `examples/host/main.go` (edit 1 of 6), replace:

```go
//	go run ./examples/host -allow staging   # applies to staging; prod stays denied
package main
```

with:

```go
//	go run ./examples/host -allow staging   # applies to staging; prod stays denied
//	go run ./examples/host -allow staging -record run.jsonl
//	go run ./examples/host -replay run.jsonl  # same output; the cluster is untouched
//
// A replay calls no effect: its answers come from the log, so the fake
// cluster it prints at the end is empty.
package main
```

In `examples/host/main.go` (edit 2 of 6), replace:

```go

// run runs the script holding k8s(ns) for each namespace in allow, and
// returns the exit code.
func run(allow []string, stdout, stderr io.Writer) int {
	c := &cluster{applied: map[string][]string{}}
```

with:

```go

// logs names an effect log to write (record) or to replay (replay).
type logs struct{ record, replay string }

// run runs the script holding k8s(ns) for each namespace in allow, and
// returns the exit code. With lg.record it writes the run's effect log;
// with lg.replay it replays one, taking its grants and limits from it.
func run(allow []string, lg logs, stdout, stderr io.Writer) int {
	c := &cluster{applied: map[string][]string{}}
```

In `examples/host/main.go` (edit 3 of 6), replace:

```go
	}
	_, err := arv.Run(script, arv.Options{
		Caps:   caps,
```

with:

```go
	}
	opts := arv.Options{
		Caps:   caps,
```

In `examples/host/main.go` (edit 4 of 6), replace:

```go
		},
	})
	if err != nil {
```

with:

```go
		},
	}
	if err := lg.setup(&opts); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if f, ok := opts.Record.(*os.File); ok {
		defer f.Close()
	}
	_, err := arv.Run(script, opts)
	if err != nil {
```

In `examples/host/main.go` (edit 5 of 6), replace:

```go
	return 0
}
```

with:

```go
	return 0
}

// setup opens lg's log for opts.
func (lg logs) setup(opts *arv.Options) error {
	switch {
	case lg.record != "" && lg.replay != "":
		return errors.New("-record or -replay, not both")
	case lg.record != "":
		f, err := os.OpenFile(lg.record, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		opts.Record = f
	case lg.replay != "":
		if len(opts.Caps) > 0 {
			return errors.New("a replay takes its grants from the log: drop -allow")
		}
		f, err := os.Open(lg.replay)
		if err != nil {
			return err
		}
		defer f.Close()
		l, err := runtime.ReadLog(f)
		if err != nil {
			return err
		}
		opts.Replay, opts.Limits = l, runtime.Limits{}
	}
	return nil
}
```

In `examples/host/main.go` (edit 6 of 6), replace:

```go
	var allow list
	flag.Var(&allow, "allow", "grant k8s(`NAMESPACE`); repeat for more")
	flag.Parse()
	os.Exit(run(allow, os.Stdout, os.Stderr))
}
```

with:

```go
	var allow list
	var lg logs
	flag.Var(&allow, "allow", "grant k8s(`NAMESPACE`); repeat for more")
	flag.StringVar(&lg.record, "record", "", "write the run's effect log to `FILE`")
	flag.StringVar(&lg.replay, "replay", "", "replay the effect log in `FILE`")
	flag.Parse()
	os.Exit(run(allow, lg, os.Stdout, os.Stderr))
}
```

- [ ] **Step 4: Run it and see it pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test ./examples/host -count=1 && go run ./examples/host -allow staging`

Expected: PASS. `go run` prints the four lines of `TestHostExample`.

- [ ] **Step 5: Check and commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && gofmt -l . && go vet ./... && go test -race ./...
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && git add -A && git commit -m "feat(examples): the host example records and replays

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
