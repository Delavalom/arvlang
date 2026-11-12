package arv

import (
	"context"
	"errors"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/evaluator"
	"github.com/delavalom/arvlang/arv/runtime"
)

// endless runs far longer than any test (10^12 loop bodies), in the root
// task and in four spawned tasks, so only a deadline or a cancel ends it.
var endless = func() string {
	big := "[" + strings.TrimSuffix(strings.Repeat("0, ", 10000), ", ") + "]"
	return "let big = " + big + ";\n" +
		"let spin = fn() { for a in big { for b in big { for c in big { a + b + c } } } };\n" +
		"let ts = for i in [1, 2, 3, 4] { spawn spin() };\nspin()"
}()

// Compiled code observes a deadline and a host's cancel as promptly as
// the tree-walker: each run ends with the budget's timeout or the
// cancellation well within the bound (generous: the machine may be under
// heavy load), and no goroutine outlives Run.
func TestEnginesObserveCancellationPromptly(t *testing.T) {
	for _, walk := range []bool{true, false} {
		old := evaluator.SetTreeWalk(walk)
		before := settle(0)

		start := time.Now()
		_, err := Run(endless, Options{Limits: runtime.Limits{Timeout: 100 * time.Millisecond}})
		var ae *Error
		if !errors.As(err, &ae) || ae.Msg != "budget exceeded: timeout (limit 100 ms)" {
			t.Errorf("walk=%v: err = %v, want the timeout", walk, err)
		}
		if d := time.Since(start); d > 20*time.Second {
			t.Errorf("walk=%v: the timeout took %v", walk, d)
		}

		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(100*time.Millisecond, cancel)
		start = time.Now()
		if _, err := Run(endless, Options{Ctx: ctx}); err == nil || !strings.Contains(err.Error(), "cancel") {
			t.Errorf("walk=%v: err = %v, want a cancellation", walk, err)
		}
		if d := time.Since(start); d > 20*time.Second {
			t.Errorf("walk=%v: the cancel took %v", walk, d)
		}
		cancel()

		evaluator.SetTreeWalk(old)
		if after := settle(before); after > before {
			buf := make([]byte, 1<<20)
			t.Fatalf("walk=%v: %d goroutines before, %d after:\n%s", walk, before, after, buf[:goruntime.Stack(buf, true)])
		}
	}
}
