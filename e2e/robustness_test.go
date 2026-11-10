package e2e

import (
	"strconv"
	"testing"
)

// Robustness: no script makes the binary crash with a Go panic, a Go
// deadlock report or a stack overflow. Each must end with a clean
// "error: ..." line and exit code 1.
var goCrash = []string{"fatal error", "goroutine ", "panic:", "deadlock"}

// I4: a task that panics while its child is blocked cancels that child
// before the process exits; nothing after the child's blocking call runs.
func TestPanicInTaskCancelsGrandchild(t *testing.T) {
	t.Parallel()
	s := spec{
		script:    "robust_panic_grandchild.arv",
		allow:     loopback,
		vars:      map[string]string{"MS": strconv.Itoa(blockMS)},
		timeout:   failFastKill,
		code:      1,
		stderrHas: []string{"error: task root/0 panicked: runtime error: integer divide by zero"},
		stderrNot: goCrash,
	}
	res := check(t, s)
	if res.duration > failFastLimit {
		t.Errorf("run took %s, limit %s\n%s", res.duration, failFastLimit, res)
	}
	end, ok := waitSlowEnd(t, failFastLimit)
	switch {
	case !ok:
		t.Fatal("server never saw the grandchild's /slow request end")
	case !end.cancelled:
		t.Fatalf("grandchild's /slow ran to completion (%s) instead of being cancelled", end.after)
	}
	if n := hits(t, "/mark"); n != 0 {
		t.Errorf("grandchild ran past its cancelled request: %d /mark requests", n)
	}
}

func TestRobustness(t *testing.T) {
	t.Parallel()
	runAll(t, []spec{
		{name: "panic in a task", script: "robust_panic_task.arv", code: 1,
			stderrHas: []string{"error: task root/0 panicked: runtime error: integer divide by zero"}, stderrNot: goCrash},
		{name: "panic at the root", script: "robust_divzero_root.arv", code: 1,
			stderrHas: []string{"error: task root panicked: runtime error: integer divide by zero"}, stderrNot: goCrash},
	})
}
