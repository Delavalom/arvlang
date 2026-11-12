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
//
// Each call loops over 1000 elements before it recurses, so the 10 000
// calls of the depth limit take seconds on any engine, and the 20 ms
// deadline ends the run first. (Before Phase 8.4 each call only
// recursed: on a quiet machine the depth limit ended the run first, and
// its logged end failed the test; see 16za-phase6-mutations.md.)
func TestHostDeadlineIsNotLogged(t *testing.T) {
	src := "let a = [" + strings.TrimSuffix(strings.Repeat("0, ", 1000), ", ") + "];\n" +
		`let f = fn(i) { for x in a { len("x") }; f(i + 1) }; f(0)`
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
