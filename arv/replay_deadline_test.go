package arv

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// A replay slower than its recording reaches the same deadlines: no
// timer runs in a replay, the log alone says whether a deadline fired
// (review round 2, Critical). The replay is made slow by its output, a
// writer that takes 300 ms a line, so three lines inside a 500 ms
// deadline outlast it, where the recording's took no time.
func TestSlowReplayKeepsItsDeadlines(t *testing.T) {
	for name, tt := range map[string]struct {
		src    string
		limits runtime.Limits
	}{
		"scope": {`puts(match scope (timeout_ms: 500) { puts("a"); puts("b"); puts("c"); "ok" } { ok(v) => v, err(e) => e["message"] })`, runtime.Limits{}},
		"run":   {`let d = fn() { puts("d") }; puts("a"); puts("b"); puts("c"); scope { spawn d() }; puts("done")`, runtime.Limits{Timeout: 500 * time.Millisecond}},
	} {
		recorded, log, rerr := recordRun(t, tt.src, Options{Limits: tt.limits})
		if rerr != nil || !strings.HasPrefix(recorded, "a\nb\nc\n") {
			t.Fatalf("%s: recording: %v, %q", name, rerr, recorded)
		}
		l, err := runtime.ReadLog(bytes.NewReader(log))
		if err != nil {
			t.Fatal(err)
		}
		out := &slowOut{d: 300 * time.Millisecond}
		_, err = Run(tt.src, Options{Replay: l, Out: out})
		if err != nil || out.buf.String() != recorded {
			t.Fatalf("%s: slow replay: %v, %q; recorded %q", name, err, out.buf.String(), recorded)
		}
	}
}

// A replay of a run whose deadline fired takes the firing from the log:
// it is as fast as the replay, not as slow as the deadline. A child that
// saw the deadline names the scope (By), and the replay fires it there.
func TestReplayFiresALoggedDeadline(t *testing.T) {
	src := `let w = fn() { sleep(60000) };
puts(match scope (timeout_ms: 1500) { spawn w(); puts("in"); "ok" } { ok(v) => v, err(e) => e["message"] })`
	recorded, log, rerr := recordRun(t, src, Options{})
	if rerr != nil || recorded != "in\nbudget exceeded: timeout (limit 1500 ms)\n" || !bytes.Contains(log, []byte(`"by":"root:1"`)) {
		t.Fatalf("recording: %v, %q\n%s", rerr, recorded, log)
	}
	start := time.Now()
	replayed, err := replayRun(t, src, log)
	if err != nil || replayed != recorded {
		t.Fatalf("replay: %v, %q", err, replayed)
	}
	if took := time.Since(start); took >= 1500*time.Millisecond {
		t.Errorf("the replay took %s: it waited for the deadline", took)
	}
}

// A log that names a deadline its task is not inside is refused as a
// divergence, not waited on.
func TestReplayRefusesADeadlineOutsideTheTask(t *testing.T) {
	src := `let w = fn() { sleep(60000) };
puts(match scope (timeout_ms: 300) { spawn w(); "ok" } { ok(v) => v, err(e) => e["message"] })`
	_, log, err := recordRun(t, src, Options{})
	if err != nil || !bytes.Contains(log, []byte(`"by":"root:1"`)) {
		t.Fatalf("recording: %v\n%s", err, log)
	}
	forged := forge(t, log, func(_ int, body []byte) []byte {
		return bytes.ReplaceAll(body, []byte(`"by":"root:1"`), []byte(`"by":"root/0:1"`))
	})
	err = replayForged(t, src, forged, Options{})
	var ae *Error
	if !errorsAs(err, &ae) || ae.Kind != runtime.KindReplay || !strings.Contains(err.Error(), "root/0:1") {
		t.Fatalf("replay of a forged deadline: %v", err)
	}
}
