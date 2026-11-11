# Phase 5, task p5.8: the Go baseline

> **For agentic workers:** this file is one task, p5.8. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p5.1 to p5.7 are committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green, and also `cd bench/uptime-go && go vet ./... && go test -race ./...` is green (its own module, outside the root's `./...`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3 and 4 merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`), and `bench/uptime-go`'s own `go vet` and `go test -race`. Design: `12-phase5-gate.md`. Deviations from it, and why: `p5-plan-1.md`. **Do not measure or write the gate verdict**: a separate step does that.

### Task p5.8: `bench/uptime-go`

The Go version, written as for production rather than as a strawman:
- `golang.org/x/sync/errgroup` with `SetLimit(20)` as the semaphore. `g.Go` blocks while 20 checks run.
- `context.WithTimeout` of 2 s around each check, retries included.
- A retry loop on transport errors (not on HTTP statuses) with 100 ms doubling backoff and up to 10% random jitter, which waits on the context.
- `http.DefaultTransport` cloned with `MaxIdleConnsPerHost` 20, so keep-alive connections are reused.
- The body drained with `io.Copy(io.Discard, ...)`.

It reads targets with `strings.Fields`, writes `out/report.json`, prints the same summary line as the Arv version, and exits 1 with the same message. The paths are constants, as they are in the Arv script; flags would only add lines.

It is its own module, so the main module stays dependency-free (deviation 7 in `p5-plan-1.md`). `golang.org/x/sync` v0.19.0 is in the module cache and needs `go 1.24.0`. A `replace` points at the repository root, so its tests can import `bench/farm` and `arv`. The go.mod and go.sum below are exactly what `go mod tidy` produced.

`TestSameReportAsTheArvExample` is the design's "test asserting the same report": against one farm, it runs this program, resets the farm (flaky targets drop again), runs `examples/uptime.arv` in process, and requires the same report and the same printed summary.

**Files:**
- Create: `bench/uptime-go/go.mod`
- Create: `bench/uptime-go/go.sum`
- Create: `bench/uptime-go/main.go`
- Create (test): `bench/uptime-go/main_test.go`
- Modify: `Makefile`

- [ ] **Step 1: Write the module files and the failing test**

Create `bench/uptime-go/go.mod`:

```text
module github.com/delavalom/arvlang/bench/uptime-go

go 1.24.0

require (
	github.com/delavalom/arvlang v0.0.0-00010101000000-000000000000
	golang.org/x/sync v0.19.0
)

replace github.com/delavalom/arvlang => ../..
```

Create `bench/uptime-go/go.sum`:

```text
golang.org/x/sync v0.19.0 h1:vV+1eWNmZ5geRlYjzm2adRgW2/mcpevXNg50YZtPCE4=
golang.org/x/sync v0.19.0/go.mod h1:9KTHXmSnoGruLpwFjVSX0lNNA75CykiMECbovNTZqGI=
```

Create `bench/uptime-go/main_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/bench/farm"
)

// fastGate is the gate's farm (200 targets, 10% down, 5% flaky) with
// latencies short enough for a test.
var fastGate = farm.Config{N: 200, MinLatency: 5 * time.Millisecond, MaxLatency: 40 * time.Millisecond, Fail: 0.1, Flaky: 0.05, Seed: 1}

// setup serves a farm and writes its targets.txt into a new dir with an
// empty out/.
func setup(t *testing.T, c farm.Config) (*farm.Farm, *httptest.Server, string) {
	t.Helper()
	f, err := farm.New(c)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "targets.txt"), []byte(f.TargetList(srv.URL)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	return f, srv, dir
}

func readReport(t *testing.T, path string) []farm.Result {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := farm.ParseReport(data)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func TestReportsTheFarm(t *testing.T) {
	f, srv, dir := setup(t, fastGate)
	var out bytes.Buffer
	err := run(context.Background(), filepath.Join(dir, "targets.txt"), filepath.Join(dir, "out", "report.json"), &out)
	if err == nil || err.Error() != "more than 5% of the targets are down" {
		t.Fatalf("run: %v", err)
	}
	if out.String() != `{"down":20,"total":200}`+"\n" {
		t.Errorf("output %q", out.String())
	}
	if err := farm.Compare(readReport(t, filepath.Join(dir, "out", "report.json")), f.Expected(srv.URL)); err != nil {
		t.Fatal(err)
	}
	if s := f.Stats(); s.Peak < 1 || s.Peak > 20 {
		t.Errorf("peak in flight %d, want 1 to 20", s.Peak)
	}
}

func TestAtFivePercentSucceeds(t *testing.T) {
	c := fastGate
	c.Fail = 0.05
	_, _, dir := setup(t, c)
	if err := run(context.Background(), filepath.Join(dir, "targets.txt"), filepath.Join(dir, "out", "report.json"), &bytes.Buffer{}); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// The gate's correctness criterion: against the same farm, the Arv
// example and this program write the same report, apart from latency,
// and print the same summary.
func TestSameReportAsTheArvExample(t *testing.T) {
	f, srv, dir := setup(t, fastGate)
	var goOut bytes.Buffer
	run(context.Background(), filepath.Join(dir, "targets.txt"), filepath.Join(dir, "out", "report.json"), &goOut)
	goReport := readReport(t, filepath.Join(dir, "out", "report.json"))

	src, err := os.ReadFile("../../examples/uptime.arv")
	if err != nil {
		t.Fatal(err)
	}
	f.Reset() // flaky targets drop their next request again
	os.Remove(filepath.Join(dir, "out", "report.json"))
	t.Chdir(dir)
	var caps []runtime.Capability
	for _, g := range []string{"fs_read=targets.txt", "fs_write=out", "net=" + strings.TrimPrefix(srv.URL, "http://")} {
		grant, err := runtime.Parse(g, dir)
		if err != nil {
			t.Fatal(err)
		}
		caps = append(caps, grant)
	}
	var arvOut bytes.Buffer
	if _, err := arv.Run(string(src), arv.Options{Caps: caps, Out: &arvOut}); err == nil {
		t.Error("the Arv example succeeded with 20 of 200 down")
	}
	if err := farm.Compare(readReport(t, filepath.Join(dir, "out", "report.json")), goReport); err != nil {
		t.Fatal(err)
	}
	if arvOut.String() != goOut.String() {
		t.Errorf("Arv printed %q, Go %q", arvOut.String(), goOut.String())
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd <worktree>/bench/uptime-go && go test ./...`
Expected: FAIL, build error `undefined: run`. If instead Go tries to download `golang.org/x/sync`, check `ls ~/go/pkg/mod/cache/download/golang.org/x/sync/@v/v0.19.0.zip` and stop and report if it is missing.

- [ ] **Step 3: Implement**

Create `bench/uptime-go/main.go`:

```go
// Command uptime-go is the Go baseline of the Phase 5 gate, written as
// for production: check every URL in targets.txt, at most 20 at a time,
// each within 2 s and with up to 2 retries on transport errors; write
// out/report.json; exit 1 if more than 5% of the targets are down.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	maxInFlight  = 20
	checkTimeout = 2 * time.Second
	retries      = 2
	backoff      = 100 * time.Millisecond
)

type result struct {
	URL       string `json:"url"`
	Status    int    `json:"status"` // 0 when no response came
	Up        bool   `json:"up"`
	LatencyMS int64  `json:"latency_ms"` // of the last attempt
}

func main() {
	if err := run(context.Background(), "targets.txt", "out/report.json", os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, targetsPath, reportPath string, stdout io.Writer) error {
	data, err := os.ReadFile(targetsPath)
	if err != nil {
		return err
	}
	urls := strings.Fields(string(data))
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = maxInFlight
	client := &http.Client{Transport: transport}

	results := make([]result, len(urls))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxInFlight) // the semaphore: g.Go blocks while 20 checks run
	for i, url := range urls {
		g.Go(func() error {
			results[i] = check(ctx, client, url)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	report, err := json.Marshal(results)
	if err != nil {
		return err
	}
	if err := os.WriteFile(reportPath, report, 0o644); err != nil {
		return err
	}
	down := 0
	for _, r := range results {
		if !r.Up {
			down++
		}
	}
	summary, err := json.Marshal(map[string]int{"down": down, "total": len(results)})
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, string(summary))
	if down*20 > len(results) {
		return errors.New("more than 5% of the targets are down")
	}
	return nil
}

// check fetches url, retrying transport errors (not HTTP error statuses)
// up to retries times with jittered exponential backoff, all within
// checkTimeout.
func check(ctx context.Context, client *http.Client, url string) result {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	r := result{URL: url}
	for attempt := 0; ; attempt++ {
		start := time.Now()
		status, err := get(ctx, client, url)
		r.LatencyMS = time.Since(start).Milliseconds()
		if err == nil {
			r.Status, r.Up = status, status >= 200 && status < 300
			return r
		}
		if attempt == retries {
			return r
		}
		wait := backoff << attempt
		wait += rand.N(wait/10 + 1)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return r
		}
	}
}

func get(ctx context.Context, client *http.Client, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return 0, err
	}
	return resp.StatusCode, nil
}
```

In `Makefile`, replace the first line:

```make
.PHONY: build test e2e
```

with:

```make
.PHONY: build test e2e bench-test
```

and append to the end of the file:

```make

# The Go baseline of the Phase 5 gate is its own module, outside ./...
bench-test:
	cd bench/uptime-go && go test -race ./...
```

(The recipe line starts with a tab.)

- [ ] **Step 4: Run to verify it passes**

Run: `cd <worktree>/bench/uptime-go && gofmt -l . && go vet ./... && go test -race ./...`, then from the root `make bench-test`, `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere; `go.mod` and `go.sum` unchanged (`git diff --stat` shows nothing for them after the tests).

- [ ] **Step 5: Commit**

```bash
git add bench/uptime-go/go.mod bench/uptime-go/go.sum bench/uptime-go/main.go bench/uptime-go/main_test.go Makefile
git commit -m "feat(bench): idiomatic Go baseline, uptime-go

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
