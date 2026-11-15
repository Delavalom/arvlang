package arv

import (
	"bytes"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// identity is a forge edit that keeps every line: forge then only
// recomputes the chain.
func identity(_ int, b []byte) []byte { return b }

// wantStall fails the test unless err is a resume's clean divergence
// for a log that holds every task waiting.
func wantStall(t *testing.T, err error) {
	t.Helper()
	var ae *Error
	if !errorsAs(err, &ae) || ae.Kind != runtime.KindReplay || !strings.Contains(ae.Msg, "nothing in this run will do") {
		t.Fatalf("err = %v, want a divergence", err)
	}
}

// cancelledChild is the first two lines of a recording of src (the
// header, puts("a")) and a forged cancellation of root/0 at its first
// check, which no deadline or failure of the resume will ever cause.
func cancelledChild(t *testing.T, src string) []byte {
	t.Helper()
	_, log, err := recordRun(t, src, Options{}) // no deadline: no timer to wait for
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(log), []byte("\n"))[:2]
	lines = append(lines, []byte(`{"task":"root/0","seq":1,"kind":"cancelled","check":1,"at":2,"h":"x"}`))
	return forge(t, append(bytes.Join(lines, []byte("\n")), '\n'), identity)
}

// Review round 6 (Critical): a forged log still killed a resume with
// "all goroutines are asleep" (exit 2), or, with a deadline, held it
// until the deadline fired, when a live task waited on a task the log
// held forever: a live await, or a live wait for a scope slot, was not
// parked, so the stall watcher never started. Both are parked now.
func TestForgedResumeWithALiveWaitOnAHeldTaskDiverges(t *testing.T) {
	t.Run("await", func(t *testing.T) {
		src := "puts(\"a\");\nlet h = spawn fn() { puts(\"x\"); 1 }();\nlet v = await h;\nputs(v);\nv"
		wantStall(t, resumeForged(t, src, cancelledChild(t, src)))
	})
	t.Run("slot", func(t *testing.T) {
		src := "puts(\"a\");\nlet r = scope (tasks: 1) { let a = spawn fn() { puts(\"x\"); 1 }(); let b = spawn fn() { 2 }(); [await a, await b] };\nputs(r);\nr"
		wantStall(t, resumeForged(t, src, cancelledChild(t, src)))
	})
}

// Review round 6 (Critical): a resumed spawn whose child has records
// waited for its slot (tasks:) unparked, on the run's abort alone. When
// the task in the slot failed live, its failure waited for the log to
// drain (Task.Fail), which needed that child: nothing could end the
// wait. Since the recorder logs a task's end before it frees its slot
// (TestTaskEndIsLoggedBeforeItsSlotIsFreed), only a forged log has the
// child's records without the holder's end, and the parked wait now
// lets such a resume diverge.
func TestForgedResumeWhoseSlotHolderFailsLiveDiverges(t *testing.T) {
	src := `let x = fn() { let h = spawn fn() { 1 }(); await h; if (getenv("ARV_P6_R6") == "fail") { 1 / 0 } else { "x" } };
let y = fn() { puts("y"); "y" };
puts(scope (tasks: 1) { let a = spawn x() with [env("ARV_P6_R6")]; let b = spawn y(); [await a, await b] });
puts("end")`
	env, _ := runtime.New(runtime.Env, "ARV_P6_R6")
	t.Setenv("ARV_P6_R6", "")
	out, log, err := recordRun(t, src, Options{Caps: []runtime.Capability{env}})
	if err != nil || out != "y\n[x, y]\nend\n" {
		t.Fatalf("recording: %v %q", err, out)
	}
	var keep [][]byte
	for _, l := range bytes.Split(bytes.TrimSpace(log), []byte("\n")) {
		if bytes.Contains(l, []byte(`"task":"root/0","`)) {
			continue // x's getenv and end: forged away
		}
		keep = append(keep, l)
		if bytes.Contains(l, []byte(`"req":["y"]`)) {
			break // the crash
		}
	}
	t.Setenv("ARV_P6_R6", "fail") // the resumed x fails live, holding the slot
	forged := forge(t, append(bytes.Join(keep, []byte("\n")), '\n'), identity)
	wantStall(t, resumeForgedWith(t, src, forged, Options{Caps: []runtime.Capability{env}}))
}
