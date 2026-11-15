# Phase 5, task p5.6: the `farm` command

> **For agentic workers:** this file is one task, p5.6. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p5.1 to p5.5 are committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3 and 4 merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`), and `bench/uptime-go`'s own `go vet` and `go test -race`. Design: `12-phase5-gate.md`. Deviations from it, and why: `p5-plan-1.md`. **Do not measure or write the gate verdict**: a separate step does that.

### Task p5.6: `bench/farm/cmd/farm`

The binary that `bench/run.sh` and `bench/escape/probe.sh` start. It listens on `-addr` (default `127.0.0.1:0`), which must be a loopback IP: `localhost`, `0.0.0.0` and any other address are refused, so the farm is local only. It prints its base URL on the first line of standard output, writes `targets.txt` (`-targets`) and the expected report (`-expect`), and serves until interrupted. `farm compare A B` exits 0 when two reports are the same apart from latency, 1 when they differ (printing the first difference), and 2 on a usage or parse error.

`main` only wires signals to `run`, which the test drives in process.

**Files:**
- Create: `bench/farm/cmd/farm/main.go`
- Create (test): `bench/farm/cmd/farm/main_test.go`

- [ ] **Step 1: Write the failing test**

Create `bench/farm/cmd/farm/main_test.go`:

```go
package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/bench/farm"
)

func TestServeWritesFilesAndServes(t *testing.T) {
	dir := t.TempDir()
	targets, expect := filepath.Join(dir, "targets.txt"), filepath.Join(dir, "expect.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pr, pw := io.Pipe()
	code := make(chan int, 1)
	go func() {
		code <- run(ctx, []string{"-n", "10", "-latency", "1ms", "-targets", targets, "-expect", expect}, pw, io.Discard)
		pw.Close()
	}()
	base, err := bufio.NewReader(pr).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	base = strings.TrimSpace(base)
	if !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Fatalf("base URL %q", base)
	}
	resp, err := http.Get(base + "/stats")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	list, _ := os.ReadFile(targets)
	if lines := strings.Fields(string(list)); len(lines) != 10 || lines[3] != base+"/t/3" {
		t.Errorf("targets.txt: %q", list)
	}
	data, _ := os.ReadFile(expect)
	if rs, err := farm.ParseReport(data); err != nil || len(rs) != 10 {
		t.Errorf("expect.json: %d entries, %v", len(rs), err)
	}
	cancel()
	if c := <-code; c != 0 {
		t.Errorf("exit %d after cancel, want 0", c)
	}
}

// The farm is local only.
func TestRefusesBadFlags(t *testing.T) {
	for _, args := range [][]string{
		{"-addr", "0.0.0.0:0"},
		{"-addr", "localhost:0"},
		{"-addr", "192.168.1.1:0"},
		{"-latency", "fast"},
		{"-n", "0"},
		{"-fail", "2"},
	} {
		var stderr bytes.Buffer
		if c := run(context.Background(), args, io.Discard, &stderr); c != 2 {
			t.Errorf("%v: exit %d, want 2 (%s)", args, c, stderr.String())
		}
	}
}

func TestCompare(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(text), 0o644)
		return p
	}
	a := write("a.json", `[{"url":"u","status":200,"up":true,"latency_ms":5}]`)
	b := write("b.json", `[{"latency_ms":80,"status":200,"up":true,"url":"u"}]`)
	c := write("c.json", `[{"url":"u","status":0,"up":false,"latency_ms":5}]`)
	for _, tc := range []struct {
		args []string
		code int
		out  string
	}{
		{[]string{"compare", a, b}, 0, "same: 1 entries\n"},
		{[]string{"compare", a, c}, 1, "reports differ at entry 0"},
		{[]string{"compare", a, filepath.Join(dir, "none.json")}, 2, ""},
		{[]string{"compare", a}, 2, ""},
	} {
		var out bytes.Buffer
		if got := run(context.Background(), tc.args, &out, io.Discard); got != tc.code || !strings.Contains(out.String(), tc.out) {
			t.Errorf("%v: exit %d, %q; want %d, %q", tc.args[1:], got, out.String(), tc.code, tc.out)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./bench/farm/cmd/farm/`
Expected: FAIL, build error `undefined: run`.

- [ ] **Step 3: Implement**

Create `bench/farm/cmd/farm/main.go`:

```go
// Command farm serves the Phase 5 gate's target farm on a loopback
// address, or compares two report.json files.
//
//	farm [-addr 127.0.0.1:0] [-n 200] [-latency 50ms-400ms] [-fail 0.1] [-flaky 0.05] [-seed 1] [-targets FILE] [-expect FILE]
//	farm compare A.json B.json
//
// Serving, it prints its base URL (http://127.0.0.1:PORT) on the first
// line of standard output, then serves until interrupted. -targets writes
// targets.txt and -expect the report a correct checker writes.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/delavalom/arvlang/bench/farm"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "compare" {
		return compare(args[1:], stdout, stderr)
	}
	fs := flag.NewFlagSet("farm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", "127.0.0.1:0", "loopback address to listen on")
	n := fs.Int("n", 200, "number of targets")
	latency := fs.String("latency", "50ms-400ms", "latency range MIN-MAX (or one duration)")
	fail := fs.Float64("fail", 0.1, "fraction of targets always down")
	flaky := fs.Float64("flaky", 0.05, "fraction of targets that drop their first request")
	seed := fs.Uint64("seed", 1, "seed for latencies and behaviours")
	targets := fs.String("targets", "", "write the target list here")
	expect := fs.String("expect", "", "write the expected report here")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	lo, hi, err := parseLatency(*latency)
	if err != nil {
		fmt.Fprintln(stderr, "farm:", err)
		return 2
	}
	f, err := farm.New(farm.Config{N: *n, MinLatency: lo, MaxLatency: hi, Fail: *fail, Flaky: *flaky, Seed: *seed})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := loopback(*addr); err != nil {
		fmt.Fprintln(stderr, "farm:", err)
		return 2
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(stderr, "farm:", err)
		return 1
	}
	base := "http://" + ln.Addr().String()
	if err := writeFiles(f, base, *targets, *expect); err != nil {
		ln.Close()
		fmt.Fprintln(stderr, "farm:", err)
		return 1
	}
	srv := &http.Server{Handler: f, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	fmt.Fprintln(stdout, base)
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(stderr, "farm:", err)
		return 1
	}
	return 0
}

// loopback refuses any address but a loopback IP: the farm is local only.
func loopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("-addr %s: want a loopback IP such as 127.0.0.1", addr)
	}
	return nil
}

// parseLatency reads "50ms-400ms", or one duration for a fixed latency.
func parseLatency(s string) (lo, hi time.Duration, err error) {
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		b = a
	}
	if lo, err = time.ParseDuration(a); err == nil {
		hi, err = time.ParseDuration(b)
	}
	if err != nil {
		return 0, 0, fmt.Errorf("-latency %q: want MIN-MAX such as 50ms-400ms", s)
	}
	return lo, hi, nil
}

func writeFiles(f *farm.Farm, base, targets, expect string) error {
	if targets != "" {
		if err := os.WriteFile(targets, []byte(f.TargetList(base)), 0o644); err != nil {
			return err
		}
	}
	if expect != "" {
		data, err := json.Marshal(f.Expected(base))
		if err != nil {
			return err
		}
		return os.WriteFile(expect, data, 0o644)
	}
	return nil
}

// compare exits 0 if two reports are the same apart from latency.
func compare(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "usage: farm compare A.json B.json")
		return 2
	}
	var rs [2][]farm.Result
	for i, name := range args {
		data, err := os.ReadFile(name)
		if err == nil {
			rs[i], err = farm.ParseReport(data)
		}
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			return 2
		}
	}
	if err := farm.Compare(rs[0], rs[1]); err != nil {
		fmt.Fprintln(stdout, err)
		return 1
	}
	fmt.Fprintf(stdout, "same: %d entries\n", len(rs[0]))
	return 0
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race ./bench/farm/...`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add bench/farm/cmd/farm/main.go bench/farm/cmd/farm/main_test.go
git commit -m "feat(bench): farm command

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
