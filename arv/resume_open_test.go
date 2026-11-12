package arv

import (
	"bytes"
	"testing"
)

// When a resumed scope opened is bounded by the records under the tasks
// its opener spawns after opening it, not only by the opener's own
// records: those tasks started after it opened. Here b has no records,
// and its clock lags the recorded run's by the 800 ms its spawn waited
// for s's slot, which the resume skips (the spawn has records under it,
// so it takes its slot at once). Opened on b's clock, the scope's
// deadline would fall at about 1000 ms on the recorded clock, and p,
// live from its random at 800 ms, would reach it 200 ms later, cutting
// short the 300 ms sleep that ended well in time in the recorded run.
func TestResumeOfAScopeWhoseOpenerSkippedAWait(t *testing.T) {
	src := `let s = fn() { sleep(800); "s" };
let p = fn() { random(10); sleep(300); "p" };
let b = fn() { puts(match scope (timeout_ms: 1000) { let h = spawn p(); await h } { ok(v) => v, err(e) => e["message"] }); "b" };
puts(match scope (tasks: 1) { let x = spawn s(); let y = spawn b(); [await x, await y] } { ok(v) => v, err(e) => e["message"] })`
	recorded, log, rerr := recordRun(t, src, Options{})
	if rerr != nil || recorded != "p\n[s, b]\n" {
		t.Fatalf("recording: %v, %q", rerr, recorded)
	}
	resumeMatches(t, "skipped wait", src, cutAfter(t, log, `"task":"root/1/0","seq":1,"kind":"random"`), nil, recorded, rerr)
}

// A replaying opener opened its scope before its next record began, and
// that is the bound the resume uses, not its last record: here the
// opener spun for about a second between its random and the scope, and
// c's 1000 ms sleep ended in time, 500 ms before the deadline. Opened at
// the random, the deadline would come about a second early, before c's
// sleep ends.
func TestResumeOpensAScopeAtTheOpenersNextRecord(t *testing.T) {
	src := spinSrc() + `let c = fn() { sleep(1000); "c" };
random(10); spin(); spin(); spin(); spin();
puts(match scope (timeout_ms: 1500) { let h = spawn c(); puts("opened"); await h } { ok(v) => v, err(e) => e["message"] })`
	recorded, log, rerr := recordRun(t, src, Options{})
	if rerr != nil || recorded != "opened\nc\n" {
		t.Fatalf("recording: %v, %q", rerr, recorded)
	}
	crashed := cutAfter(t, log, `"kind":"puts","req":["opened"]`)
	if bytes.Contains(crashed, []byte(`"task":"root/0","seq"`)) {
		t.Fatalf("premise: c's sleep is in the cut\n%s", crashed)
	}
	resumeMatches(t, "next record", src, crashed, nil, recorded, rerr)
}

// No goroutine outlives a resume or a replay: not a deadline's timer
// (one armed for a minute, one that fires), not a wait for the log's
// frontier before a logged deadline fires, not a quiet check's watcher.
func TestResumeAndReplayOfDeadlinesLeaveNoGoroutines(t *testing.T) {
	src := `let c = fn(n) { sleep(n); "c" };
let w = fn() { sleep(60000) };
puts(match scope (timeout_ms: 60000) { let a = spawn c(10); let b = spawn c(200); [await a, await b] } { ok(v) => v, err(e) => e["message"] });
puts(match scope (timeout_ms: 300) { spawn w(); spawn w(); "ok" } { ok(v) => v, err(e) => e["message"] })`
	recorded, log, rerr := recordRun(t, src, Options{})
	if rerr != nil || recorded != "[c, c]\nbudget exceeded: timeout (limit 300 ms)\n" {
		t.Fatalf("recording: %v, %q", rerr, recorded)
	}
	crashed := cutAfter(t, log, `"task":"root/0","seq":1,"kind":"sleep"`)
	before := settle(0)
	for i := 0; i < 5; i++ {
		resumeMatches(t, "leaks", src, crashed, nil, recorded, rerr)
		if replayed, err := replayRun(t, src, log); err != nil || replayed != recorded {
			t.Fatalf("replay: %v, %q", err, replayed)
		}
	}
	if after := settle(before); after > before {
		t.Fatalf("%d goroutines before, %d after", before, after)
	}
}
