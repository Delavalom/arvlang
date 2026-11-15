# Phase 5, task p5.7: `examples/uptime.arv`

> **For agentic workers:** this file is one task, p5.7. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p5.1 to p5.6 are committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3 and 4 merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`), and `bench/uptime-go`'s own `go vet` and `go test -race`. Design: `12-phase5-gate.md`. Deviations from it, and why: `p5-plan-1.md`. **Do not measure or write the gate verdict**: a separate step does that.

### Task p5.7: the Arv uptime checker

The gate scenario in Arv, using Phases 3 and 4 as the language intends:
- `scope (tasks: 20)` around the fan-out keeps at most 20 checks running; a full scope makes `spawn` wait.
- Each check runs its own `scope (supervise: "restart", retries: 2, backoff_ms: 100, timeout_ms: 2000)`: a dropped connection is an effect error, which restart retries, and the 2 s timeout covers the attempts and their backoff. When that scope fails (retries spent, or the timeout), `match` turns its error into a "down" entry. A restart scope that gives up fails like `fail_fast`, so it must sit inside the check, not around all 200.
- Each check holds `net` for its own URL's host, and passes the same to its probe (I2). The root holds only the three `--allow` grants.
- The script writes the report before deciding the exit, and fails with its own `user` error when more than 5% are down.

It has 28 code lines (not blank, not comments); the design's target is 40, which the test enforces. Do not shorten or "improve" the script beyond what is written here: its shape is what the gate measures.

The test runs it in process (`arv.Run`) against a farm with the gate's shape (200 targets, 10% down, 5% flaky) and latencies of 5 to 40 ms, in a temp directory (`t.Chdir`), with exactly the grants its header names. It checks the report against `Expected`, the printed summary, the error, the farm's request total (exact for Arv: 200 + 10 flaky retries + 2 × 10 dropped retries = 230), the peak in flight, and that no goroutine survives the run.

**Files:**
- Create: `examples/uptime.arv`
- Create (test): `uptime_example_test.go`

- [ ] **Step 1: Write the failing test**

Create `uptime_example_test.go` (package `main`, next to `example_test.go`):

```go
package main

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/bench/farm"
)

// uptimeRun is one run of examples/uptime.arv against an in-process farm.
type uptimeRun struct {
	out   string        // what the script printed
	err   error         // what arv.Run returned
	stats farm.Stats    // the farm's counters afterwards
	want  []farm.Result // the farm's expected report
	got   []farm.Result // out/report.json
}

// runUptime runs the example in a temp dir holding targets.txt, with
// exactly the grants its header names, against a farm planned from c.
func runUptime(t *testing.T, c farm.Config) uptimeRun {
	t.Helper()
	src, err := os.ReadFile("examples/uptime.arv")
	if err != nil {
		t.Fatal(err)
	}
	f, err := farm.New(c)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(f)
	defer srv.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "targets.txt"), []byte(f.TargetList(srv.URL)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	var caps []runtime.Capability
	for _, g := range []string{"fs_read=targets.txt", "fs_write=out", "net=" + strings.TrimPrefix(srv.URL, "http://")} {
		grant, err := runtime.Parse(g, dir)
		if err != nil {
			t.Fatal(err)
		}
		caps = append(caps, grant)
	}
	var out bytes.Buffer
	r := uptimeRun{want: f.Expected(srv.URL)}
	_, r.err = arv.Run(string(src), arv.Options{Caps: caps, Out: &out, Limits: runtime.Limits{Timeout: 5 * time.Minute}})
	r.out, r.stats = out.String(), f.Stats()
	data, err := os.ReadFile(filepath.Join(dir, "out", "report.json"))
	if err != nil {
		t.Fatalf("no report (run error %v): %v", r.err, err)
	}
	if r.got, err = farm.ParseReport(data); err != nil {
		t.Fatal(err)
	}
	return r
}

// fastGate is the gate's farm with latencies short enough for a test.
var fastGate = farm.Config{N: 200, MinLatency: 5 * time.Millisecond, MaxLatency: 40 * time.Millisecond, Fail: 0.1, Flaky: 0.05, Seed: 1}

// The gate scenario: 20 of 200 down is more than 5%, so the run fails
// with the script's own error, after writing the full report. Flaky
// targets are up after one restart; dropped ones are retried twice.
func TestUptimeExampleReportsTheFarm(t *testing.T) {
	r := runUptime(t, fastGate)
	if err := farm.Compare(r.got, r.want); err != nil {
		t.Fatal(err)
	}
	var ae *arv.Error
	if !errors.As(r.err, &ae) || ae.Kind != "user" || ae.Msg != "more than 5% of the targets are down" {
		t.Fatalf("run error %v, want the user error", r.err)
	}
	if r.out != `{"down":20,"total":200}`+"\n" {
		t.Errorf("output %q", r.out)
	}
	// 200 first attempts, a second for each of the 10 flaky targets, and
	// two more for each of the 10 that drop every connection.
	if r.stats.Total != 230 {
		t.Errorf("farm saw %d requests, want 230", r.stats.Total)
	}
	if r.stats.Peak < 1 || r.stats.Peak > 20 {
		t.Errorf("peak in flight %d, want 1 to 20", r.stats.Peak)
	}
	t.Logf("peak in flight: %d", r.stats.Peak)
}

// At exactly 5% down the run succeeds.
func TestUptimeExampleAtFivePercentSucceeds(t *testing.T) {
	c := fastGate
	c.Fail = 0.05
	r := runUptime(t, c)
	if r.err != nil {
		t.Fatalf("run error %v", r.err)
	}
	if err := farm.Compare(r.got, r.want); err != nil {
		t.Fatal(err)
	}
	if r.out != `{"down":10,"total":200}`+"\n" {
		t.Errorf("output %q", r.out)
	}
}

// No goroutine of the run survives it.
func TestUptimeExampleLeaksNothing(t *testing.T) {
	c := fastGate
	c.N = 40
	before := goruntime.NumGoroutine()
	runUptime(t, c)
	for deadline := time.Now().Add(30 * time.Second); goruntime.NumGoroutine() > before; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			t.Fatalf("%d goroutines, %d before:\n%s", goruntime.NumGoroutine(), before, buf[:goruntime.Stack(buf, true)])
		}
	}
}

// The gate's concision target: at most 40 lines that are not blank or
// comments.
func TestUptimeExampleIsShort(t *testing.T) {
	src, err := os.ReadFile("examples/uptime.arv")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(string(src), "\n") {
		if l := strings.TrimSpace(line); l != "" && !strings.HasPrefix(l, "//") {
			n++
		}
	}
	if n > 40 {
		t.Errorf("%d code lines, want at most 40", n)
	}
	t.Logf("%d code lines", n)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run TestUptime .`
Expected: FAIL: `open examples/uptime.arv: no such file or directory`.

- [ ] **Step 3: Implement**

Create `examples/uptime.arv`:

```arv
// Uptime checker: the Phase 5 gate scenario. Check every URL in
// targets.txt, at most 20 at a time, each within 2 s and with up to 2
// restarts on effect errors; write out/report.json; fail if more than 5%
// of the targets are down.
//
//   arv run --allow fs_read=targets.txt --allow fs_write=out \
//           --allow net=127.0.0.1:PORT examples/uptime.arv
//
// Each check holds net access to its own URL's host and nothing else.

// One attempt. A dropped connection is an effect error: the restart
// scope in check runs the attempt again.
let probe = fn(url) {
  let start = clock_ms();
  let status = http_get(url)["status"];
  let up = if (status > 199) { status < 300 } else { false };
  {"url": url, "status": status, "up": up, "latency_ms": clock_ms() - start}
};

// One target: up to 3 attempts within 2 s. Still failing, it is down.
let check = fn(url, host) {
  match scope (supervise: "restart", retries: 2, backoff_ms: 100, timeout_ms: 2000) {
    await spawn probe(url) with [net(host)]
  } {
    ok(r) => r,
    err(e) => {"url": url, "status": 0, "up": false, "latency_ms": 0}
  }
};

let urls = lines(read_file("targets.txt"));
let results = scope (tasks: 20) {
  await for url in urls {
    let host = split(url, "/")[2];
    spawn check(url, host) with [net(host)]
  }
};
write_file("out/report.json", json(results));

let count_down = fn(rs, i) {
  if (i == len(rs)) { 0 } else { count_down(rs, i + 1) + if (rs[i]["up"]) { 0 } else { 1 } }
};
let down = count_down(results, 0);
puts(json({"down": down, "total": len(results)}));
if (down * 20 > len(results)) { error("more than 5% of the targets are down") }
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run TestUptime -v .` (it logs `peak in flight: N` and `28 code lines`), then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere. If `Total` is not 230 or the reports differ, a check timed out: that needs a stall of more than 2 s, so rerun once and report it if it repeats. Do not loosen the test.

- [ ] **Step 5: Commit**

```bash
git add examples/uptime.arv uptime_example_test.go
git commit -m "feat(examples): uptime.arv, the Phase 5 gate scenario

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
