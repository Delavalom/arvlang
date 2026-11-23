package arv

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/evaluator"
	"github.com/delavalom/arvlang/arv/runtime"
)

// bigLoggedSrc is about 2 MB of uncalled functions, then a loop that
// only a deadline ends: compiling it takes far longer than 10 ms.
var bigLoggedSrc = func() string {
	var b strings.Builder
	for i := 0; b.Len() < 2<<20; i++ {
		fmt.Fprintf(&b, "let f%d = fn(a, b) { if (a < b) { [a, b, a + b * 2] } else { f%d(b, a) } };\n", i, i)
	}
	b.WriteString("let big = [" + strings.TrimSuffix(strings.Repeat("0, ", 1000), ", ") + "];\n")
	b.WriteString("for a in big { for b in big { for c in big { a + b + c } } }\n")
	return b.String()
}()

// A7: a recorded run whose deadline passes while it compiles stops
// compiling and runs on the tree-walker, whose first check sees the
// deadline and is logged as any check is: check 1, the first check the
// run makes. The log replays on both engines with the same error and no
// divergence, as a log whose run never compiled would.
func TestCompileCancelledRunLogsAndReplays(t *testing.T) {
	const want = "budget exceeded: timeout (limit 10 ms)"
	old := evaluator.SetTreeWalk(false)
	defer evaluator.SetTreeWalk(old)
	start := time.Now()
	_, log, err := recordRun(t, bigLoggedSrc, Options{Limits: runtime.Limits{Timeout: 10 * time.Millisecond}})
	var ae *Error
	if !errors.As(err, &ae) || ae.Msg != want || ae.Kind != runtime.KindBudget {
		t.Fatalf("recorded err = %v, want %q", err, want)
	}
	t.Logf("recorded in %v (parse included), log %d bytes", time.Since(start), len(log))
	if !bytes.Contains(log, []byte(`"check":1,`)) {
		t.Errorf("the log has no check 1:\n%s", log)
	}
	for _, walk := range []bool{false, true} {
		evaluator.SetTreeWalk(walk)
		_, err := replayRun(t, bigLoggedSrc, log)
		if !errors.As(err, &ae) || ae.Msg != want || ae.Kind != runtime.KindBudget {
			t.Errorf("walk=%v: replay err = %v, want %q", walk, err, want)
		}
	}
}

// A7: Run charges the program's code to alloc_mb, 64 bytes per AST node,
// on both engines: `1 + 2` (5 nodes, 320 bytes, nothing else allocated)
// runs under 320 bytes and fails under 319, before puts prints.
func TestRunChargesTheProgramsCode(t *testing.T) {
	const src = "1 + 2"
	for _, walk := range []bool{false, true} {
		old := evaluator.SetTreeWalk(walk)
		if _, err := Run(src, Options{Limits: runtime.Limits{AllocBytes: 320}}); err != nil {
			t.Errorf("walk=%v: under 320 bytes: %v", walk, err)
		}
		var out bytes.Buffer
		_, err := Run(`puts("x"); `+src, Options{Out: &out, Limits: runtime.Limits{AllocBytes: 319 + 4*64}})
		var ae *Error
		if !errors.As(err, &ae) || ae.Msg != "budget exceeded: alloc (limit 575 bytes)" || ae.Kind != runtime.KindBudget || out.Len() != 0 {
			t.Errorf("walk=%v: under 575 bytes: %v, printed %q", walk, err, out.String())
		}
		evaluator.SetTreeWalk(old)
	}
}
