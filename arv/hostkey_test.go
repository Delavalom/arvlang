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

// A host that echoes back 1023 hashes sharing one 1 MiB key string (the
// program pays for the key once and for each small hash) ends the run
// soon after Fn returns: the walk hashes the shared key once, not once
// per hash. Hashing it per hash took as long as the program's own loop
// (each {k: i} hashes k), uninterruptible between checks every 1024
// hashes, so the scope's timeout was overshot by that much: 39 s with a
// 16 MiB key. The bound is relative to the program's own time, so load
// slows both sides alike.
func TestRunHostEffectEchoesSharedBigKey(t *testing.T) {
	var fnStart atomic.Int64
	echo := Effect{Kind: "k8s", Scope: nsScope, Fn: func(t *runtime.Task, args ...value.Object) (value.Object, error) {
		fnStart.Store(time.Now().UnixNano())
		return args[1], nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	opts := Options{Limits: runtime.Limits{AllocBytes: 128 << 20}, Ctx: ctx,
		Caps: []runtime.Capability{staging}, Effects: map[string]Effect{"echo": echo}}
	src := `let dbl = fn(s, n) { if (n == 0) { s } else { dbl(s + s, n - 1) } }; let k = dbl("x", 20);
let xs = for i in ` + zeroes(1023) + ` { {k: i} };
scope (timeout_ms: 100) { echo("staging", xs) }`
	before := goruntime.NumGoroutine()
	start := time.Now()
	_, err := Run(src, opts)
	end := time.Now()
	if fnStart.Load() == 0 {
		t.Fatalf("Fn was never called: %v", err)
	}
	fn := time.Unix(0, fnStart.Load())
	build, after := fn.Sub(start), end.Sub(fn)
	t.Logf("program %v before Fn, Run returned %v after it: %v", build, after, err)
	// The fixed walk hashes 1 MiB once (milliseconds); the old one did
	// what the program's loop did. A quarter leaves room for stalls.
	if after > build/4 {
		t.Fatalf("Run returned %v after Fn, the program took %v before it", after, build)
	}
	noLeak(t, before, src)
}
