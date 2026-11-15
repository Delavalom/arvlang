package arv

import (
	"bytes"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// replayAlone replays log with no host context, as the CLI does: nothing
// but the replay itself can end it. It fails the test if the replay has
// not ended after 60 s, and returns its error.
func replayAlone(t *testing.T, src string, log []byte) error {
	t.Helper()
	l, err := runtime.ReadLog(bytes.NewReader(log))
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		_, err := Run(src, Options{Replay: l})
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(60 * time.Second):
		t.Fatal("the replay hung")
		return nil
	}
}

// Review round 3 (Important): a forged cancellation, its chain valid,
// held a replay forever; with no host context (the CLI) the Go runtime
// aborted with "all goroutines are asleep". Now a replay whose every task
// waits for something nothing will do diverges with a clean error.
func TestForgedCancellationDivergesInsteadOfHanging(t *testing.T) {
	src := "puts(\"a\");\nputs(\"b\");\n1"
	_, log, err := recordRun(t, src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// After puts("a"), the root was "cancelled" at its second check.
	var lines [][]byte
	for _, line := range bytes.Split(bytes.TrimSpace(log), []byte("\n")) {
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
	before := goruntime.NumGoroutine()
	start := time.Now()
	err = replayAlone(t, src, forged)
	var ae *Error
	if !errorsAs(err, &ae) || ae.Kind != runtime.KindReplay || !strings.Contains(ae.Msg, "nothing in this run will do") {
		t.Fatalf("err = %v, want a divergence", err)
	}
	t.Logf("diverged after %v", time.Since(start))
	if n := settle(before); n > before {
		t.Fatalf("%d goroutines outlive the replay, %d before", n, before)
	}
}

// A forged deadline that names the scope the task is in, at a place where
// the records before it can never be used (they belong to a task spawned
// after it), stalls the same way, and diverges the same way.
func TestForgedDeadlineAtTheWrongPlaceDiverges(t *testing.T) {
	src := `scope (timeout_ms: 60000) { puts("x"); await spawn fn() { puts("child") }() }`
	_, log, err := recordRun(t, src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var lines [][]byte
	for _, line := range bytes.Split(bytes.TrimSpace(log), []byte("\n")) {
		if bytes.Contains(line, []byte(`"end"`)) {
			continue
		}
		lines = append(lines, line)
	}
	// Drop the root's records, and say the deadline stopped it at its
	// first check, after the child's records. The child cannot start before the
	// root passes that point, and the root waits for the child's records.
	var rest [][]byte
	for _, line := range lines[1:] {
		if !bytes.Contains(line, []byte(`"task":"root",`)) {
			rest = append(rest, line)
		}
	}
	all := append([][]byte{lines[0]}, rest...)
	all = append(all, []byte(`{"task":"root","seq":1,"kind":"cancelled","check":1,"by":"root:1","h":"x"}`))
	all = append(all, []byte(`{"end":{"result":[null]},"h":"x"}`))
	forged := forge(t, append(bytes.Join(all, []byte("\n")), '\n'), func(i int, b []byte) []byte { return b })
	err = replayAlone(t, src, forged)
	var ae *Error
	if !errorsAs(err, &ae) || ae.Kind != runtime.KindReplay || !strings.Contains(ae.Msg, "nothing in this run will do") {
		t.Fatalf("err = %v, want a divergence", err)
	}
}
