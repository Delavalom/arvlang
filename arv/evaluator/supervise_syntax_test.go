package evaluator

import (
	"fmt"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// supervise: "restart" in a scope's options restarts its tasks, with the
// options' retries and retry_on; each scope has its own policy.
func TestRestartThroughOptions(t *testing.T) {
	srv, hits := flakyServer(t, 2)
	got, _ := runWith(t, check+fmt.Sprintf(`
		scope (supervise: "restart", retries: 3, backoff_ms: 1, retry_on: ["effect", "user"]) {
			await spawn check(%q) with [net("127.0.0.1")]
		}`, srv.URL), grant(t, runtime.Net, "127.0.0.1"))
	if got.Inspect() != "up" || hits.Load() != 3 {
		t.Fatalf("got %s after %d hits, want up after 3", got.Inspect(), hits.Load())
	}
	got, _ = runWith(t, `let f = fn(n) { error("again") };
		for n in [1, 2, 3] {
			match scope (supervise: "restart", retries: 3, backoff_ms: 1, retry_on: ["user"]) { await spawn f(n) } { ok(v) => v, err(e) => e["task"] }
		}`)
	if got.Inspect() != "[root/0#4, root/1#4, root/2#4]" {
		t.Fatalf("got %s", got.Inspect())
	}
	// Without retry_on, a user error is not restarted.
	_, out := runWith(t, `let f = fn() { puts("attempt"); error("down") };
		scope (supervise: "restart", backoff_ms: 1) { await spawn f() }`)
	if out != "attempt\n" {
		t.Fatalf("default retry_on restarted a user error: %q", out)
	}
}
