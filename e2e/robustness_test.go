package e2e

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Robustness: no script makes the binary crash with a Go panic, a Go
// deadlock report or a stack overflow. Each must end with a clean
// "error: ..." line and exit code 1.
var goCrash = []string{"fatal error", "goroutine ", "panic:", "deadlock"}

// I5 reaching a grandchild: a task that fails (division by zero) while its
// child is blocked in a request cancels that child; the server sees the
// request end early, and nothing after it runs. That the process also
// waits for the child (I4), and the Go-panic variant of this, are proven
// in the evaluator with builtins that ignore cancellation or panic.
func TestFailingTaskCancelsGrandchild(t *testing.T) {
	t.Parallel()
	s := spec{
		script:    "robust_fail_grandchild.arv",
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
		await("self", "robust_await_self.arv", "error: await: task root/0 cannot await itself"),
		await("siblings", "robust_await_mutual.arv", "error: await: task root/"),
		await("child awaits parent", "robust_await_parent.arv", "error: await: task root/0 was not spawned by task root/0/0 or its descendants"),
	})
}

func TestRobustness(t *testing.T) {
	t.Parallel()
	runAll(t, []spec{
		{name: "division by zero in a task", script: "robust_divzero_task.arv", code: 1,
			stderrHas: []string{"error: division by zero"}, stderrNot: goCrash},
		{name: "division by zero at the root", script: "robust_divzero_root.arv", code: 1,
			stderrHas: []string{"error: division by zero"}, stderrNot: goCrash},
		{name: "oversized sleep", script: "robust_sleep_overflow.arv", code: 1,
			stderrHas: []string{"error: sleep: ", "too long"}, stderrNot: goCrash},
		{name: "calls times nesting at the root", script: "robust_deep_calls.arv", code: 1,
			stderrHas: []string{"error: stack overflow: evaluation nested deeper than"}, stderrNot: goCrash},
		{name: "calls times nesting in a task", script: "robust_deep_calls_task.arv", code: 1,
			stderrHas: []string{"error: stack overflow: evaluation nested deeper than"}, stderrNot: goCrash},
		{name: "await a descendant's handle", script: "robust_await_descendant.arv", stdout: "8\n"},
		{name: "empty block is nil", script: "robust_empty_block.arv", stdout: "nil\nnil\n"},
		// I1: /etc/localtime is a symlink (into zoneinfo) on macOS and Linux.
		{name: "capability value does not resolve symlinks", script: "robust_cap_value.arv",
			stdout:    "fs_read(\"/etc/localtime\")\nfs_write(\"/var/root/.ssh/x\")\n",
			stdoutNot: []string{"zoneinfo", "/private"}},
		{name: "spawn denial shows the path as written", script: "robust_cap_denied.arv", code: 1,
			stderrHas: []string{`error: spawn in task root: capability denied: fs_read("/etc/localtime") (holds: [])`},
			stderrNot: append([]string{"zoneinfo", "/private"}, goCrash...)},
	})
}

// Deep nesting is a parse error. 6 MB of "[" used to overflow the parser's
// goroutine stack: "fatal error: stack overflow", exit code 2.
func TestDeepNesting(t *testing.T) {
	t.Parallel()
	const n = 6_000_000
	for name, src := range map[string]string{
		"arrays":      "puts(" + strings.Repeat("[", n) + ");",
		"parentheses": strings.Repeat("(", n) + "1" + strings.Repeat(")", n),
		"fn literals": strings.Repeat("fn() { ", 10_001) + "1" + strings.Repeat(" }", 10_001),
		"blocks":      strings.Repeat("if (true) { ", 10_001) + "1" + strings.Repeat(" }", 10_001),
		"right infix": strings.Repeat("1 + (", n/5) + "1" + strings.Repeat(")", n/5),
		"call args":   strings.Repeat("f(", n/2) + "1" + strings.Repeat(")", n/2),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "deep.arv")
			if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			check(t, spec{args: []string{"run", path}, code: 1,
				stderrHas: []string{"error: parse error: nesting"}, stderrNot: goCrash})
		})
	}
}

// I1/I3 at use: with no grants, a denied read_file or write_file shows the
// path as the script wrote it, never its symlink target (/etc/localtime
// points into zoneinfo) or an lstat error (under a mode-000 directory).
func TestUseTimeDenialRevealsNothing(t *testing.T) {
	t.Parallel()
	noperm := filepath.Join(t.TempDir(), "noperm")
	if err := os.Mkdir(noperm, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(noperm, 0o755) })
	hide := append([]string{"zoneinfo", "/private", "permission", "lstat", "canonical"}, goCrash...)
	var specs []spec
	for _, path := range []string{"/etc/localtime", filepath.Join(noperm, "x")} {
		specs = append(specs,
			spec{name: "read " + path, script: "read_file.arv", vars: map[string]string{"PATH": path},
				code: 1, stderrHas: []string{`error: capability denied: fs_read("` + path + `") in task root (holds: [])`}, stderrNot: hide},
			spec{name: "write " + path, script: "write_file.arv", vars: map[string]string{"PATH": path, "TEXT": "x"},
				code: 1, stderrHas: []string{`error: capability denied: fs_write("` + path + `") in task root (holds: [])`}, stderrNot: hide},
		)
	}
	runAll(t, specs)
}

// A spawned write_file to a FIFO with no reader, then a failure at the
// root, must end promptly instead of hanging in the child's open.
func TestWriteFileFIFODoesNotHang(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := mkfifo(filepath.Join(dir, "pipe")); err != nil {
		t.Skip("no FIFOs here:", err)
	}
	t.Cleanup(func() { // unblock a stuck writer, should one exist
		if f, err := os.OpenFile(filepath.Join(dir, "pipe"), os.O_RDWR, 0); err == nil {
			f.Close()
		}
	})
	res := check(t, spec{script: "robust_fifo_write.arv", vars: map[string]string{"DIR": dir},
		allow: []string{"fs_write={{DIR}}"}, timeout: failFastKill, code: 1, stderrHas: []string{"error: write_file: "}, stderrNot: goCrash})
	if res.duration > failFastLimit {
		t.Errorf("run took %s, limit %s\n%s", res.duration, failFastLimit, res)
	}
}

// A left-associative infix chain is not nesting: 100000 terms (the
// reviewer's 400 KB probe) parse and evaluate.
func TestLongInfixChain(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "chain.arv")
	src := "puts(1" + strings.Repeat(" + 1", 99_999) + ");"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	check(t, spec{args: []string{"run", path}, stdout: "100000\n"})
}
