package arv

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// Review round 4 (Important): a forged cancellation in the part of a log
// a resume replays held the task forever; with no host context (the CLI)
// the Go runtime aborted with "all goroutines are asleep". A resume now
// watches for a stall while the log has records left, as a replay does.
func TestForgedCancellationInAResumeDiverges(t *testing.T) {
	src := "puts(\"a\");\nputs(\"b\");\n1"
	_, log, err := recordRun(t, src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var lines [][]byte
	for _, line := range bytes.Split(bytes.TrimSpace(log), []byte("\n")) {
		if bytes.Contains(line, []byte(`"end"`)) {
			continue // the run crashed before it ended
		}
		lines = append(lines, line)
		if bytes.Contains(line, []byte(`"req":["a"]`)) {
			lines = append(lines, []byte(`{"task":"root","seq":2,"kind":"cancelled","check":2,"h":"x"}`))
		}
	}
	forged := forge(t, append(bytes.Join(lines, []byte("\n")), '\n'), func(i int, b []byte) []byte {
		if bytes.Contains(b, []byte(`"req":["b"]`)) {
			return bytes.Replace(b, []byte(`"seq":2`), []byte(`"seq":3`), 1)
		}
		return b
	})
	l, err := runtime.ReadLog(bytes.NewReader(forged))
	if err != nil || l.End != nil {
		t.Fatalf("forged log: %v", err)
	}
	before := settle(0)
	done := make(chan error, 1)
	go func() {
		_, err := Run(src, Options{Resume: l, Record: bytes.NewBuffer(append([]byte(nil), forged[:l.Size]...))})
		done <- err
	}()
	select {
	case err = <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("the resume hung")
	}
	var ae *Error
	if !errorsAs(err, &ae) || ae.Kind != runtime.KindReplay || !strings.Contains(ae.Msg, "nothing in this run will do") {
		t.Fatalf("err = %v, want a divergence", err)
	}
	if n := settle(before); n > before {
		t.Fatalf("%d goroutines outlive the resume, %d before", n, before)
	}
}

// resumeForged resumes src from forged, a log of a run that did not
// finish, with no host context (the CLI), and fails the test if the
// resume has not ended after 60 s or leaks a goroutine. It returns the
// error.
func resumeForged(t *testing.T, src string, forged []byte) error {
	t.Helper()
	l, err := runtime.ReadLog(bytes.NewReader(forged))
	if err != nil || l.End != nil {
		t.Fatalf("forged log: %v", err)
	}
	before := settle(0)
	done := make(chan error, 1)
	go func() {
		_, err := Run(src, Options{Resume: l, Record: bytes.NewBuffer(append([]byte(nil), forged[:l.Size]...))})
		done <- err
	}()
	select {
	case err = <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("the resume hung")
	}
	if n := settle(before); n > before {
		t.Fatalf("%d goroutines outlive the resume, %d before", n, before)
	}
	return err
}

// Review round 5 (Important): two forged logs still stalled a resume
// with nothing live to end it, which through the CLI is the Go runtime's
// "all goroutines are asleep". A forged cancellation that is the log's
// last record drained the log, and the stall watcher stopped diverging
// once it had; and a forged end of a task that never runs held a live
// spawn in its wait for that end (waitDone), which did not count as a
// wait on the log. A resume now diverges whenever every task waits on
// the log, or on another task, and no live deadline is pending.
func TestForgedResumesThatDrainOrWaitForAnEndDiverge(t *testing.T) {
	t.Run("cancellation last", func(t *testing.T) {
		src := "puts(\"a\");\nputs(\"b\");\n1"
		_, log, err := recordRun(t, src, Options{})
		if err != nil {
			t.Fatal(err)
		}
		var lines [][]byte
		for _, line := range bytes.Split(bytes.TrimSpace(log), []byte("\n")) {
			lines = append(lines, line)
			if bytes.Contains(line, []byte(`"req":["a"]`)) {
				lines = append(lines, []byte(`{"task":"root","seq":2,"kind":"cancelled","check":2,"h":"x"}`))
				break // the run crashed here
			}
		}
		err = resumeForged(t, src, forge(t, append(bytes.Join(lines, []byte("\n")), '\n'), func(i int, b []byte) []byte { return b }))
		var ae *Error
		if !errorsAs(err, &ae) || ae.Kind != runtime.KindReplay || !strings.Contains(ae.Msg, "nothing in this run will do") {
			t.Fatalf("err = %v, want a divergence", err)
		}
	})
	t.Run("end of a task that never runs", func(t *testing.T) {
		src := "puts(\"a\");\nlet t = spawn fn() { 1 }();\nawait t"
		_, log, err := recordRun(t, src, Options{Limits: runtime.Limits{Timeout: 10 * time.Minute}}) // as the CLI does
		if err != nil {
			t.Fatal(err)
		}
		lines := bytes.Split(bytes.TrimSpace(log), []byte("\n"))[:2] // the header, puts("a")
		lines = append(lines, []byte(`{"task":"root/7","seq":1,"kind":"done","at":0,"h":"x"}`))
		err = resumeForged(t, src, forge(t, append(bytes.Join(lines, []byte("\n")), '\n'), func(i int, b []byte) []byte {
			if i == 1 { // puts("a") ran after root/7 ended
				b = regexp.MustCompile(`,"(began|at)":\d+`).ReplaceAll(b, nil)
				return append(bytes.TrimSuffix(b, []byte(`}`)), `,"began":5,"at":5}`...)
			}
			return b
		}))
		var ae *Error
		if !errorsAs(err, &ae) || ae.Kind != runtime.KindReplay {
			t.Fatalf("err = %v, want a divergence", err)
		}
	})
}
