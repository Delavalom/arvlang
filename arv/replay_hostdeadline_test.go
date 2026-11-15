package arv

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// Review round 3 (Critical): a host's deadline (Options.Ctx) that passed
// before its timer fired was seen by checks as context.DeadlineExceeded
// and logged as a cancellation no deadline in the log names. The run
// then finished its log, and a replay of it waited for a cancellation
// nothing would cause. A host's deadline is the host cancelling the run:
// nothing from then on is logged, so the log is unfinished, and a resume
// of it, which replays its records, does not wait on anything.
func TestHostDeadlineIsNotLogged(t *testing.T) {
	// Busy work that outlasts the deadline on any machine: 10^9 calls of
	// a builtin, each a check, at most 1000 deep. (An unbounded recursion
	// here hit the 10000-call depth limit before 20 ms on an idle machine
	// without -race.) Each g(1000) leaves one 0 in the loops' arrays.
	src := `let a = [` + ones(1000) + `];
let g = fn(n) { if (n == 0) { 0 } else { len("x"); g(n - 1) } };
for i in a { for j in a { g(1000) } }`
	for i := range 3 {
		var log bytes.Buffer
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		_, err := Run(src, Options{Ctx: ctx, Record: &log})
		cancel()
		if err == nil {
			t.Fatal("the run ended without its deadline")
		}
		if strings.Contains(log.String(), `"kind":"cancelled"`) || strings.Contains(log.String(), `"end"`) {
			t.Fatalf("run %d: the host's deadline is in the log:\n%s", i, log.String())
		}
		l, rerr := runtime.ReadLog(bytes.NewReader(log.Bytes()))
		if rerr != nil {
			t.Fatal(rerr)
		}
		if l.End != nil {
			t.Fatal("the log is finished")
		}
	}
}
