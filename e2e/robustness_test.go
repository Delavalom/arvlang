package e2e

import (
	"strconv"
	"testing"
	"time"
)

// Robustness: no script makes the binary crash with a Go panic, a Go
// deadlock report or a stack overflow. Each must end with a clean
// "error: ..." line and exit code 1.
var goCrash = []string{"fatal error", "goroutine ", "panic:", "deadlock"}

// I4: a task that fails (division by zero; before it was an Arv error it
// was a Go panic) while its child is blocked cancels that child before the
// process exits; nothing after the child's blocking call runs. The panic
// path itself is covered in the evaluator with a builtin that panics.
func TestPanicInTaskCancelsGrandchild(t *testing.T) {
	t.Parallel()
	s := spec{
		script:    "robust_panic_grandchild.arv",
		allow:     loopback,
		vars:      map[string]string{"MS": strconv.Itoa(blockMS)},
		timeout:   failFastKill,
		code:      1,
		stderrHas: []string{"error: division by zero"},
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

// Only the spawning task may await a handle. Each script would otherwise
// leave tasks waiting on each other forever: the Go runtime reported a
// deadlock, or (with a network connection open) the process hung.
func TestAwaitCycles(t *testing.T) {
	t.Parallel()
	await := func(name, script, msg string) spec {
		return spec{name: name, script: script, allow: loopback, timeout: 10 * time.Second,
			code: 1, stderrHas: []string{msg}, stderrNot: goCrash}
	}
	runAll(t, []spec{
		await("self", "robust_await_self.arv", "await: task root/0 cannot await itself"),
		await("siblings", "robust_await_mutual.arv", "was not spawned by task root/"),
		await("child awaits parent", "robust_await_parent.arv", "await: task root/0 was not spawned by task root/0/0"),
	})
}

func TestRobustness(t *testing.T) {
	t.Parallel()
	runAll(t, []spec{
		{name: "division by zero in a task", script: "robust_divzero_task.arv", code: 1,
			stderrHas: []string{"error: division by zero"}, stderrNot: goCrash},
		{name: "division by zero at the root", script: "robust_divzero_root.arv", code: 1,
			stderrHas: []string{"error: division by zero"}, stderrNot: goCrash},
		{name: "empty block is nil", script: "robust_empty_block.arv", stdout: "nil\nnil\n"},
	})
}
