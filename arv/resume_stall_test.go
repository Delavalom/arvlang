package arv

import (
	"bytes"
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
