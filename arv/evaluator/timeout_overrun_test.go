package evaluator

import (
	goruntime "runtime"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// slow is a host builtin that ignores cancellation: it takes d, then
// returns 7. Context is checked only every 256 nodes and in cancellable
// builtins, so code like this can run past a deadline without noticing.
func slow(d time.Duration) map[string]value.BuiltinFunction {
	return map[string]value.BuiltinFunction{"slow": func(*runtime.Task, ...value.Object) value.Object {
		time.Sleep(d)
		return &value.Integer{Value: 7}
	}}
}

// A scope that closes after its deadline has exceeded its timeout, even
// if nothing checked the deadline in time and everything succeeded.
func TestTimeoutOverrunIsAnError(t *testing.T) {
	for _, tt := range []struct{ name, src, pos string }{
		{"body", "scope (timeout_ms: 20) {\n slow() }", "1:1"},
		{"spawned child", "scope (timeout_ms: 20) { spawn slow(); 1 }", "1:1"},
		{"nested unlimited scope", "scope (timeout_ms: 20) { scope { slow() } }", "1:1"},
		{"awaited child", "scope (timeout_ms: 20) { await spawn slow() }", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := goruntime.NumGoroutine()
			got := runBounded(t, time.Minute, tt.src, slow(400*time.Millisecond))
			if tt.pos == "" {
				wantErrorHas(t, got, "budget exceeded: timeout (limit 20 ms)")
			} else {
				wantErrorAt(t, got, "budget exceeded: timeout (limit 20 ms)", tt.pos)
			}
			settled(t, before)
		})
	}
}

// The run's own timeout is enforced the same way.
func TestRootTimeoutOverrunIsAnError(t *testing.T) {
	binds := slow(400 * time.Millisecond)
	for _, src := range []string{"1;\nslow()", "spawn slow();\n1"} {
		got := runLimitedEnv(t, src, binds, runtime.Limits{Timeout: 20 * time.Millisecond})
		wantErrorAt(t, got, "budget exceeded: timeout (limit 20 ms)", "2:1")
	}
}

// A scope that ends in time is not a timeout.
func TestScopeWithinItsTimeoutSucceeds(t *testing.T) {
	got := runBounded(t, time.Minute, "scope (timeout_ms: 60000) { slow() }", slow(time.Millisecond))
	if got.Inspect() != "7" {
		t.Fatalf("got %s, want 7", got.Inspect())
	}
}

// A host timeout under a millisecond is reported as 1 ms, the next whole
// millisecond, not as "limit 0 ms".
func TestSubMillisecondTimeoutLimit(t *testing.T) {
	got := runLimited(t, "sleep(5000)", runtime.Limits{Timeout: 999 * time.Microsecond})
	wantErrorAt(t, got, "budget exceeded: timeout (limit 1 ms)", "1:1")
	got = runLimited(t, "sleep(5000)", runtime.Limits{Timeout: 1500 * time.Microsecond})
	wantErrorAt(t, got, "budget exceeded: timeout (limit 2 ms)", "1:1")
}
