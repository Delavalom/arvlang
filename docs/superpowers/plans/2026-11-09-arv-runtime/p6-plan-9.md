# Phase 6, task p6.9: arv.Run records, replays and resumes

> **For agentic workers:** this file is one task, p6.9. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p6.1 to p6.8 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`. `arv/runtime` stays stdlib-only.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `13-phase6-durable-replay.md`. Deviations from it, and why: `p6-plan-1.md`.

### Task p6.9: arv.Run records, replays and resumes

`arv.Options` gains `Record` (and `RecordSync`), `Replay` and `Resume`. `logSetup` (`arv/logged.go`) checks them:
- a replay takes its grants and limits from the header, so `Caps` and `Limits` must be empty; the grants are installed through `runtime.Literal`, which reads no file, and must already be canonical;
- a resume needs `Record` (the same file, positioned at `Resume.Size`) and exactly the recorded grants, and takes its limits from the header.

`startLog` writes the header: the script's SHA-256, the grants, the limits (the timeout rounded up to whole milliseconds), the working directory and the start time. `Run` starts the log, gives it to the root task and calls `Finish` with the encoded result; a log failure is an `*arv.Error` of kind `replay`.

**Files:**
- Modify: `arv/arv.go`
- Create: `arv/logged.go`
- Create (test): `arv/logged_test.go`
- Create (test): `arv/replay_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/logged_test.go`:

```go
package arv

import (
	"testing"
	"time"
)

// The header keeps whole milliseconds: a timeout rounds up, so a replay
// never times out sooner than the recording.
func TestCeilMS(t *testing.T) {
	for d, want := range map[time.Duration]int64{0: 0, time.Millisecond: 1, 1500 * time.Microsecond: 2, time.Nanosecond: 1} {
		if got := ceilMS(d); got != want {
			t.Errorf("ceilMS(%s) = %d, want %d", d, got, want)
		}
	}
}
```

Create `arv/replay_test.go`:

```go
package arv

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// counted is a test server that counts every request; /flaky fails the
// first two times it is asked.
func counted(t *testing.T) (*httptest.Server, *atomic.Int64) {
	var n, flaky atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if r.URL.Path == "/flaky" && flaky.Add(1) <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
		}
		fmt.Fprintf(w, "body of %s", r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

var loopback = []runtime.Capability{{Kind: runtime.Net, Scope: "127.0.0.1"}}

// recordRun runs src recording its log; it returns what it printed, the
// log and the run's error.
func recordRun(t *testing.T, src string, opts Options) (string, []byte, error) {
	t.Helper()
	var out, log bytes.Buffer
	opts.Out, opts.Record = &out, &log
	_, err := Run(src, opts)
	return out.String(), log.Bytes(), err
}

// replayRun replays log for src; it returns what it printed and the error.
func replayRun(t *testing.T, src string, log []byte) (string, error) {
	t.Helper()
	l, err := runtime.ReadLog(bytes.NewReader(log))
	if err != nil {
		t.Fatalf("ReadLog: %v", err)
	}
	var out bytes.Buffer
	_, err = Run(src, Options{Out: &out, Replay: l})
	return out.String(), err
}

const fanout = `let get = fn(p) { http_get("%[1]s" + p)["body"] };
let r = scope {
	let a = spawn get("/a") with [net("127.0.0.1")];
	let b = spawn get("/b") with [net("127.0.0.1")];
	sleep(%[2]d);
	puts(await a); puts(await b);
	puts(now()); puts(random(1000000)); puts(clock_ms() > -1);
	{"a": await a, "b": await b}
};
puts(r);
r`

// I7: a replay prints exactly what the recorded run printed and ends the
// same, without a single request, and without sleeping.
func TestReplayIsIdenticalAndOffline(t *testing.T) {
	srv, hits := counted(t)
	src := fmt.Sprintf(fanout, srv.URL, 2000)
	recorded, log, err := recordRun(t, src, Options{Caps: loopback})
	if err != nil || hits.Load() != 2 {
		t.Fatalf("recording: %v, %d requests", err, hits.Load())
	}
	start := time.Now()
	replayed, err := replayRun(t, src, log)
	if took := time.Since(start); took >= 2*time.Second {
		t.Errorf("replay took %s: it slept", took)
	}
	if err != nil || replayed != recorded || hits.Load() != 2 {
		t.Fatalf("replay: %v, %d requests\nprinted %q\nrecorded %q", err, hits.Load(), replayed, recorded)
	}
	if !strings.Contains(recorded, "{a: body of /a, b: body of /b}") {
		t.Fatalf("recorded %q", recorded)
	}
}

// --replay takes the grants from the header: spawn ... with works without
// any, and a replay given grants is refused.
func TestReplayUsesTheHeaderGrants(t *testing.T) {
	srv, _ := counted(t)
	src := fmt.Sprintf(fanout, srv.URL, 1)
	_, log, err := recordRun(t, src, Options{Caps: loopback})
	if err != nil {
		t.Fatal(err)
	}
	l, _ := runtime.ReadLog(bytes.NewReader(log))
	if g := l.Header.Grants; len(g) != 1 || g[0] != (runtime.LogGrant{Kind: runtime.Net, Scope: "127.0.0.1"}) {
		t.Fatalf("header grants = %+v", g)
	}
	if _, err := Run(src, Options{Replay: l, Caps: loopback}); err == nil || !strings.Contains(err.Error(), "takes its grants from the log") {
		t.Fatalf("replay with grants: %v", err)
	}
	if _, err := Run(src, Options{Replay: l, Limits: runtime.Limits{Steps: 5}}); err == nil || !strings.Contains(err.Error(), "limits from the log") {
		t.Fatalf("replay with limits: %v", err)
	}
}

// A changed script diverges, naming the task, the effect and both requests.
func TestReplayOfAnEditedScriptDiverges(t *testing.T) {
	srv, hits := counted(t)
	src := fmt.Sprintf(fanout, srv.URL, 1)
	_, log, err := recordRun(t, src, Options{Caps: loopback})
	if err != nil {
		t.Fatal(err)
	}
	_, err = replayRun(t, strings.Replace(src, `get("/a")`, `get("/zen")`, 1), log)
	var ae *Error
	want := fmt.Sprintf(`replay diverged at task root/0, effect 1: the log has http_get("%[1]s/a"), the program asked for http_get("%[1]s/zen")`, srv.URL)
	if err == nil || !strings.Contains(err.Error(), want) || !errorsAs(err, &ae) || ae.Kind != runtime.KindReplay {
		t.Fatalf("err = %v\nwant %s", err, want)
	}
	if hits.Load() != 2 {
		t.Fatalf("%d requests: the replay went live", hits.Load())
	}
}

func errorsAs(err error, ae **Error) bool {
	e, ok := err.(*Error)
	*ae = e
	return ok
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/ -run 'TestCeilMS|TestReplayIsIdenticalAndOffline|TestReplayUsesTheHeaderGrants|TestReplayOfAnEditedScriptDiverges'`

Expected: the package fails to build: `undefined: ceilMS`, `opts.Record undefined`, `unknown field Replay in struct literal of type Options`.

- [ ] **Step 3: Implement**

In `arv/arv.go`, replace:

```go
	Out    io.Writer            // where puts writes; defaults to io.Discard
	Limits runtime.Limits       // the run's budget; zero values mean no limit, except LiveTasks (runtime.DefaultLiveTasks); a negative one is an error
}

```

with:

```go
	Out    io.Writer            // where puts writes; defaults to io.Discard
	Limits runtime.Limits       // the run's budget; zero values mean no limit, except LiveTasks (runtime.DefaultLiveTasks); a negative one is an error

	// Record, unless nil, receives the run's effect log (JSON Lines, one
	// write per line; see runtime.LogRecord). RecordSync, unless nil, runs
	// after each line: an fsync, so a crash loses no logged effect.
	Record     io.Writer
	RecordSync func() error
	// Replay, unless nil, is a finished run's log: every effect is
	// answered from it and none runs live (I7). The log's header supplies
	// the grants and limits, so Caps and Limits must be empty.
	Replay *runtime.Log
	// Resume, unless nil, is the log of a run that did not finish: each
	// task is answered from it while it has records, then runs live,
	// appending to Record (which must start at Resume.Size). Caps must be
	// the grants the run was recorded with, and Limits empty: the header
	// supplies them.
	Resume *runtime.Log
}

```

In `arv/arv.go`, replace:

```go
		return nil, fmt.Errorf("arv: %w", err)
	}
	caps := make([]runtime.Capability, len(opts.Caps))
	for i, c := range opts.Caps {
		valid, err := runtime.New(c.Kind, c.Scope)
		if err != nil {
			return nil, fmt.Errorf("invalid capability %s: %w", c, err)
		}
		caps[i] = valid
	}
	if opts.Ctx == nil {
```

with:

```go
		return nil, fmt.Errorf("arv: %w", err)
	}
	logged, err := logSetup(&opts)
	if err != nil {
		return nil, err
	}
	var caps []runtime.Capability
	if opts.Replay != nil {
		caps = logged // the log's grants: canonical when recorded, not resolved again
	} else if caps, err = validCaps(opts.Caps); err != nil {
		return nil, err
	}
	if opts.Resume != nil && !sameGrants(caps, logged) {
		return nil, fmt.Errorf("arv: resume needs the grants the run was recorded with: %s", grantList(logged))
	}
	if opts.Ctx == nil {
```

In `arv/arv.go`, replace:

```go
		afterParse()
	}
	root := runtime.NewRoot(opts.Ctx, runtime.NewCapSet(caps...), opts.Out)
	result = evaluator.New(root).RunLimited(program, value.NewEnvironment(), opts.Limits)
	if e, ok := result.(*value.Error); ok {
		return result, &Error{Msg: e.Message, Pos: e.Pos, Kind: e.ErrKind()}
```

with:

```go
		afterParse()
	}
	l, err := startLog(src, opts, caps)
	if err != nil {
		return nil, err
	}
	ctx := opts.Ctx
	if l != nil {
		ctx = l.Start(ctx)
	}
	root := runtime.NewRoot(ctx, runtime.NewCapSet(caps...), opts.Out)
	root.SetLog(l)
	result = evaluator.New(root).RunLimited(program, value.NewEnvironment(), opts.Limits)
	if l != nil {
		if err := l.Finish(value.EncodeRequest([]value.Object{result})); err != nil {
			return result, &Error{Msg: err.Error(), Kind: runtime.ErrorKind(err, runtime.KindReplay)}
		}
	}
	if e, ok := result.(*value.Error); ok {
		return result, &Error{Msg: e.Message, Pos: e.Pos, Kind: e.ErrKind()}
```

Create `arv/logged.go`:

```go
package arv

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// ScriptSHA256 is the hash of src a log's header records, so a host can
// warn that the script changed since the run was recorded.
func ScriptSHA256(src string) string {
	sum := sha256.Sum256([]byte(src))
	return hex.EncodeToString(sum[:])
}

// logSetup checks opts for a logged run, and for a replay or resume puts
// the header's limits in opts.Limits and returns its grants.
func logSetup(opts *Options) ([]runtime.Capability, error) {
	log := opts.Replay
	switch {
	case opts.Replay != nil && opts.Resume != nil:
		return nil, errors.New("arv: replay or resume a run, not both")
	case opts.Replay != nil && opts.Record != nil:
		return nil, errors.New("arv: a replay records nothing")
	case opts.Resume != nil && opts.Record == nil:
		return nil, errors.New("arv: resume appends to the log: set Record")
	case opts.Resume != nil:
		log = opts.Resume
	}
	if log == nil {
		return nil, nil
	}
	if opts.Replay != nil && len(opts.Caps) > 0 {
		return nil, errors.New("arv: a replay takes its grants from the log; pass none")
	}
	if opts.Limits != (runtime.Limits{}) {
		return nil, errors.New("arv: a replayed or resumed run takes its limits from the log; pass none")
	}
	h := log.Header
	opts.Limits = runtime.Limits{Timeout: time.Duration(h.Limits.TimeoutMS) * time.Millisecond,
		Steps: h.Limits.Steps, LiveTasks: h.Limits.LiveTasks, AllocBytes: h.Limits.AllocBytes}
	if err := opts.Limits.Validate(); err != nil {
		return nil, fmt.Errorf("arv: the log's header: %w", err)
	}
	caps := make([]runtime.Capability, len(h.Grants))
	for i, g := range h.Grants {
		c, err := runtime.Literal(g.Kind, g.Scope)
		if err != nil || c.Scope != g.Scope {
			return nil, fmt.Errorf("arv: the log's header grants %s, which is not a canonical capability", runtime.Capability{Kind: g.Kind, Scope: g.Scope})
		}
		caps[i] = c
	}
	return caps, nil
}

// validCaps validates and canonicalises the host's grants (runtime.New).
func validCaps(given []runtime.Capability) ([]runtime.Capability, error) {
	caps := make([]runtime.Capability, len(given))
	for i, c := range given {
		valid, err := runtime.New(c.Kind, c.Scope)
		if err != nil {
			return nil, fmt.Errorf("invalid capability %s: %w", c, err)
		}
		caps[i] = valid
	}
	return caps, nil
}

func sameGrants(a, b []runtime.Capability) bool {
	key := func(cs []runtime.Capability) []string {
		s := make([]string, len(cs))
		for i, c := range cs {
			s[i] = c.String()
		}
		slices.Sort(s)
		return slices.Compact(s)
	}
	return slices.Equal(key(a), key(b))
}

func grantList(caps []runtime.Capability) string {
	if len(caps) == 0 {
		return "none"
	}
	s := make([]string, len(caps))
	for i, c := range caps {
		s[i] = c.String()
	}
	return strings.Join(s, ", ")
}

// startLog makes the run's effect log, or nil for a run that keeps none.
func startLog(src string, opts Options, caps []runtime.Capability) (*runtime.EffectLog, error) {
	switch {
	case opts.Replay != nil:
		return runtime.NewReplayer(opts.Replay, opts.Out)
	case opts.Resume != nil:
		return runtime.NewResumer(opts.Resume, opts.Record, opts.Out, opts.RecordSync), nil
	case opts.Record == nil:
		return nil, nil
	}
	grants := make([]runtime.LogGrant, len(caps))
	for i, c := range caps {
		grants[i] = runtime.LogGrant{Kind: c.Kind, Scope: c.Scope}
	}
	cwd, _ := os.Getwd()
	h := runtime.Header{ScriptSHA256: ScriptSHA256(src), Grants: grants, Cwd: cwd,
		Started: time.Now().UTC().Format(time.RFC3339),
		Limits: runtime.LogLimits{TimeoutMS: ceilMS(opts.Limits.Timeout), Steps: opts.Limits.Steps,
			LiveTasks: opts.Limits.LiveTasks, AllocBytes: opts.Limits.AllocBytes}}
	return runtime.NewRecorder(opts.Record, h, opts.Out, opts.RecordSync)
}

// ceilMS rounds d up to whole milliseconds, so a replay's timeout is never
// shorter than the recording's.
func ceilMS(d time.Duration) int64 {
	ms := d.Milliseconds()
	if time.Duration(ms)*time.Millisecond < d {
		ms++
	}
	return ms
}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/ -run 'TestCeilMS|TestReplayIsIdenticalAndOffline|TestReplayUsesTheHeaderGrants|TestReplayOfAnEditedScriptDiverges'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/arv.go arv/logged.go arv/logged_test.go arv/replay_test.go && git commit -F - <<'MSG'
feat(arv): Options.Record, Replay and Resume; the log header

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
