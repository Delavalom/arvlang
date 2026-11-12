package arv

import (
	"bytes"
	"testing"
)

// A live task is behind the log while any spawn it has still to make has
// records, not only its next one. Here q spawns first, which the log has
// no record of (its sleep was cut short after the crash), and then
// second, whose puts the log has. b's clock lags the recorded run's: the
// resume skips the 1500 ms that b's spawn waited for s's slot. Before
// scopes opened no sooner than the tasks spawned in them started
// (TestResumeOfAScopeWhoseOpenerSkippedAWait), b's deadline fell at about
// 1000 ms on the recorded clock, and p, live from its random at 1500 ms,
// fired it at once. q, still in spin(), must not see such a deadline, or
// it stops short of second, whose record is then never used (found by
// TestTimedProgramsReplayAndResume, seed 20020: "replay diverged at task
// root/0/1/1/1 ... which this run never reached"). The unit test
// TestBehindUntilTheLastSpawnWithRecords holds the rule itself.
//
// The times here are twice those the bug was found with (b's timeout
// 2000 ms, s's sleep 3000 ms), which keeps the ratios that matter: spin()
// takes about 300 ms when calibrated, but a loaded machine running the
// suite in parallel stretched it past a 1000 ms timeout, and the recording
// lost second's puts (review round 4). A recording that still misses it
// is made again, up to three times.
func TestResumedTaskBehindALaterSpawnDoesNotSeeTheDeadlineYet(t *testing.T) {
	src := spinSrc() + `let s = fn() { sleep(3000); "s" };
let p = fn() { random(10); sleep(60000) };
let first = fn() { sleep(60000) };
let second = fn() { puts("second"); sleep(60000) };
let q = fn() { spin(); let x = spawn first(); let y = spawn second(); await y };
let b = fn() { puts(match scope (timeout_ms: 2000) { spawn p(); let h = spawn q(); await h } { ok(v) => v, err(e) => e["message"] }); "b" };
puts(match scope (tasks: 1) { let x = spawn s(); let y = spawn b(); [await x, await y] } { ok(v) => v, err(e) => e["message"] })`
	var recorded string
	var log []byte
	var rerr error
	for try := 1; ; try++ {
		recorded, log, rerr = recordRun(t, src, Options{})
		if rerr == nil && recorded == "second\nbudget exceeded: timeout (limit 2000 ms)\n[s, b]\n" {
			break
		}
		if try == 3 {
			t.Fatalf("recording: %v, %q", rerr, recorded)
		}
		t.Logf("recording %d missed its premise under load: %v, %q", try, rerr, recorded)
	}
	crashed := cutAfter(t, log, `"task":"root/1/1/1","seq":1,"kind":"puts"`)
	if bytes.Contains(crashed, []byte(`"task":"root/1/1/0","seq"`)) || bytes.Contains(crashed, []byte(`"by"`)) {
		t.Fatalf("premise: the cut has first's record or a deadline's\n%s", crashed)
	}
	resumeMatches(t, "behind a later spawn", src, crashed, nil, recorded, rerr)
}
