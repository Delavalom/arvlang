package evaluator

import (
	"fmt"
	"testing"
)

// A user error's message is the program's own string, charged once, when
// it was built. When it fails a fail_fast scope, every task the failure
// cancels formats the cause into its own error ("sleep cancelled: ...",
// "task X cancelled: ..."), and those errors live as long as the handles
// do. Each copy must be clipped, or the uncharged memory is (tasks x
// message size), which the program controls.
func TestCancellationDoesNotCopyAUserMessage(t *testing.T) {
	s := newSession(nil)
	got := s.run(t, `let d = fn(s, n) { if (n == 0) { s } else { d(s + s, n - 1) } };
		let big = d("aaaaaaaaaaaaaaaa", 12);
		let f = fn() { sleep(600000) };
		let g = fn() { let ts = [spawn f(), spawn f()]; await ts };
		let bomb = fn() { error(big) };
		let r = match scope { let ts = [spawn f(), spawn f(), spawn g(), spawn f()]; spawn bomb(); await ts } { ok(v) => 0, err(e) => [e["kind"], len(e["message"])] };
		r`)
	if got.Inspect() != "[user, 65536]" {
		t.Fatalf("got %s, want the user error in full", got.Inspect())
	}
	for i := range 4 {
		got := s.run(t, fmt.Sprintf(`match await ts[%d] { ok(v) => "ok", err(e) => [e["kind"], len(e["message"])] }`, i))
		var n int
		if _, err := fmt.Sscanf(got.Inspect(), "[cancelled, %d]", &n); err != nil || n > 2*clipLen {
			t.Errorf("handle %d: got %s, want a cancelled error with a clipped message (at most %d bytes)", i, got.Inspect(), 2*clipLen)
		}
	}
}
