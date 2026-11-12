# Phase 6x7, task p67.9: e2e: `p82host` records, replays and resumes

> **For agentic workers:** this file is one task, p67.9. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p67.1 to p67.8 are committed on `phase/p67`. Work in `/Users/delavalom/delavalom-labs/arvlang-wt/p67` on branch `phase/p67`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 &&`: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push, never touch remotes; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `min`, `max` and `t.Chdir` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report status "blocked", reason "disk" (only `go clean -cache` may be deleted). The machine is shared and heavily loaded: keep wall-clock bounds generous and order events with server gates, not sleeps. No test may reach the network or spend money: every test uses the fake provider or a local `httptest` server; never set `ARV_LIVE_ANTHROPIC`, never put a real API key anywhere. If a test exposes a real bug outside this task, mark it `t.Skip("BUG: ...")` with a reproduction and report it; never weaken a test to make it pass.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` at `b516ca2` (Phases 3 to 8.3 and 5E merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`); each "expected failure" was seen. Designs: `13-phase6-durable-replay.md`, `14-phase7-agents.md`; what was built overrides them (`16-implementation-notes.md`, `16o-phase-index.md`). Phase overview and deviations: `p67-plan-1.md`.

### Task p67.9: e2e: `p82host` records, replays and resumes

`16-phase82.md` left "record and replay `e2e/p82host`" to this phase. `p82host` gains `-record`, `-replay` and `-resume FILE` (`e2e/p82host/log.go`; like the CLI, a replay takes its grants and limits from the log and a resume its limits) and `k8s_gate`, a host effect that waits at a server gate, so a test can kill the host while a host call is in flight. The program calls host effects from the root and from a child spawned with `k8s("staging")` (p67.2's path), and a `team` effect (subtree coverage). A replay with no grants prints the same lines and calls no `Fn` (the call log stays empty). A resume after SIGKILL during `k8s_gate` calls only `k8s_gate` again. `checkHost` and `hostSpec` are in `e2e/p82_helpers_test.go`.

**Files:**
- Create (test): `e2e/p67_host_test.go`
- Create: `e2e/p82host/log.go`
- Modify: `e2e/p82host/main.go`

- [ ] **Step 1: Write the failing tests**

Create `e2e/p67_host_test.go`:

```go
package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Phase 8.2 host effects through the Phase 6 log, with a real Go host
// (e2e/p82host): recorded, replayed with no Fn called, and resumed after
// SIGKILL with only the call that was in flight made again.

// p67HostSrc calls host effects from the root and from a child handed
// k8s("staging") (a replayed spawn of a host kind's grant), then waits
// at the server's gate {{K}} in one more.
const p67HostSrc = `puts(k8s_apply("staging", "web.yaml")["applied"]);
let t = spawn fn() { k8s_apply("staging", "api.yaml")["applied"] }() with [k8s("staging")];
puts(await t);
puts(team_read("a/b"));
puts(k8s_gate("staging", "{{K}}"));
puts("done")
`

var p67HostCaps = []string{"k8s=staging", "team=a"}

const p67HostOut = "web.yaml\napi.yaml\nread a/b\nopen\ndone\n"

// Record, then replay with no grants: the same output, and no Fn runs.
func TestP67HostRecordAndReplay(t *testing.T) {
	t.Parallel()
	log := filepath.Join(t.TempDir(), "run.jsonl")
	arrive(testID(t), "open")
	src := strings.ReplaceAll(p67HostSrc, "{{K}}", "open")
	checkHost(t, hostSpec{src: src, caps: p67HostCaps, gates: true, flags: []string{"-record", log}, stdout: p67HostOut,
		calls: []string{"k8s_apply(staging) task=root", "k8s_apply(staging) task=root/0", "team_read(a/b) task=root", "k8s_gate(staging) task=root"}})
	checkHost(t, hostSpec{src: src, gates: true, flags: []string{"-replay", log}, stdout: p67HostOut})
	if n := hits(t, "/wait"); n != 1 {
		t.Fatalf("the gate saw %d requests, want the recording's 1", n)
	}
}

// SIGKILL while k8s_gate waits, then -resume: the calls the log holds are
// not made again; the one in flight is, and the output is the
// uninterrupted run's.
func TestP67HostResumeAfterSIGKILL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log, script := filepath.Join(dir, "run.jsonl"), filepath.Join(dir, "main.arv")
	mustWrite(t, script, expand(t, p67HostSrc, map[string]string{"K": "shut"}))
	calls := filepath.Join(dir, "calls.log")
	args := []string{"-log", calls, "-server", srv.URL, "-id", testID(t), "-record", log}
	for _, c := range p67HostCaps {
		args = append(args, "-cap", c)
	}
	cmd := exec.Command(p82Host(t), append(args, script)...)
	cmd.Env, cmd.Dir = []string{}, dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitHits(t, "/wait", 1)
	cmd.Process.Kill()
	cmd.Wait()
	if out.String() != "web.yaml\napi.yaml\nread a/b\n" {
		t.Fatalf("before the kill: %q", out.String())
	}
	lines := readLogLines(t, log)
	if kinds(lines, "host:k8s_apply") != 2 || kinds(lines, "host:team_read") != 1 || kinds(lines, "host:k8s_gate") != 0 {
		t.Fatalf("the killed run's log:\n%v", lines)
	}
	if err := os.Remove(calls); err != nil {
		t.Fatal(err)
	}
	arrive(testID(t), "shut")
	src, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	checkHost(t, hostSpec{src: string(src), caps: p67HostCaps, gates: true, flags: []string{"-resume", log},
		stdout: p67HostOut, calls: []string{"k8s_gate(staging) task=root"}, timeout: 90 * time.Second})
	checkHost(t, hostSpec{src: string(src), gates: true, flags: []string{"-replay", log}, stdout: p67HostOut})
	if n := hits(t, "/wait"); n != 2 {
		t.Fatalf("the gate saw %d requests, want 2 (the killed run's and the resume's)", n)
	}
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test ./e2e -run TestP67Host -count=1`

Expected: FAIL. Both tests fail: `exit code = 2, want 0`, stderr `flag provided but not defined: -record`.

- [ ] **Step 3: Give p82host the log flags and k8s_gate**

Create `e2e/p82host/log.go`:

```go
package main

import (
	"errors"
	"io"
	"os"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/runtime"
)

// logOptions sets opts up for -record, -replay or -resume FILE (at most
// one of them), as the arv CLI does: a replay takes its grants and limits
// from the log, a resume its limits. It returns what closes the file.
func logOptions(opts *arv.Options, record, replay, resume string) (func(), error) {
	n := 0
	for _, f := range []string{record, replay, resume} {
		if f != "" {
			n++
		}
	}
	switch {
	case n > 1:
		return nil, errors.New("-record, -replay and -resume exclude each other")
	case record != "":
		f, err := os.OpenFile(record, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, err
		}
		opts.Record = f
		return func() { f.Close() }, nil
	case replay != "":
		f, err := os.Open(replay)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		l, err := runtime.ReadLog(f)
		if err != nil {
			return nil, err
		}
		opts.Replay, opts.Caps, opts.Limits = l, nil, runtime.Limits{}
		return func() {}, nil
	case resume != "":
		f, err := os.OpenFile(resume, os.O_RDWR, 0)
		if err != nil {
			return nil, err
		}
		l, err := runtime.ReadLog(f)
		if err == nil { // drop a partial last line: the resume appends after the whole ones
			if err = f.Truncate(l.Size); err == nil {
				_, err = f.Seek(l.Size, io.SeekStart)
			}
		}
		if err != nil {
			f.Close()
			return nil, err
		}
		opts.Resume, opts.Record, opts.Limits = l, f, runtime.Limits{}
		return func() { f.Close() }, nil
	}
	return func() {}, nil
}
```

In `e2e/p82host/main.go` (edit 1 of 4), replace:

```go
//	p82host [-cap kind=scope]... [-log FILE] [-server URL -id ID]
//	        [-steps N] [-alloc-mb N] [-timeout-ms N] [-setup NAME] SCRIPT
//
```

with:

```go
//	p82host [-cap kind=scope]... [-log FILE] [-server URL -id ID]
//	        [-steps N] [-alloc-mb N] [-timeout-ms N] [-setup NAME]
//	        [-record FILE | -replay FILE | -resume FILE] SCRIPT
//
```

In `e2e/p82host/main.go` (edit 2 of 4), replace:

```go
		}),
		// k8s_call calls the server with the host's secret and returns
```

with:

```go
		}),
		// k8s_gate waits at the server's gate a[1] (/wait, one arrival)
		// and returns its answer.
		"k8s_gate": k8s("k8s_gate", func(t *runtime.Task, a []value.Object) (value.Object, error) {
			if len(a) != 2 {
				return nil, errors.New("want (namespace, gate)")
			}
			gate, ok := a[1].(*value.String)
			if !ok {
				return nil, errors.New("the gate must be STRING")
			}
			body, err := h.get(t.Ctx, "/wait", url.Values{"k": {gate.Value}, "n": {"1"}})
			if err != nil {
				return nil, err
			}
			return str(body), nil
		}),
		// k8s_call calls the server with the host's secret and returns
```

In `e2e/p82host/main.go` (edit 3 of 4), replace:

```go
	setup := fs.String("setup", "", "a bad host setup to try")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
```

with:

```go
	setup := fs.String("setup", "", "a bad host setup to try")
	record := fs.String("record", "", "record the run's effect log to `FILE`")
	replay := fs.String("replay", "", "replay the effect log in `FILE`")
	resume := fs.String("resume", "", "resume the effect log in `FILE`")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
```

In `e2e/p82host/main.go` (edit 4 of 4), replace:

```go
	opts.Limits = runtime.Limits{Steps: *steps, AllocBytes: *allocMB << 20, Timeout: time.Duration(*timeoutMS) * time.Millisecond}

```

with:

```go
	opts.Limits = runtime.Limits{Steps: *steps, AllocBytes: *allocMB << 20, Timeout: time.Duration(*timeoutMS) * time.Millisecond}
	closeLog, err := logOptions(&opts, *record, *replay, *resume)
	if err != nil {
		fmt.Fprintln(stderr, "usage:", err)
		return 2
	}
	defer closeLog()

```

- [ ] **Step 4: Run it and see it pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test ./e2e -run 'TestP67Host|TestP82' -count=1`

Expected: PASS.

- [ ] **Step 5: Check and commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && gofmt -l . && go vet ./... && go test -race ./...
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && git add -A && git commit -m "test(e2e): p82host records, replays and resumes host effects

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
