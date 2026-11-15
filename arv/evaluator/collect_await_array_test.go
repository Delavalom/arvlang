package evaluator

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// await of an array stops at its first failure (as everywhere), so under
// collect the handles after it are not awaited by that await, and a later
// failure among them is reported when the scope closes, not dropped. To
// see every outcome, await each handle (TestCollect).
func TestCollectAwaitArrayLeavesLaterHandlesUnawaited(t *testing.T) {
	got, _ := runWith(t, `let bad = fn(m) { error(m) };
		scope (supervise: "collect") {
			let ts = [spawn bad("first"), spawn bad("second")];
			match await ts { ok(v) => v, err(e) => e["message"] }
		}`)
	e := wantKind(t, got, runtime.KindUser)
	if !strings.HasPrefix(e.Message, "1 unawaited task failures: second") {
		t.Fatalf("got %q, want the second task's failure reported as unawaited", e.Message)
	}
	got, _ = runWith(t, `let bad = fn(m) { error(m) };
		scope (supervise: "collect") {
			let ts = [spawn bad("first"), spawn bad("second")];
			for t in ts { match await t { ok(v) => v, err(e) => e["message"] } }
		}`)
	if got.Inspect() != "[first, second]" {
		t.Fatalf("got %s, want [first, second]", got.Inspect())
	}
}
