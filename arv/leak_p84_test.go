package arv

import (
	"bytes"
	"context"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/evaluator"
	"github.com/delavalom/arvlang/arv/runtime"
)

// Leak rule for Phase 8.4: compiled code runs task bodies, restarts,
// scopes and the loops a cancellation interrupts; on either engine no
// goroutine may outlive Run, whether the run succeeds, fails fast,
// restarts, runs out of steps in a task, hits a scope's or the run's
// timeout, or is cancelled by the host.
func TestP84NoGoroutineOutlivesRun(t *testing.T) {
	spin := "let ten = [0, 0, 0, 0, 0, 0, 0, 0, 0, 0];\n" +
		"let spin = fn() { for a in ten { for b in ten { for c in ten { for d in ten { for e in ten { for f in ten { for g in ten { for h in ten { a + h } } } } } } } } };\n"
	runs := []struct {
		name, src string
		limits    runtime.Limits
		cancel    time.Duration // the host cancels after this; 0 never
		errHas    string        // "" means the run succeeds
	}{
		{"tasks run compiled closures", `let k = 3; let f = fn(x) { fn(y) { x * y + k } };
			scope { let ts = for i in [1, 2, 3, 4] { spawn f(i)(i) }; for t in ts { await t } }`, runtime.Limits{}, 0, ""},
		{"fail fast cancels spinning tasks", spin + `let boom = fn() { error("boom") };
			scope { let a = spawn spin(); let b = spawn spin(); let c = spawn boom(); await a }`, runtime.Limits{}, 0, "boom"},
		{"restarts", `let f = fn() { error("again") };
			scope (supervise: "restart", retries: 3, backoff_ms: 1, retry_on: ["user"]) { let t = spawn f(); await t }`, runtime.Limits{}, 0, "again"},
		{"collect", `let f = fn(i) { if (i == 2) { error("two") } else { i } };
			scope (supervise: "collect") { let ts = for i in [1, 2, 3] { spawn f(i) }; for t in ts { match await t { ok(v) => v, err(e) => e.message } } }`, runtime.Limits{}, 0, ""},
		{"steps run out in tasks", spin + `scope { let ts = for i in [1, 2, 3] { spawn spin() }; for t in ts { await t } }`,
			runtime.Limits{Steps: 20000}, 0, "budget exceeded: steps"},
		{"a scope's timeout", spin + `match scope (timeout_ms: 50) { let t = spawn spin(); spin() } { ok(v) => v, err(e) => e.message }`,
			runtime.Limits{}, 0, ""},
		{"the run's timeout", spin + `let t = spawn spin(); spin()`, runtime.Limits{Timeout: 100 * time.Millisecond}, 0, "timeout"},
		{"the host cancels", spin + `let ts = for i in [1, 2] { spawn spin() }; spin()`, runtime.Limits{}, 100 * time.Millisecond, "cancel"},
	}
	for _, walk := range []bool{true, false} {
		old := evaluator.SetTreeWalk(walk)
		before := settle(0)
		for _, r := range runs {
			ctx, cancel := context.WithCancel(context.Background())
			if r.cancel > 0 {
				time.AfterFunc(r.cancel, cancel)
			}
			var out bytes.Buffer
			_, err := Run(r.src, Options{Ctx: ctx, Out: &out, Limits: r.limits})
			cancel()
			switch {
			case r.errHas == "" && err != nil:
				t.Errorf("walk=%v, %s: %v", walk, r.name, err)
			case r.errHas != "" && (err == nil || !strings.Contains(err.Error(), r.errHas)):
				t.Errorf("walk=%v, %s: err = %v, want %q", walk, r.name, err, r.errHas)
			}
		}
		evaluator.SetTreeWalk(old)
		if after := settle(before); after > before {
			buf := make([]byte, 1<<20)
			t.Fatalf("walk=%v: %d goroutines before the runs, %d after:\n%s", walk, before, after, buf[:goruntime.Stack(buf, true)])
		}
	}
}

// Runs with tasks leave no goroutine behind when eight run at once, on
// each engine.
func TestP84NoGoroutineOutlivesConcurrentRuns(t *testing.T) {
	before := settle(0)
	src := `let f = fn(x) { for i in [1, 2, 3] { x * i } };
		scope { let ts = for i in [1, 2, 3] { spawn f(i) }; for t in ts { await t } }`
	for _, walk := range []bool{true, false} {
		old := evaluator.SetTreeWalk(walk)
		done := make(chan error, 8)
		for i := 0; i < 8; i++ {
			go func() {
				_, err := Run(src, Options{})
				done <- err
			}()
		}
		for i := 0; i < 8; i++ {
			if err := <-done; err != nil {
				t.Errorf("walk=%v: %v", walk, err)
			}
		}
		evaluator.SetTreeWalk(old)
	}
	if after := settle(before); after > before {
		t.Fatalf("%d goroutines before, %d after", before, after)
	}
}
