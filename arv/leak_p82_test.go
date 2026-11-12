package arv

import (
	"context"
	"errors"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// Leak rule for host effects: no goroutine outlives Run on any path that
// ends a run while host calls are in flight or failing: timeouts with
// many tasks waiting in Fn, a budget spent by a host result while
// siblings wait in Fn, restarts and collect over failing host calls, and
// the run's own timeout. Every waiting Fn must also have returned (the
// scope waits for it, I4): calls entered equals calls returned.
func TestNoGoroutineOutlivesRunWithHostEffects(t *testing.T) {
	var entered, returned atomic.Int64
	wait := func(t *runtime.Task, args ...value.Object) (value.Object, error) {
		entered.Add(1)
		defer returned.Add(1)
		<-t.Ctx.Done()
		return nil, context.Cause(t.Ctx)
	}
	fail := func(t *runtime.Task, args ...value.Object) (value.Object, error) {
		entered.Add(1)
		defer returned.Add(1)
		return nil, errors.New("flaky")
	}
	big := func(t *runtime.Task, args ...value.Object) (value.Object, error) {
		entered.Add(1)
		defer returned.Add(1)
		elems := make([]value.Object, 4096)
		for i := range elems {
			elems[i] = &value.String{Value: strings.Repeat("y", 1024)}
		}
		return &value.Array{Elements: elems}, nil
	}
	k8s := func(fn func(*runtime.Task, ...value.Object) (value.Object, error)) Effect {
		return Effect{Kind: "k8s", Scope: nsScope, Fn: fn}
	}
	base := Options{Caps: []runtime.Capability{staging}, Effects: map[string]Effect{
		"wait": k8s(wait), "fail": k8s(fail), "big": k8s(big)}}
	const w = "let w = fn() { wait(\"staging\") };\nlet many = fn(n) { if (n > 0) { spawn w() with [k8s(\"staging\")]; many(n - 1) } };\n"
	for _, tt := range []struct {
		name, src, want string
		timeout         time.Duration
	}{
		{"scope timeout with tasks waiting in Fn",
			w + `scope (timeout_ms: 50) { many(20); w() }`, "timeout", 0},
		{"run timeout with tasks waiting in Fn",
			w + `many(20); w()`, "timeout", 50 * time.Millisecond},
		{"nested scopes time out around waiting calls",
			w + `scope (timeout_ms: 50) { scope (timeout_ms: 600000) { many(5); w() } }`, "timeout", 0},
		{"a host result spends alloc_mb while siblings wait in Fn",
			w + `scope (alloc_mb: 1) { many(10); let b = fn() { big("staging") }; await spawn b() with [k8s("staging")] }`, "alloc", 0},
		{"a failing host call fails siblings waiting in Fn",
			w + `scope { many(10); let f = fn() { fail("staging") }; await spawn f() with [k8s("staging")] }`, "flaky", 0},
		{"restart retries a failing host call while siblings wait",
			w + `let f = fn() { fail("staging") };
scope (supervise: "restart", retries: 3, backoff_ms: 1, retry_on: ["effect"]) { many(5); await spawn f() with [k8s("staging")] }`, "flaky", 0},
		{"collect over failing calls, then a timeout",
			w + `scope (timeout_ms: 50) { scope (supervise: "collect") { let f = fn() { fail("staging") }; spawn f() with [k8s("staging")]; many(5) } }`, "", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			entered.Store(0)
			returned.Store(0)
			opts := base
			opts.Limits.Timeout = tt.timeout
			before := goruntime.NumGoroutine()
			_, err := Run(tt.src, opts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, tt.want)
			}
			if entered.Load() != returned.Load() {
				t.Fatalf("Run returned with %d of %d host calls still in Fn", entered.Load()-returned.Load(), entered.Load())
			}
			noLeak(t, before, tt.src)
		})
	}
}
