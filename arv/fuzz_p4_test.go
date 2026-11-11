package arv

import (
	"context"
	"errors"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// superviseSeeds exercise Phase 4: error kinds, match, and each supervise
// strategy with its options, valid and not, nested and under budgets.
var superviseSeeds = []string{
	`match error("boom") { ok(v) => v, err(e) => [e["kind"], e["message"], e["task"], e["pos"]] }`,
	`match 5 { ok(v) => v + 1, err(e) => 0 }`,
	`match getenv("X") { ok(v) => v, err(e) => if (e["kind"] == "capability") { "denied" } else { "other" } }`,
	`match scope { let f = fn() { error("x") }; spawn f(); sleep(60000) } { ok(v) => 0, err(e) => e["kind"] }`,
	`let f = fn(n) { if (n == 2) { error("two") } else { n } };
	 scope (supervise: "collect") { let ts = [spawn f(1), spawn f(2), spawn f(3)]; for t in ts { match await t { ok(v) => v, err(e) => e["kind"] } } }`,
	`scope (supervise: "collect") { let bad = fn() { error("x") }; spawn bad(); 1 }`,
	`scope (supervise: "collect") { let bad = fn() { error("x") }; await [spawn bad(), spawn bad()] }`,
	`scope (supervise: "restart", retries: 2, backoff_ms: 1, retry_on: ["user"]) { let f = fn() { error("no") }; await spawn f() }`,
	`let f = fn() { getenv("NOPE") }; scope (supervise: "restart", retries: 3, backoff_ms: 1) { await spawn f() }`,
	`let f = fn() { read_file("/nope") }; scope (supervise: "restart", retries: 1000, backoff_ms: 30000) { await spawn f() }`,
	`let g = fn() { error("in") };
	 let inner = fn() { scope (supervise: "restart", retries: 2, backoff_ms: 1, retry_on: ["user"]) { await spawn g() } };
	 scope (supervise: "collect") { let ts = [spawn inner(), spawn len("ab")]; for t in ts { match await t { ok(v) => v, err(e) => e["message"] } } }`,
	`let burn = fn(n) { if (n == 0) { 0 } else { burn(n - 1) } }; let f = fn() { burn(300); error("again") };
	 scope (steps: 2000, supervise: "restart", retries: 100, backoff_ms: 1, retry_on: ["user"]) { await spawn f() }`,
	`let x = 1; let f = fn() { x }; let t = spawn f(); let x = 2; [await t, x]`,
	`let x = 1; let f = fn() { let g = fn() { x }; await spawn g() }; let t = spawn f(); let x = 2; await t`,
	`scope (supervise: "nope") { 1 }`,
	`scope (retries: 2) { 1 }`,
	`scope (supervise: "restart", retry_on: []) { 1 }`,
	`scope (supervise: "restart", retry_on: ["budget"]) { 1 }`,
	`scope (supervise: "restart", retries: 0) { 1 }`,
	`scope (supervise: "restart", backoff_ms: 30001) { 1 }`,
	`scope (supervise: "collect", supervise: "restart") { 1 }`,
	`scope (alloc_mb: 1, supervise: "collect") { let f = fn() { error("x") }; for i in [1, 2, 3, 4, 5, 6, 7, 8] { match await spawn f() { ok(v) => v, err(e) => e } } }`,
	`scope (timeout_ms: 20, supervise: "restart", backoff_ms: 5000, retry_on: ["user"]) { let f = fn() { error("u") }; await spawn f() }`,
	`match match 1 / 0 { ok(v) => v, err(e) => e } { ok(h) => h["kind"], err(e) => 0 }`,
}

var errorKinds = map[string]bool{
	runtime.KindCapability: true, runtime.KindBudget: true, runtime.KindCancelled: true,
	runtime.KindEffect: true, runtime.KindRuntime: true, runtime.KindUser: true,
}

// FuzzRunSupervise runs any program, seeded with Phase 4 programs, under
// small budgets with one directory granted. Beyond FuzzRun's checks (ends
// in time, no panic into the host, a position on every Arv error, no
// goroutine left behind) every error that reaches the host has one of the
// six kinds. With hostCancel the host cancels the run after 20 ms instead
// of a timeout ending it.
func FuzzRunSupervise(f *testing.F) {
	for _, s := range superviseSeeds {
		f.Add(s, false)
	}
	f.Add(superviseSeeds[9], true)
	dir := realDir(f.TempDir())
	caps := []runtime.Capability{{Kind: runtime.FSRead, Scope: dir}}
	limits := runtime.Limits{Timeout: 200 * time.Millisecond, Steps: 50000, AllocBytes: 4 << 20, LiveTasks: 20}
	f.Fuzz(func(t *testing.T, src string, hostCancel bool) {
		src = strings.ReplaceAll(src, "/nope", dir+"/nope")
		before := goruntime.NumGoroutine()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		opts := Options{Ctx: ctx, Caps: caps, Limits: limits}
		if hostCancel {
			opts.Limits.Timeout = 0
			stop := time.AfterFunc(20*time.Millisecond, cancel)
			defer stop.Stop()
		}
		start := time.Now()
		_, err := Run(src, opts)
		if took := time.Since(start); took > 20*time.Second {
			t.Fatalf("run took %s: %q", took, src)
		}
		var ae *Error
		var pe *ParseError
		switch {
		case err == nil, errors.As(err, &pe):
		case errors.As(err, &ae):
			if !position.MatchString(ae.Pos) {
				t.Fatalf("error %q has position %q for %q", ae.Msg, ae.Pos, src)
			}
			if !errorKinds[ae.Kind] {
				t.Fatalf("error %q has kind %q for %q", ae.Msg, ae.Kind, src)
			}
		default:
			t.Fatalf("run of %q returned %T %v, want an *Error or *ParseError", src, err, err)
		}
		noLeak(t, before, src)
	})
}
