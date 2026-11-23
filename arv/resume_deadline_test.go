package arv

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// Review round 2 of Phase 6: deadlines in a resume. A deadline firing is
// time, so it is part of the log (I7): a resume takes the deadlines the
// crashed run saw from the log, and runs the others on the recorded
// run's clock, each live task's reading going on from its last record
// (runtime/deadline.go).

// resumeMatches resumes crashed and checks that it prints what the
// uninterrupted run printed and ends with its error, and that the
// resumed log replays to the same.
func resumeMatches(t *testing.T, name, src string, crashed []byte, caps []runtime.Capability, recorded string, rerr error) {
	t.Helper()
	printed, resumed, err := resumeRun(t, src, crashed, caps)
	if fmt.Sprint(err) != fmt.Sprint(rerr) || printed != recorded {
		t.Fatalf("%s: resume: %v, printed %q; the uninterrupted run: %v, %q", name, err, printed, rerr, recorded)
	}
	if replayed, err := replayRun(t, src, resumed); fmt.Sprint(err) != fmt.Sprint(rerr) || replayed != recorded {
		t.Fatalf("%s: replay of the resumed log: %v, %q", name, err, replayed)
	}
}

// A scope closed long before its deadline, and the crash came after the
// deadline would have passed: the resume reopens it during the replay,
// where the recorded run's clock is far past it, and must not time it
// out (review round 2, Critical: "replay diverged ... budget exceeded").
func TestResumeOfAScopeClosedBeforeItsDeadline(t *testing.T) {
	src := `puts(match scope (timeout_ms: 500) { puts("in scope"); sleep(100); "ok" } { ok(v) => v, err(e) => e["message"] });
sleep(1000);
puts("end")`
	recorded, log, rerr := recordRun(t, src, Options{})
	if rerr != nil || recorded != "in scope\nok\nend\n" {
		t.Fatalf("recording: %v, %q", rerr, recorded)
	}
	resumeMatches(t, "closed", src, cutAfter(t, log, `"req":[1000]`), nil, recorded, rerr)
}

// An effect in flight at the crash runs again from its start, so the
// time the crashed run had spent on it must not count twice: e's sleep
// began at 0, and its task's clock goes on from there, not from d's
// later record. Uninterrupted, the run ends at 1.25 s of its 2 s; counted
// twice, at 2.25 s (review round 2).
func TestResumeCountsAnInFlightEffectOnce(t *testing.T) {
	fns := `let e = fn() { sleep(1250); puts("e done"); "e" };
let d = fn() { sleep(1000); puts("d done"); "d" };
`
	for name, tt := range map[string]struct {
		src    string
		limits runtime.Limits
	}{
		"run":   {fns + `scope { spawn e(); spawn d() }; puts("all done")`, runtime.Limits{Timeout: 2 * time.Second}},
		"scope": {fns + `puts(match scope (timeout_ms: 2000) { puts("start"); spawn e(); spawn d(); "ok" } { ok(v) => v, err(x) => x["message"] })`, runtime.Limits{}},
	} {
		recorded, log, rerr := recordRun(t, tt.src, Options{Limits: tt.limits})
		if rerr != nil || !strings.Contains(recorded, "e done") {
			t.Fatalf("%s: recording: %v, %q", name, rerr, recorded)
		}
		resumeMatches(t, name, tt.src, cutAfter(t, log, `"req":["d done"]`), nil, recorded, rerr)
	}
}

// A scope whose opener only spawns and awaits has no record of the
// opener's inside it. The crash came after w's first line; w's clock
// goes on from there, so the deadline is 500 ms into the resume, not a
// whole timeout_ms (review round 2).
func TestResumeOfAScopeWithoutTheOpenersRecords(t *testing.T) {
	src := `let w = fn() { sleep(1500); puts("first"); sleep(1500); puts("second"); 1 };
scope (timeout_ms: 2000) { let h = spawn w(); await h }`
	recorded, log, rerr := recordRun(t, src, Options{})
	if rerr == nil || !strings.Contains(rerr.Error(), "timeout (limit 2000 ms)") || recorded != "first\n" {
		t.Fatalf("recording: %v, %q", rerr, recorded)
	}
	start := time.Now()
	resumeMatches(t, "await", src, cutAfter(t, log, `"req":["first"]`), nil, recorded, rerr)
	if took := time.Since(start); took >= 1500*time.Millisecond {
		t.Errorf("the resume took %s: its deadline started again", took)
	}
}

// A task that finished before the crash, whose last stretch was pure work
// after its last record, is replayed to its end (its end is a record), and
// the live spawn after the root's logged sleep waits for it before looking
// at the cap. Running c's tail live while the replay raced ahead failed
// the spawn with "budget exceeded: tasks" (review round 2, Critical).
// The recording needs c to finish within the root's sleep. spin() is
// calibrated once, to about 300 ms, and the load of a whole-suite run
// (seen with the tree-walker) can stretch it past 2500 ms, so c is still
// live at the spawns: the recording then hits the cap, and is made again
// with a longer sleep. The resumes replay the sleep, so they do not
// depend on how long it was.
func TestResumeWaitsForFinishedTasksBeforeTheCap(t *testing.T) {
	var src, whole, sleepMs string
	var log []byte
	for _, ms := range []string{"2500", "10000", "40000"} {
		sleepMs = ms
		src = spinSrc() + `let c = fn() { sleep(10); spin(); 1 };
let d = fn(n) { puts(n); n };
scope { spawn c(); sleep(` + ms + `); let h1 = spawn d(1); let h2 = spawn d(2); await h1 + await h2 }`
		var rerr error
		whole, log, rerr = recordRun(t, src, Options{Limits: runtime.Limits{LiveTasks: 2}})
		if rerr != nil && strings.Contains(rerr.Error(), "budget exceeded: tasks") {
			t.Logf("recording with sleep(%s): c outlasted the sleep (%v); trying a longer one", ms, rerr)
			continue
		}
		if rerr != nil || sorted(whole) != sorted("1\n2\n") {
			t.Fatalf("recording: %v, %q", rerr, whole)
		}
		break
	}
	if sorted(whole) != sorted("1\n2\n") {
		t.Fatalf("recording: c outlasted every sleep, printed %q", whole)
	}
	for i := 0; i < 3; i++ {
		printed, resumed, err := resumeRun(t, src, cutAfter(t, log, `"req":[`+sleepMs+`]`), nil)
		if err != nil || sorted(printed) != sorted(whole) {
			t.Fatalf("resume %d: %v, printed %q", i, err, printed)
		}
		if replayed, err := replayRun(t, src, resumed); err != nil || replayed != printed {
			t.Fatalf("replay of resume %d: %v, %q", i, err, replayed)
		}
	}
}

// A resumed task past its records checks quietly while a finished task's
// pure tail is replayed, but it still sees a deadline: here the scope's
// 200 ms passes in the middle of its spin()s, as in the uninterrupted run, so
// neither the file nor "late" is written. Quiet checks hid the deadline
// for as long as c's tail took (review round 2). Gates, not timing,
// order b's line before the root's: b asks /arrive after it, and the
// root's /wait answers only then. The scope works until 1000 ms have
// passed on clock_ms (hold, A8), so it cannot reach write_file before its
// 200 ms deadline in any run; before A8 it spun a calibrated 1200 ms,
// which a calibration made while the machine was busier could shorten
// below the timeout (16zg, round 2). c's tail, eight spins, outlasts the
// scope's work, so the bug stays reachable.
func TestResumedQuietChecksSeeDeadlines(t *testing.T) {
	dir := t.TempDir()
	late := filepath.Join(dir, "late.txt")
	srv := gated(t)
	src := fmt.Sprintf(spinSrc()+holdSrc+`let c = fn() { sleep(10); spin(); spin(); spin(); spin(); spin(); spin(); spin(); spin(); 1 };
let b = fn() { let h = spawn c(); await h; puts("b"); http_get("%[1]s/arrive") };
spawn b() with [net("127.0.0.1")];
http_get("%[1]s/wait");
scope (timeout_ms: 200) { hold(1000); write_file(%[2]q, "late"); puts("late") };
puts("done")`, srv.URL, late)
	caps := []runtime.Capability{loopback[0], {Kind: runtime.FSWrite, Scope: dir}}
	recorded, log, rerr := recordRun(t, src, Options{Caps: caps})
	if rerr == nil || !strings.Contains(rerr.Error(), "timeout (limit 200 ms)") || recorded != "b\n" {
		t.Fatalf("recording: %v, %q", rerr, recorded)
	}
	if _, err := os.Stat(late); err == nil {
		t.Fatal("the uninterrupted run wrote the file")
	}
	resumeMatches(t, "opener", src, cutAfter(t, log, `/wait"]`), caps, recorded, rerr)
	if _, err := os.Stat(late); err == nil {
		t.Fatal("the resumed run wrote the file after its scope's deadline")
	}
}

// The same for a task spawned live inside the timed scope: w's checks
// are quiet while x's tail is replayed, and must still see the scope's
// deadline before write_file (review round 2). w works until 1000 ms have
// passed on clock_ms (hold, A8): it cannot reach write_file before the
// 200 ms deadline, in the recording or the resume. Before A8 w spun a
// calibrated 1200 ms, and once, under falling load, finished inside the
// scope: "recording: <nil>, \"q\nok\n\"" (16zg, round 2). x's tail, eight
// spins, outlasts w's work, so the bug stays reachable.
func TestResumedQuietChildSeesItsDeadline(t *testing.T) {
	dir := t.TempDir()
	late := filepath.Join(dir, "late.txt")
	srv := gated(t)
	src := fmt.Sprintf(spinSrc()+holdSrc+`let x = fn() { spin(); spin(); spin(); spin(); spin(); spin(); spin(); spin(); "x" };
let q = fn() { let h = spawn x(); await h; puts("q"); http_get("%[1]s/arrive"); "q" };
let w = fn() { hold(1000); write_file(%[2]q, "w ran past its deadline"); "w" };
let o = fn() { http_get("%[1]s/wait"); spin(); puts(match scope (timeout_ms: 200) { spawn w() with [fs_write(%[3]q)]; "ok" } { ok(v) => v, err(e) => e["message"] }) };
scope { spawn q() with [net("127.0.0.1")]; spawn o() with [net("127.0.0.1"), fs_write(%[3]q)] }`, srv.URL, late, dir)
	caps := []runtime.Capability{loopback[0], {Kind: runtime.FSWrite, Scope: dir}}
	recorded, log, rerr := recordRun(t, src, Options{Caps: caps})
	if rerr != nil || recorded != "q\nbudget exceeded: timeout (limit 200 ms)\n" {
		t.Fatalf("recording: %v, %q", rerr, recorded)
	}
	if _, err := os.Stat(late); err == nil {
		t.Fatal("the uninterrupted run wrote the file")
	}
	resumeMatches(t, "child", src, cutAfter(t, log, `/wait"]`), caps, recorded, rerr)
	if _, err := os.Stat(late); err == nil {
		t.Fatal("the resumed run wrote the file after its scope's deadline")
	}
}

// The wait for finished tasks before a live spawn takes no time on the
// recorded run's clock: those tasks had finished before the spawner's
// last record. So the spawner's clock does not run while it waits, and
// the scope's deadline, 500 ms after the sleep, does not pass during
// c's 1.2 s replayed tail. c works until 1200 ms have passed on clock_ms
// (hold, A8): four calibrated spins, as before A8, took longer than the
// 4 s sleep under load (-race, load about 300), and the premise failed.
func TestResumeWaitForFinishedTasksTakesNoTime(t *testing.T) {
	src := spinSrc() + holdSrc + `let c = fn() { sleep(10); hold(1200); 1 };
let d = fn() { puts("d"); 1 };
puts(match scope (timeout_ms: 4500) { spawn c(); sleep(4000); let h = spawn d(); await h } { ok(v) => v, err(e) => e["message"] })`
	recorded, log, rerr := recordRun(t, src, Options{})
	if rerr != nil || recorded != "d\n1\n" {
		t.Fatalf("recording: %v, %q", rerr, recorded)
	}
	done, slept := -1, bytes.Index(log, []byte(`"req":[4000]`))
	if m := regexp.MustCompile(`"task":"root/0","seq":\d+,"kind":"done"`).FindIndex(log); m != nil {
		done = m[0]
	}
	if done < 0 || done > slept {
		t.Fatalf("premise: c did not finish before the root's sleep ended\n%s", log)
	}
	resumeMatches(t, "wait", src, cutAfter(t, log, `"req":[4000]`), nil, recorded, rerr)
}

// A spawned task with no records of its own can still have records under
// it: its descendants', its restarts'. Its spawn got its slot then, and
// waiting for the finished tasks under it before spawning it would wait
// forever (found by TestTimedProgramsReplayAndResume).
func TestResumeSpawnsATaskWhoseDescendantsHaveRecords(t *testing.T) {
	src := `let leaf = fn() { puts("leaf"); 1 };
let mid = fn() { let r = await spawn leaf(); sleep(300); r };
puts(await spawn mid());
puts("end")`
	recorded, log, rerr := recordRun(t, src, Options{})
	if rerr != nil || recorded != "leaf\n1\nend\n" {
		t.Fatalf("recording: %v, %q", rerr, recorded)
	}
	crashed := cutAfter(t, log, `"task":"root/0/0","seq":2,"kind":"done"`)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	l, err := runtime.ReadLog(bytes.NewReader(crashed))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	_, err = Run(src, Options{Ctx: ctx, Resume: l, Record: bytes.NewBuffer(append([]byte(nil), crashed...)), Out: &out})
	if err != nil || out.String() != recorded {
		t.Fatalf("resume: %v, %q", err, out.String())
	}
}

// A finished task the spawner starts after this spawn is not waited for:
// the wait covers only tasks whose end the log has before the spawner's
// last record. Here p's spawn of a (no records: its first effect was in
// flight) comes before its spawn of b, which finished; waiting for b
// there would wait forever (found by TestTimedProgramsReplayAndResume).
func TestResumeDoesNotWaitForALaterSibling(t *testing.T) {
	src := `let a = fn() { sleep(400); puts("a"); 1 };
let b = fn() { puts("b"); 2 };
let p = fn() { puts("p"); let x = spawn a(); let y = spawn b(); await x + await y };
puts(await spawn p())`
	recorded, log, rerr := recordRun(t, src, Options{})
	if rerr != nil || recorded != "p\nb\na\n3\n" {
		t.Fatalf("recording: %v, %q", rerr, recorded)
	}
	crashed := cutAfter(t, log, `"task":"root/0/1","seq":2,"kind":"done"`)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	l, err := runtime.ReadLog(bytes.NewReader(crashed))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	_, err = Run(src, Options{Ctx: ctx, Resume: l, Record: bytes.NewBuffer(append([]byte(nil), crashed...)), Out: &out})
	if err != nil || out.String() != recorded {
		t.Fatalf("resume: %v, %q", err, out.String())
	}
}

// A deadline fired by a replayed record is not seen by a live task that
// is still behind the log: one whose next spawn has records. Here mid
// has no records of its own; its work() before spawning leaf runs live,
// while the root's logged sight of the deadline is replayed at once.
// Seeing it, mid would stop short of leaf, whose records the log has
// (found by TestTimedProgramsReplayAndResume).
func TestResumedTaskBehindTheLogDoesNotSeeTheDeadlineYet(t *testing.T) {
	src := spinSrc() + `let leaf = fn() { puts("leaf"); sleep(60000) };
let mid = fn() { spin(); let h = spawn leaf(); await h };
puts(match scope (timeout_ms: 1500) { let h = spawn mid(); await h } { ok(v) => v, err(e) => e["message"] })`
	// The cut is just after the root's await saw the deadline. The three
	// tasks see it at once, so the recording is repeated until mid's
	// record of it comes after the root's, and is cut off.
	for try := 1; ; try++ {
		recorded, log, rerr := recordRun(t, src, Options{})
		if rerr != nil || recorded != "leaf\nbudget exceeded: timeout (limit 1500 ms)\n" {
			t.Fatalf("recording: %v, %q", rerr, recorded)
		}
		crashed := cutAfter(t, log, `"task":"root","seq":1,"kind":"cancelled"`)
		if bytes.Contains(crashed, []byte(`"task":"root/0","seq"`)) {
			if try == 10 {
				t.Fatalf("premise: mid's record came first in 10 recordings\n%s", crashed)
			}
			continue
		}
		resumeMatches(t, "behind", src, crashed, nil, recorded, rerr)
		return
	}
}

// Every restart attempt's end is a record, not only the last one's: a
// failed attempt whose last stretch was pure work is replayed to its
// failure. Run live, its spin() saw the deadline that p's logged sleep
// fired at once, and it failed as cancelled instead of "down" (found by
// TestTimedProgramsReplayAndResume). The stretch is two unit()s, about
// 20 000 steps and 78 checks (A8): a calibrated spin() each, as before
// A8, made the three attempts outlast the 1500 ms scope under load.
func TestResumeReplaysAFailedAttemptToItsEnd(t *testing.T) {
	srv := gated(t)
	src := fmt.Sprintf(spinSrc()+`let f = fn() { let r = http_get("%[1]s/flaky"); unit(); unit(); if (r["status"] == 200) { "up" } else { error("down") } };
let p = fn() { puts(match scope (supervise: "restart", retries: 2, backoff_ms: 5, retry_on: ["user"]) { await spawn f() with [net("127.0.0.1")] } { ok(v) => v, err(e) => e["message"] }); sleep(60000) };
puts(match scope (timeout_ms: 1500) { let h = spawn p() with [net("127.0.0.1")]; await h } { ok(v) => v, err(e) => e["message"] })`, srv.URL)
	recorded, log, rerr := recordRun(t, src, Options{Caps: loopback})
	if rerr != nil || recorded != "up\nbudget exceeded: timeout (limit 1500 ms)\n" {
		t.Fatalf("recording: %v, %q", rerr, recorded)
	}
	resumeMatches(t, "attempt", src, cutAfter(t, log, `"by":"root:1"`), loopback, recorded, rerr)
}
