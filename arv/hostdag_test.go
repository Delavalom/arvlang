package arv

import (
	"context"
	goruntime "runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func echoed(t *runtime.Task, args ...value.Object) (value.Object, error) {
	return value.NewHash(map[string]value.Object{"applied": args[1], "ok": &value.Boolean{Value: true}}), nil
}

// A host that echoes its argument back, given the program's own DAG (2^40
// paths, 41 nodes), returns at once: the result is walked once per node,
// so neither the timeout, the context nor alloc_mb has anything to stop.
func TestRunHostEffectEchoesDAG(t *testing.T) {
	var n atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	opts := Options{Ctx: ctx, Caps: []runtime.Capability{staging},
		Effects: map[string]Effect{"k8s_apply": counted(&n, echoed)},
		Limits:  runtime.Limits{AllocBytes: 1 << 20}}
	src := `let grow = fn(a, n) { if (n == 0) { a } else { grow([a, a], n - 1) } };
let m = grow([1], 40); let r = k8s_apply("staging", m); [r["ok"] == true, r["applied"] == m]`
	before := goruntime.NumGoroutine()
	done := make(chan struct{})
	var got value.Object
	var err error
	go func() { got, err = Run(src, opts); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Minute):
		t.Fatal("Run still running after 2 minutes")
	}
	if err != nil || got.Inspect() != "[true, true]" || n.Load() != 1 {
		t.Fatalf("got %v, %v after %d calls", got, err, n.Load())
	}
	noLeak(t, before, src)
}
