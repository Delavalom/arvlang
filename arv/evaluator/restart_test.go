package evaluator

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// runPolicy runs input as the root task holding caps, with l as the run's
// budget and p as the root scope's policy, as RunLimited would if the
// root scope had a supervise option. It returns the result and what puts
// printed.
func runPolicy(t *testing.T, p runtime.Policy, l runtime.Limits, input string, binds map[string]value.BuiltinFunction, caps ...runtime.Capability) (value.Object, string) {
	t.Helper()
	program := parse(t, input)
	env := value.NewEnvironment()
	for name, fn := range binds {
		env.Set(name, &value.Builtin{Fn: fn})
	}
	var out bytes.Buffer
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(caps...), &out)
	in := New(root)
	s, b := newScope(root.Ctx, l), runtime.Root(l)
	s.SetPolicy(p)
	got := in.withScope(frame{s, b}, func() value.Object { return in.Eval(program, env) })
	in.releaseDepth(b, 0)
	return got, out.String()
}

// restartOn is a restart policy with a 1 ms backoff (unless set).
func restartOn(retries int, backoff time.Duration, kinds ...string) runtime.Policy {
	p := runtime.Policy{Strategy: runtime.Restart, Retries: retries, Backoff: backoff, RetryOn: map[string]bool{}}
	for _, k := range kinds {
		p.RetryOn[k] = true
	}
	return p
}

// flakyServer fails its first fails requests with 500, then answers "up".
func flakyServer(t *testing.T, fails int32) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) <= fails {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, "up")
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// check is an Arv function that turns a status other than 200 into a user
// error.
const check = `let check = fn(url) { let r = http_get(url); if (r["status"] == 200) { r["body"] } else { error("not ok") } };`

// Acceptance: restart recovers. Two 500s, then success: 3 hits.
func TestRestartRecovers(t *testing.T) {
	srv, hits := flakyServer(t, 2)
	got, _ := runPolicy(t, restartOn(3, time.Millisecond, "effect", "user"), runtime.Limits{},
		check+fmt.Sprintf(`await spawn check(%q) with [net("127.0.0.1")]`, srv.URL), nil, grant(t, runtime.Net, "127.0.0.1"))
	if got.Inspect() != "up" || hits.Load() != 3 {
		t.Fatalf("got %s after %d hits, want up after 3", got.Inspect(), hits.Load())
	}
}

// Acceptance: no retry on capability. The task runs once.
func TestRestartSkipsCapabilityErrors(t *testing.T) {
	got, out := runPolicy(t, restartOn(3, time.Millisecond, "effect", "user"), runtime.Limits{},
		`let f = fn() { puts("attempt"); getenv("ARV_RESTART_X") }; await spawn f()`, nil)
	wantKind(t, got, runtime.KindCapability)
	if out != "attempt\n" {
		t.Fatalf("output %q, want one attempt", out)
	}
}

// Acceptance: backoff. 100 ms, two failures, then success: at least 300 ms.
func TestRestartBackoff(t *testing.T) {
	srv, _ := flakyServer(t, 2)
	start := time.Now()
	got, _ := runPolicy(t, restartOn(3, 100*time.Millisecond, "user"), runtime.Limits{},
		check+fmt.Sprintf(`await spawn check(%q) with [net("127.0.0.1")]`, srv.URL), nil, grant(t, runtime.Net, "127.0.0.1"))
	if got.Inspect() != "up" {
		t.Fatalf("got %s", got.Inspect())
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Fatalf("took %s, want at least 300ms (100 + 200)", elapsed)
	}
}

// Out of retries, the last attempt's error fails the scope, named after
// that attempt (root/0, root/0#2, ...); by default only effect errors
// restart.
func TestRestartGivesUpWithTheLastError(t *testing.T) {
	src := `let f = fn() { puts("attempt"); error("down") }; await spawn f()`
	got, out := runPolicy(t, restartOn(3, time.Millisecond, "user"), runtime.Limits{}, src, nil)
	if e := wantKind(t, got, runtime.KindUser); e.Message != "down" || e.Task != "root/0#4" {
		t.Fatalf("got %q in %s, want down in root/0#4", e.Message, e.Task)
	}
	if out != strings.Repeat("attempt\n", 4) {
		t.Fatalf("output %q, want 4 attempts", out)
	}
	if _, out = runPolicy(t, runtime.RestartPolicy(), runtime.Limits{}, src, nil); out != "attempt\n" {
		t.Fatalf("the default policy restarted a user error: %q", out)
	}
}

// Each attempt runs against the environment frozen at the spawn (I8), not
// the parent's later rebinding.
func TestRestartSeesTheSpawnTimeEnvironment(t *testing.T) {
	got, out := runPolicy(t, restartOn(2, time.Millisecond, "user"), runtime.Limits{}, `let x = 1;
		let f = fn() { wait(); puts(x); error("again") };
		let t = spawn f(); let x = 2; open(); await t`, gate())
	wantKind(t, got, runtime.KindUser)
	if out != "1\n1\n1\n" {
		t.Fatalf("output %q, want x = 1 in all 3 attempts", out)
	}
}

// Restarting never refunds a budget: attempts share the scope's steps and
// bytes, so a task that keeps failing runs out of them before its retries.
func TestRestartNeverResetsBudgets(t *testing.T) {
	for name, tt := range map[string]struct {
		l   runtime.Limits
		src string
	}{
		"steps": {runtime.Limits{Steps: 4000}, `let burn = fn(n) { if (n == 0) { 0 } else { burn(n - 1) } };
			let f = fn() { puts("attempt"); burn(100); error("again") }; await spawn f()`},
		"alloc": {runtime.Limits{AllocBytes: 1 << 20}, `let d = fn(s, n) { if (n == 0) { s } else { d(s + s, n - 1) } };
			let f = fn() { puts("attempt"); d("aaaaaaaaaaaaaaaa", 13); error("again") }; await spawn f()`},
	} {
		got, out := runPolicy(t, restartOn(20, time.Millisecond, "user"), tt.l, tt.src, nil)
		wantKind(t, got, runtime.KindBudget)
		if n := strings.Count(out, "attempt"); n < 2 || n > 10 {
			t.Errorf("%s: %d attempts, want a few, stopped by the budget", name, n)
		}
	}
}

// A timeout ends the restarts, backoff included.
func TestRestartWithinTimeout(t *testing.T) {
	start := time.Now()
	got, _ := runPolicy(t, restartOn(1000, 50*time.Millisecond, "user"), runtime.Limits{Timeout: 200 * time.Millisecond},
		`let f = fn() { error("again") }; await spawn f()`, nil)
	if e := wantKind(t, got, runtime.KindBudget); !strings.Contains(e.Message, "timeout") {
		t.Fatalf("got %q, want the timeout", e.Message)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("took %s", elapsed)
	}
}

// While one task restarts, its siblings carry on; an error that is not
// restarted fails the scope fast and cancels them (I5).
func TestRestartSiblings(t *testing.T) {
	var calls atomic.Int32
	flaky := func(*runtime.Task, ...value.Object) value.Object {
		if calls.Add(1) <= 2 {
			return &value.Error{Message: "flaky", Kind: runtime.KindEffect}
		}
		return &value.String{Value: "ok"}
	}
	got, _ := runPolicy(t, restartOn(3, time.Millisecond, "effect"), runtime.Limits{},
		`let n = fn(x) { x }; let a = spawn n(1); let b = spawn flaky(); let c = spawn n(3); [await a, await b, await c]`,
		map[string]value.BuiltinFunction{"flaky": flaky})
	if got.Inspect() != "[1, ok, 3]" || calls.Load() != 3 {
		t.Fatalf("got %s after %d calls, want [1, ok, 3] after 3", got.Inspect(), calls.Load())
	}
	start := time.Now()
	got, _ = runPolicy(t, restartOn(3, time.Millisecond, "effect", "user"), runtime.Limits{},
		`let slow = fn() { sleep(10000) }; let f = fn() { getenv("ARV_RESTART_X") }; spawn slow(); spawn f(); sleep(10000)`, nil)
	wantKind(t, got, runtime.KindCapability)
	if time.Since(start) > 5*time.Second {
		t.Fatal("a capability error did not cancel the restart scope")
	}
}

// Restarts leave no goroutine behind.
func TestRestartLeaksNothing(t *testing.T) {
	before := goruntime.NumGoroutine()
	got, _ := runPolicy(t, restartOn(3, time.Millisecond, "user"), runtime.Limits{},
		`let f = fn() { error("x") }; for n in [1, 2, 3, 4] { spawn f() }; 1`, nil)
	wantKind(t, got, runtime.KindUser)
	settled(t, before)
}
