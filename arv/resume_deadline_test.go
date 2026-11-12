package arv

import (
	"fmt"
	"os"
	"path/filepath"
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

// spin is about 300 ms of pure work here; the tests below rely only on
// ratios between such calls, which load does not change much.
const spin = `let a = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10];
let spin = fn() { for x in a { for y in a { for z in a { for w in a { for v in a { for u in a { 1 } } } } } }; 0 };
`

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
func TestResumeWaitsForFinishedTasksBeforeTheCap(t *testing.T) {
	src := spin + `let c = fn() { sleep(10); spin(); 1 };
let d = fn(n) { puts(n); n };
scope { spawn c(); sleep(2500); let h1 = spawn d(1); let h2 = spawn d(2); await h1 + await h2 }`
	whole, log, rerr := recordRun(t, src, Options{Limits: runtime.Limits{LiveTasks: 2}})
	if rerr != nil || sorted(whole) != sorted("1\n2\n") {
		t.Fatalf("recording: %v, %q", rerr, whole)
	}
	for i := 0; i < 3; i++ {
		printed, resumed, err := resumeRun(t, src, cutAfter(t, log, `"req":[2500]`), nil)
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
// 200 ms passes in the middle of spin(), as in the uninterrupted run, so
// neither the file nor "late" is written. Quiet checks hid the deadline
// for as long as c's tail took (review round 2). Gates, not timing,
// order b's line before the root's: b asks /arrive after it, and the
// root's /wait answers only then.
func TestResumedQuietChecksSeeDeadlines(t *testing.T) {
	dir := t.TempDir()
	late := filepath.Join(dir, "late.txt")
	srv := gated(t)
	src := fmt.Sprintf(spin+`let c = fn() { sleep(10); spin(); spin(); spin(); spin(); spin(); spin(); 1 };
let b = fn() { let h = spawn c(); await h; puts("b"); http_get("%[1]s/arrive") };
spawn b() with [net("127.0.0.1")];
http_get("%[1]s/wait");
scope (timeout_ms: 200) { spin(); write_file(%[2]q, "late"); puts("late") };
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
// deadline before write_file (review round 2).
func TestResumedQuietChildSeesItsDeadline(t *testing.T) {
	dir := t.TempDir()
	late := filepath.Join(dir, "late.txt")
	srv := gated(t)
	src := fmt.Sprintf(spin+`let x = fn() { spin(); spin(); spin(); spin(); spin(); spin(); spin(); spin(); "x" };
let q = fn() { let h = spawn x(); await h; puts("q"); http_get("%[1]s/arrive"); "q" };
let w = fn() { spin(); spin(); write_file(%[2]q, "w ran past its deadline"); "w" };
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
