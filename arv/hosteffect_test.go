package arv

import (
	"bytes"
	"context"
	"errors"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// nsScope is the Scope of a k8s effect: its first argument, a namespace.
func nsScope(args []value.Object) (string, error) {
	if len(args) == 0 {
		return "", errors.New("want a namespace")
	}
	s, ok := args[0].(*value.String)
	if !ok {
		return "", errors.New("namespace must be STRING")
	}
	return s.Value, nil
}

// counted is a k8s effect that counts the calls reaching fn.
func counted(n *atomic.Int64, fn func(t *runtime.Task, args ...value.Object) (value.Object, error)) Effect {
	return Effect{Kind: "k8s", Scope: nsScope, Fn: func(t *runtime.Task, args ...value.Object) (value.Object, error) {
		n.Add(1)
		return fn(t, args...)
	}}
}

func applied(t *runtime.Task, args ...value.Object) (value.Object, error) {
	return &value.String{Value: "applied to " + args[0].Inspect()}, nil
}

var staging = runtime.Capability{Kind: "k8s", Scope: "staging"}

// The acceptance test of 15-phase8-tooling.md §8.2, through the
// embedding API: denied without the grant, allowed with it.
func TestRunHostEffect(t *testing.T) {
	var n atomic.Int64
	opts := Options{Effects: map[string]Effect{"k8s_apply": counted(&n, applied)}}
	_, err := Run(`k8s_apply("staging", "web.yaml")`, opts)
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != runtime.KindCapability || !strings.Contains(ae.Msg, `k8s("staging")`) {
		t.Fatalf("without a grant: err = %v", err)
	}
	if n.Load() != 0 {
		t.Fatal("Fn ran without a grant")
	}
	opts.Caps = []runtime.Capability{staging}
	got, err := Run(`k8s_apply("staging", "web.yaml")`, opts)
	if err != nil || got.Inspect() != "applied to staging" || n.Load() != 1 {
		t.Fatalf("with the grant: %v, %v after %d calls", got, err, n.Load())
	}
	_, err = Run(`k8s_apply("prod", "web.yaml")`, opts)
	if !errors.As(err, &ae) || ae.Kind != runtime.KindCapability || n.Load() != 1 {
		t.Fatalf("another namespace: err = %v after %d calls", err, n.Load())
	}
	got, err = Run(`let d = fn() { k8s_apply("staging", "m") }; await spawn d() with [k8s("staging")]`, opts)
	if err != nil || got.Inspect() != "applied to staging" {
		t.Fatalf("attenuated child: %v, %v", got, err)
	}
}

// A bad setup is an error before anything runs.
func TestRunRejectsBadHostSetup(t *testing.T) {
	ok := counted(new(atomic.Int64), applied)
	noKind := ok
	noKind.Kind = ""
	for _, tt := range []struct {
		name string
		opts Options
		want string
	}{
		{"effect without a kind", Options{Effects: map[string]Effect{"k8s_apply": noKind}}, "no capability kind"},
		{"effect hiding a builtin", Options{Effects: map[string]Effect{"read_file": ok}}, "builtin"},
		{"built-in kind declared", Options{Kinds: []runtime.KindSpec{{Kind: runtime.Net}}}, "built in"},
		{"grant of an undeclared kind", Options{Caps: []runtime.Capability{{Kind: "queue", Scope: "q"}}}, "unknown capability kind"},
		{"empty host grant", Options{Effects: map[string]Effect{"k8s_apply": ok}, Caps: []runtime.Capability{{Kind: "k8s"}}}, "empty scope"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			tt.opts.Out = &out
			_, err := Run(`puts("ran")`, tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, tt.want)
			}
			if out.Len() != 0 {
				t.Fatal("the program ran")
			}
		})
	}
}

// A kind the host declares with its own Covers: team("infra") covers
// team("infra/db"), and a child can be narrowed to it.
func TestRunHostKindCovers(t *testing.T) {
	sub := func(g, r string) bool { return r == g || strings.HasPrefix(r, g+"/") }
	team := Effect{Kind: "team", Scope: nsScope, Fn: applied}
	opts := Options{
		Kinds:   []runtime.KindSpec{{Kind: "team", Covers: sub}},
		Effects: map[string]Effect{"page": team},
		Caps:    []runtime.Capability{{Kind: "team", Scope: "infra"}},
	}
	src := `let p = fn(t) { page(t) }; [page("infra/db"), await spawn p("infra/db/eu") with [team("infra/db")]]`
	got, err := Run(src, opts)
	if err != nil || got.Inspect() != "[applied to infra/db, applied to infra/db/eu]" {
		t.Fatalf("got %v, %v", got, err)
	}
	_, err = Run(`let p = fn(t) { page(t) }; await spawn p("infra/web") with [team("infra/db")]`, opts)
	if err == nil || !strings.Contains(err.Error(), `team("infra/web")`) {
		t.Fatalf("a narrowed child reached infra/web: %v", err)
	}
}

// Budgets: every call settles the steps before it, so a spent steps
// budget stops the calls; a result is charged to alloc_mb.
func TestRunHostEffectBudgets(t *testing.T) {
	var n atomic.Int64
	opts := Options{Caps: []runtime.Capability{staging}, Limits: runtime.Limits{Steps: 2000}}
	opts.Effects = map[string]Effect{"k8s_apply": counted(&n, applied)}
	src := `let loop = fn(i) { k8s_apply("staging", "m"); loop(i + 1) }; loop(0)`
	_, err := Run(src, opts)
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != runtime.KindBudget || n.Load() == 0 || n.Load() > 2000 {
		t.Fatalf("err = %v after %d calls", err, n.Load())
	}
	big := func(t *runtime.Task, args ...value.Object) (value.Object, error) {
		return &value.Array{Elements: []value.Object{&value.String{Value: strings.Repeat("x", 2<<20)}}}, nil
	}
	opts = Options{Caps: []runtime.Capability{staging}, Effects: map[string]Effect{"big": counted(&n, big)}}
	_, err = Run(`scope (alloc_mb: 1) { big("staging") }`, opts)
	if !errors.As(err, &ae) || ae.Kind != runtime.KindBudget || !strings.Contains(ae.Msg, "alloc") {
		t.Fatalf("a 2 MiB result under alloc_mb: 1: err = %v", err)
	}
}

// Cancellation reaches Fn through t.Ctx: a timeout and a failing sibling
// both end a call that waits, and nothing outlives the run. The second
// script fails only once both waits have reached Fn (both), not after a
// sleep; the first never blocks on Fn having been reached, since under
// load its timeout can fire before the call.
func TestRunHostEffectIsCancelled(t *testing.T) {
	var n, saw atomic.Int64
	arrived := make(chan struct{}, 2)
	wait := func(t *runtime.Task, args ...value.Object) (value.Object, error) {
		arrived <- struct{}{}
		<-t.Ctx.Done()
		saw.Add(1)
		return nil, context.Cause(t.Ctx)
	}
	both := func(t *runtime.Task, args ...value.Object) (value.Object, error) {
		for i := 0; i < 2; i++ {
			select {
			case <-arrived:
			case <-t.Ctx.Done():
				return nil, context.Cause(t.Ctx)
			}
		}
		return nil, nil
	}
	opts := Options{Caps: []runtime.Capability{staging}, Effects: map[string]Effect{
		"wait": counted(&n, wait), "both": counted(new(atomic.Int64), both)}}
	before := goruntime.NumGoroutine()
	_, err := Run(`scope (timeout_ms: 100) { wait("staging") }`, opts)
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != runtime.KindBudget || !strings.Contains(ae.Msg, "timeout") {
		t.Fatalf("timeout: err = %v", err)
	}
	// Under load the timeout can fire before wait reaches Fn: then Fn never
	// ran, which is also right. Either way no call outlived the scope.
	if n.Load() != saw.Load() {
		t.Fatalf("timeout: %d calls, %d saw the cancellation", n.Load(), saw.Load())
	}
	select {
	case <-arrived:
	default:
	}
	n.Store(0)
	saw.Store(0)
	src := `let w = fn() { wait("staging") }; scope { spawn w() with [k8s("staging")]; spawn w() with [k8s("staging")]; both("staging"); 1 / 0 }`
	_, err = Run(src, opts)
	if err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("fail fast: err = %v", err)
	}
	if n.Load() != 2 || saw.Load() != 2 {
		t.Fatalf("%d calls, %d saw the cancellation; want 2 and 2", n.Load(), saw.Load())
	}
	noLeak(t, before, src)
}

// Many tasks call one effect at once, under -race.
func TestRunHostEffectConcurrent(t *testing.T) {
	var n atomic.Int64
	opts := Options{Caps: []runtime.Capability{staging}, Effects: map[string]Effect{"k8s_apply": counted(&n, applied)}}
	src := `let d = fn(i) { k8s_apply("staging", "m") }; let ts = for i in ` + zeroes(50) + ` { spawn d(i) with [k8s("staging")] }; len(await ts)`
	got, err := Run(src, opts)
	if err != nil || got.Inspect() != "50" || n.Load() != 50 {
		t.Fatalf("got %v, %v after %d calls", got, err, n.Load())
	}
}

// A panicking host effect is an error, never a panic in the host.
func TestRunHostEffectPanics(t *testing.T) {
	boom := func(t *runtime.Task, args ...value.Object) (value.Object, error) { panic("host bug") }
	opts := Options{Caps: []runtime.Capability{staging}, Effects: map[string]Effect{"boom": counted(new(atomic.Int64), boom)}}
	_, err := Run(`boom("staging")`, opts)
	if err == nil || !strings.Contains(err.Error(), "host bug") {
		t.Fatalf("err = %v", err)
	}
}

func zeroes(n int) string { return "[" + strings.TrimSuffix(strings.Repeat("0, ", n), ", ") + "]" }
