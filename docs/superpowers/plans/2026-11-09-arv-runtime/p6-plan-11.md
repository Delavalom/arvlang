# Phase 6, task p6.11: Acceptance: resume, property tests, no live effect, no leak

> **For agentic workers:** this file is one task, p6.11. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p6.1 to p6.10 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`. `arv/runtime` stays stdlib-only.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `13-phase6-durable-replay.md`. Deviations from it, and why: `p6-plan-1.md`.

### Task p6.11: Acceptance: resume, property tests, no live effect, no leak

The tests the design's acceptance table asks for, in process. They need no new code: if one fails, stop and report it with its output.
- `TestResumeAfterACrash`: a run cut off after 3 of 5 requests (a writer cancels the run, as a crash would) resumes; the server sees exactly the other 2, the output equals the uninterrupted run's, a resume without the grants is refused, and the resumed log replays as a whole.
- `TestReplayProperty`: 200 random programs (spawn trees, nested and `collect` scopes, `puts`, `sleep`, `http_get`, `random`, at most one failure) record and replay identically, after the server has closed. `TestResumeProperty`: 60 programs, each crashed at a random record and resumed; the sorted output equals an uninterrupted run's, and the resumed log replays.
- `TestReplayRunsNoEffectLive`: against an empty log, every effect diverges; no request, file or output happens. `TestNoGoroutineOutlivesALoggedRun`: settle loop after record, replay, divergence and resume.

**Files:**
- Create (test): `arv/replay_gate_test.go`
- Create (test): `arv/replay_prop_test.go`
- Create (test): `arv/replay_resume_test.go`

- [ ] **Step 1: Write the acceptance tests**

Create `arv/replay_gate_test.go`:

```go
package arv

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// I7: replayed against a log that has none of them, every effect stops
// the run with a divergence and none happens: no request, no file, no
// output, no wait. Each would have been allowed by the header's grants.
func TestReplayRunsNoEffectLive(t *testing.T) {
	srv, hits := counted(t)
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.txt"), filepath.Join(dir, "out.txt")
	if err := os.WriteFile(in, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARV_P6_VAR", "secret")
	caps := []runtime.Capability{loopback[0], {Kind: runtime.FSRead, Scope: dir}, {Kind: runtime.FSWrite, Scope: dir}, {Kind: runtime.Env, Scope: "ARV_P6_VAR"}}
	_, log, err := recordRun(t, "1", Options{Caps: caps})
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []string{
		fmt.Sprintf("http_get(%q)", srv.URL+"/x"), fmt.Sprintf("read_file(%q)", in), fmt.Sprintf("write_file(%q, %q)", out, "x"),
		`getenv("ARV_P6_VAR")`, "sleep(60000)", "clock_ms()", "now()", "random(5)", `puts("x")`,
		fmt.Sprintf(`let f = fn() { 1 }; spawn f() with [fs_read(%q)]`, dir),
	} {
		start := time.Now()
		printed, err := replayRun(t, call+"; 1", log)
		name := call[:strings.IndexByte(call, '(')]
		if strings.HasPrefix(call, "let f") {
			name = "spawn"
		}
		if err == nil || !strings.Contains(err.Error(), "the program asked for "+name+"(") {
			t.Errorf("%s: err = %v", call, err)
		}
		if printed != "" || time.Since(start) > 30*time.Second {
			t.Errorf("%s: printed %q, took %s", call, printed, time.Since(start))
		}
	}
	if _, err := os.Stat(out); hits.Load() != 0 || !os.IsNotExist(err) {
		t.Fatalf("%d requests; %s: %v", hits.Load(), out, err)
	}
}

// Leak rule: no goroutine outlives a recorded, replayed, resumed or
// diverging run, including tasks a replay holds until they are cancelled.
func TestNoGoroutineOutlivesALoggedRun(t *testing.T) {
	srv, _ := counted(t)
	src := fmt.Sprintf(fanout, srv.URL, 1)
	failFast := `let boom = fn() { sleep(50); error("boom") };
let busy = fn() { puts("b1"); sleep(60000) };
scope { spawn boom(); spawn busy(); sleep(60000) }`
	recordRun(t, src, Options{Caps: loopback}) // warm up net/http
	before := settle(0)
	_, log, _ := recordRun(t, src, Options{Caps: loopback})
	replayRun(t, src, log)
	replayRun(t, strings.Replace(src, `get("/b")`, `get("/c")`, 1), log)
	_, ffLog, _ := recordRun(t, failFast, Options{})
	replayRun(t, failFast, ffLog)
	replayRun(t, strings.Replace(failFast, `puts("b1")`, `puts("b2")`, 1), ffLog)
	l, _ := runtime.ReadLog(bytes.NewReader(log[:bytes.LastIndexByte(log[:len(log)-1], '\n')+1]))
	Run(src, Options{Caps: loopback, Resume: l, Record: &bytes.Buffer{}, Out: &bytes.Buffer{}})
	if after := settle(before); after > before {
		buf := make([]byte, 1<<20)
		t.Fatalf("%d goroutines before, %d after:\n%s", before, after, buf[:goruntime.Stack(buf, true)])
	}
}
```

Create `arv/replay_prop_test.go`:

```go
package arv

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// progGen writes small random Arv programs: trees of spawned tasks that
// print, sleep, fetch from a local server, draw random numbers, open
// nested (sometimes collect) scopes, and at most once fail.
type progGen struct {
	r       *rand.Rand
	url     string
	fns     []string
	n       int
	failure bool // whether the program may fail once
	failed  bool
}

func (g *progGen) task(depth int) string {
	g.n++
	name := fmt.Sprintf("t%d", g.n)
	var b strings.Builder
	for i, n := 0, 1+g.r.Intn(5); i < n; i++ {
		switch k := g.r.Intn(8); {
		case k == 0:
			fmt.Fprintf(&b, "puts(%q);\n", fmt.Sprintf("%s.%d", name, i))
		case k == 1:
			fmt.Fprintf(&b, "sleep(%d);\n", g.r.Intn(4))
		case k == 2:
			fmt.Fprintf(&b, "puts(http_get(%q)[\"body\"]);\n", fmt.Sprintf("%s/%s/%d", g.url, name, i))
		case k == 3:
			fmt.Fprintf(&b, "puts(random(1000));\n")
		case k <= 5 && depth < 3:
			child := g.task(depth + 1)
			fmt.Fprintf(&b, "let h%d = spawn %s() with [net(\"127.0.0.1\")];\n", i, child)
			if g.r.Intn(2) == 0 {
				fmt.Fprintf(&b, "puts(await h%d);\n", i)
			}
		case k == 6 && depth < 3:
			a, c := g.task(depth+1), g.task(depth+1)
			opts := ""
			if g.r.Intn(3) == 0 {
				opts = `(supervise: "collect") `
			}
			fmt.Fprintf(&b, "puts(match scope %s{ let x = spawn %s() with [net(\"127.0.0.1\")]; let y = spawn %s() with [net(\"127.0.0.1\")]; [await x, await y] } { ok(v) => v, err(e) => e[\"message\"] });\n", opts, a, c)
		case k == 7 && g.failure && !g.failed && g.r.Intn(2) == 0:
			g.failed = true
			fmt.Fprintf(&b, "error(%q);\n", "boom in "+name)
		}
	}
	g.fns = append(g.fns, fmt.Sprintf("let %s = fn() {\n%s%q\n};\n", name, b.String(), name+" done"))
	return name
}

func (g *progGen) program() string {
	top := g.task(0)
	return strings.Join(g.fns, "") + fmt.Sprintf("puts(await spawn %s() with [net(\"127.0.0.1\")])\n", top)
}

// I7 property: every recorded run replays identically, output and result,
// with no request reaching the server. Each case has its own server,
// closed before the replay (Close waits for the requests a cancelled task
// left in flight): a live request would fail and change the output.
func TestReplayProperty(t *testing.T) {
	cases := 200
	if testing.Short() {
		cases = 40
	}
	for i := 0; i < cases; i++ {
		srv, hits := counted(t)
		g := &progGen{r: rand.New(rand.NewSource(int64(i))), url: srv.URL, failure: i%2 == 1}
		src := g.program()
		recorded, log, rerr := recordRun(t, src, Options{Caps: loopback})
		srv.Close()
		before := hits.Load()
		replayed, err := replayRun(t, src, log)
		if fmt.Sprint(err) != fmt.Sprint(rerr) || replayed != recorded || hits.Load() != before {
			t.Fatalf("case %d: replay %v (recorded %v), %d requests\nprinted:\n%s\nrecorded:\n%s\nprogram:\n%s",
				i, err, rerr, hits.Load()-before, replayed, recorded, src)
		}
	}
}

// Differential property: a run cut off after a random number of records
// and resumed ends as an uninterrupted run does, prints the same lines
// (their order across tasks is the scheduler's), and leaves a log that
// replays identically.
func TestResumeProperty(t *testing.T) {
	srv, _ := counted(t)
	cases := 60
	if testing.Short() {
		cases = 15
	}
	for i := 0; i < cases; i++ {
		g := &progGen{r: rand.New(rand.NewSource(int64(1000 + i))), url: srv.URL}
		src := g.program()
		whole, full, err := recordRun(t, src, Options{Caps: loopback})
		if err != nil {
			t.Fatalf("case %d: %v\n%s", i, err, src)
		}
		n := bytes.Count(full, []byte("\n")) - 2 // records, header and end excluded
		ctx, cancel := context.WithCancel(context.Background())
		crash := &crashAfter{kind: "", n: 1 + g.r.Intn(max(n, 1)), cancel: cancel}
		Run(src, Options{Ctx: ctx, Caps: loopback, Record: crash, Out: &bytes.Buffer{}})
		cancel()
		l, err := runtime.ReadLog(bytes.NewReader(crash.buf.Bytes()))
		if err != nil {
			t.Fatalf("case %d: crashed log: %v", i, err)
		}
		var out bytes.Buffer
		log := bytes.NewBuffer(crash.buf.Bytes()[:l.Size])
		if _, err := Run(src, Options{Caps: loopback, Resume: l, Record: log, Out: &out}); err != nil {
			t.Fatalf("case %d: resume after %d records: %v\n%s", i, crash.n, err, src)
		}
		if sorted(out.String()) != sorted(whole) {
			t.Fatalf("case %d: resumed run printed\n%s\nuninterrupted\n%s", i, out.String(), whole)
		}
		if replayed, err := replayRun(t, src, log.Bytes()); err != nil || replayed != out.String() {
			t.Fatalf("case %d: replay of the resumed log: %v\n%s\nwant\n%s", i, err, replayed, out.String())
		}
	}
}

// sorted is s's lines, sorted, except random numbers, which differ
// between two live runs.
func sorted(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" && strings.Trim(l, "0123456789") == "" {
			lines[i] = "N"
		}
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n")
}
```

Create `arv/replay_resume_test.go`:

```go
package arv

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// crashAfter is a log writer that stops the run (as a crash would) once
// it has written n records whose kind starts with kind.
type crashAfter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	kind   string
	n      int
	cancel context.CancelFunc
}

func (c *crashAfter) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf.Write(p)
	if bytes.Contains(p, []byte(`"kind":"`+c.kind)) && !bytes.Contains(p, []byte(`"header"`)) {
		if c.n--; c.n == 0 {
			c.cancel()
		}
	}
	return len(p), nil
}

// A run cut off after 3 of its 5 requests resumes: the 3 are answered from
// the log, the server sees only the other 2, and the output is the
// uninterrupted run's. The resumed log then replays as a whole.
func TestResumeAfterACrash(t *testing.T) {
	srv, hits := counted(t)
	src := fmt.Sprintf(`for p in ["/1", "/2", "/3", "/4", "/5"] { puts(http_get("%s" + p)["body"]) }; "done"`, srv.URL)
	whole, _, err := recordRun(t, src, Options{Caps: loopback})
	if err != nil || hits.Load() != 5 {
		t.Fatalf("uninterrupted run: %v, %d requests", err, hits.Load())
	}
	hits.Store(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	crash := &crashAfter{kind: "http_get", n: 3, cancel: cancel}
	if _, err := Run(src, Options{Ctx: ctx, Caps: loopback, Record: crash, Out: &bytes.Buffer{}}); err == nil {
		t.Fatal("the crashed run succeeded")
	}
	if hits.Load() != 3 {
		t.Fatalf("crashed after %d requests, want 3", hits.Load())
	}
	l, err := runtime.ReadLog(bytes.NewReader(crash.buf.Bytes()))
	if err != nil || l.End != nil {
		t.Fatalf("crashed log: %v, end %v", err, l.End)
	}
	hits.Store(0)
	var out bytes.Buffer
	log := bytes.NewBuffer(crash.buf.Bytes()[:l.Size])
	if _, err := Run(src, Options{Caps: loopback, Resume: l, Record: log, Out: &out}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if hits.Load() != 2 || out.String() != whole {
		t.Fatalf("resume made %d requests, want 2; printed %q, want %q", hits.Load(), out.String(), whole)
	}
	if _, err := Run(src, Options{Resume: l, Record: &bytes.Buffer{}}); err == nil || !strings.Contains(err.Error(), "grants the run was recorded with") {
		t.Fatalf("resume without the grants: %v", err)
	}
	hits.Store(0)
	if replayed, err := replayRun(t, src, log.Bytes()); err != nil || replayed != whole || hits.Load() != 0 {
		t.Fatalf("replay of the resumed log: %v, %q, %d requests", err, replayed, hits.Load())
	}
}
```

- [ ] **Step 2: Run them**

Run: `cd <worktree> && go test -race ./arv/ -run 'TestResumeAfterACrash|TestReplayProperty|TestResumeProperty|TestReplayRunsNoEffectLive|TestNoGoroutineOutlivesALoggedRun'`

Expected: `ok`. Run them a few more times under load (`-count=3`): they must stay green.

- [ ] **Step 3: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/ -run 'TestResumeAfterACrash|TestReplayProperty|TestResumeProperty|TestReplayRunsNoEffectLive|TestNoGoroutineOutlivesALoggedRun'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 4: Commit**

```bash
cd <worktree> && git add arv/replay_gate_test.go arv/replay_prop_test.go arv/replay_resume_test.go && git commit -F - <<'MSG'
test(arv): resume after a crash, record/replay property tests, no live effect, no leak

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
