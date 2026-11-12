package arv

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// Review round 3 (Important): an argument, or a run's result, that shares
// its parts (n levels of [a, a]) encoded to 2^n paths, uncharged and not
// cancellable, under --record: 1.56 GB and a 298 MB log at n = 22. The
// codec now writes a shared value once, so both stay small at n = 60.
func TestSharedValuesLogSmall(t *testing.T) {
	src := `let d = fn(a, n) { if (n == 0) { a } else { d([a, a], n - 1) } };
let a = d(["` + strings.Repeat("x", 64) + `"], 60);
puts(match scope { getenv(a) } { ok(v) => v, err(e) => e["kind"] });
a`
	start := time.Now()
	out, log, err := recordRun(t, src, Options{Limits: runtime.Limits{AllocBytes: 16 << 20}})
	if err != nil || out != "runtime\n" {
		t.Fatalf("record: %q, %v", out, err)
	}
	if len(log) > 64<<10 || time.Since(start) > 30*time.Second {
		t.Fatalf("a 60-level shared value made a %d-byte log in %v", len(log), time.Since(start))
	}
	if got, rerr := replayRun(t, src, log); rerr != nil || got != out {
		t.Fatalf("replay: %v, %q; want %q", rerr, got, out)
	}
}

// Review round 3 (Minor): recording no longer changes what puts prints.
// When rendering a later argument fails, the lines before it are printed,
// with or without a log.
func TestLoggedPutsPrintsTheLinesBeforeAFailure(t *testing.T) {
	src := `let dbl = fn(s, n) { if (n == 0) { s } else { dbl(s + s, n - 1) } };
let s = dbl("xxxxxxxxxxxxxxxx", 15);
puts(len(s));
let a = [s, s, s, s, s, s, s, s];
scope (alloc_mb: 3) { puts("first line", a) }`
	var plain bytes.Buffer
	_, perr := Run(src, Options{Out: &plain})
	recorded, log, rerr := recordRun(t, src, Options{})
	if perr == nil || rerr == nil || plain.String() != "524288\nfirst line\n" || recorded != plain.String() || perr.Error() != rerr.Error() {
		t.Fatalf("unlogged %q, %v; recorded %q, %v", plain.String(), perr, recorded, rerr)
	}
	if got, err := replayRun(t, src, log); err == nil || got != recorded || err.Error() != rerr.Error() {
		t.Fatalf("replay %q, %v", got, err)
	}
}

// Review round 3 (Minor): a resume of a finished log replays it: the same
// output, nothing appended.
func TestResumeOfAFinishedLogReplaysIt(t *testing.T) {
	src := `puts(now()); puts(random(100000)); 7`
	recorded, log, err := recordRun(t, src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	l, err := runtime.ReadLog(bytes.NewReader(log))
	if err != nil || l.End == nil {
		t.Fatalf("ReadLog: %v", err)
	}
	var out, appended bytes.Buffer
	res, err := Run(src, Options{Resume: l, Record: &appended, Out: &out})
	if err != nil || out.String() != recorded || res.Inspect() != "7" || appended.Len() != 0 {
		t.Fatalf("resume: %v, %v, printed %q (want %q), appended %d bytes", res, err, out.String(), recorded, appended.Len())
	}
}

// Review round 3 (Minor): a replay that its host cancels while a task
// waits for a logged cancellation's cause reports the host's cancellation,
// not a divergence the replay did not make.
func TestHostCancelledReplayIsNotADivergence(t *testing.T) {
	src := spinSrc() + `let r = match scope {
  let w = spawn fn() { sleep(60000); puts("never") }();
  let f = spawn fn() { spin(); spin(); error("boom") }();
  0
} { ok(v) => v, err(e) => e["message"] };
puts(r)`
	recorded, log, err := recordRun(t, src, Options{})
	if err != nil || recorded != "boom\n" {
		t.Fatalf("record: %q, %v", recorded, err)
	}
	l, _ := runtime.ReadLog(bytes.NewReader(log))
	// The replay's w waits for f's failure, and f spins: cancel it then.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = Run(src, Options{Replay: l, Ctx: ctx})
	var ae *Error
	if !errorsAs(err, &ae) || ae.Kind == runtime.KindReplay {
		t.Fatalf("err = %v, want the host's cancellation, not a divergence", err)
	}
}
