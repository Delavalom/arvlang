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

// A panic anywhere in a host's code (Scope, Fn in a spawned task, a
// kind's Covers while spawn ... with narrows a grant or while a child is
// admitted) is an error from Run, never a panic in the host, and leaves
// no goroutine behind.
func TestRunHostPanicsAnywhere(t *testing.T) {
	boom := func(t *runtime.Task, args ...value.Object) (value.Object, error) { panic("fn bug") }
	badScope := Effect{Kind: "k8s", Fn: applied, Scope: func([]value.Object) (string, error) { panic("scope bug") }}
	covers := func(g, r string) bool {
		if r == "panic" {
			panic("covers bug")
		}
		return g == r || g == "*"
	}
	team := Effect{Kind: "team", Scope: nsScope, Fn: applied}
	teamOpts := Options{
		Kinds:   []runtime.KindSpec{{Kind: "team", Covers: covers}},
		Effects: map[string]Effect{"page": team},
		Caps:    []runtime.Capability{{Kind: "team", Scope: "*"}},
	}
	for _, tt := range []struct {
		name, src, want string
		opts            Options
	}{
		{"scope", `bad("staging")`, "scope bug", Options{Caps: []runtime.Capability{staging}, Effects: map[string]Effect{"bad": badScope}}},
		{"fn in a child", `let f = fn() { boom("staging") }; await spawn f() with [k8s("staging")]`, "fn bug",
			Options{Caps: []runtime.Capability{staging}, Effects: map[string]Effect{"boom": {Kind: "k8s", Scope: nsScope, Fn: boom}}}},
		{"covers in spawn with", `let f = fn() { 1 }; await spawn f() with [team("panic")]`, "covers bug", teamOpts},
		{"covers in a child", `let f = fn() { page("panic") }; await spawn f() with [team("*")]`, "covers bug", teamOpts},
		{"covers in a scope", `scope { page("panic") }`, "covers bug", teamOpts},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := goruntime.NumGoroutine()
			_, err := Run(tt.src, tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, tt.want)
			}
			noLeak(t, before, tt.src)
		})
	}
}

// A call issued once its task is cancelled, or once its scope's steps
// are spent (by a child the root has awaited), never reaches Fn. The
// cancel and the spending both happen before the call, in program order,
// so no timing is involved. Whichever check stops the program first, the
// guarantee is the same: Fn is not called.
func TestRunHostEffectStopsBeforeFn(t *testing.T) {
	var n atomic.Int64
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := func(t *runtime.Task, args ...value.Object) (value.Object, error) {
		cancel()
		<-t.Ctx.Done() // the run's context reaches the task before stop returns
		return nil, nil
	}
	opts := Options{Ctx: ctx, Caps: []runtime.Capability{staging}, Effects: map[string]Effect{
		"stop": {Kind: "k8s", Scope: nsScope, Fn: stop}, "k8s_apply": counted(&n, applied)}}
	_, err := Run(`stop("staging"); k8s_apply("staging", "m")`, opts)
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != runtime.KindCancelled || n.Load() != 0 {
		t.Fatalf("after cancel: err = %v after %d calls", err, n.Load())
	}
	opts = Options{Caps: []runtime.Capability{staging}, Effects: map[string]Effect{"k8s_apply": counted(&n, applied)}}
	src := `scope (steps: 2000) {
  let burn = fn(i) { burn(i + 1) };
  let t = spawn burn(0);
  match await t { ok(v) => v, err(e) => e };
  k8s_apply("staging", "m")
}`
	_, err = Run(src, opts)
	if !errors.As(err, &ae) || ae.Kind != runtime.KindBudget || n.Load() != 0 {
		t.Fatalf("after the steps are spent: err = %v after %d calls", err, n.Load())
	}
}

// A call whose scope times out while the host's Scope runs never reaches
// Fn: the task is checked again once the call is admitted. Task.Err reads
// the deadline itself, so once Scope has slept past it the check sees it
// whatever the timer has done: no timing is involved.
func TestRunHostEffectStopsAfterSlowScope(t *testing.T) {
	var n atomic.Int64
	slow := counted(&n, applied)
	slow.Scope = func(a []value.Object) (string, error) {
		time.Sleep(300 * time.Millisecond)
		return nsScope(a)
	}
	opts := Options{Caps: []runtime.Capability{staging}, Effects: map[string]Effect{"slow": slow}}
	before := goruntime.NumGoroutine()
	src := `scope (timeout_ms: 50) { slow("staging") }`
	_, err := Run(src, opts)
	var ae *Error
	if !errors.As(err, &ae) || n.Load() != 0 {
		t.Fatalf("err = %v after %d calls of Fn", err, n.Load())
	}
	noLeak(t, before, src)
}
