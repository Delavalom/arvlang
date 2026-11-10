package e2e

import (
	"strconv"
	"testing"
	"time"
)

// Phase 2: tasks, scopes and attenuation, through the real CLI. Timing is
// taken from the server (when /mark requests arrive), never from process
// wall time, so binary start-up on a loaded machine cannot skew it. Where
// order matters, the server's /wait gate enforces it instead of a sleep.

// loopback is the grant every networked script in this file needs.
var loopback = []string{"net=127.0.0.1"}

// sleep is timed by the server: the script hits /mark before and after
// sleeping, so process start-up time can't hide a sleep that returns early.
func TestSleep(t *testing.T) {
	t.Parallel()
	const ms = 100
	s := spec{
		script: "sleep.arv", vars: map[string]string{"MS": strconv.Itoa(ms)},
		allow: loopback, stdout: "slept\n",
	}
	res := run(t, s)
	expect(t, s, res)
	m := markTimes(t)
	if len(m) != 2 {
		t.Fatalf("server saw %d /mark requests, want 2", len(m))
	}
	if gap := m[1].Sub(m[0]); gap < ms*time.Millisecond {
		t.Fatalf("sleep(%d) took %s between marks", ms, gap)
	}
}

// Three tasks that each sleep run at the same time. Sleeps never end early,
// so run one after another the last mark would be at least 3*nap after the
// first. Run in parallel it is about 1*nap plus four local requests. The
// bound sits halfway between, leaving 1.5 s of scheduling slack on a loaded
// machine while still failing for any sequential run.
func TestParallelTasks(t *testing.T) {
	t.Parallel()
	const nap = time.Second
	s := spec{
		script: "par_sleep.arv", vars: map[string]string{"MS": strconv.Itoa(int(nap.Milliseconds()))},
		allow: loopback, stdout: "done\n",
	}
	expect(t, s, run(t, s))
	m := markTimes(t)
	if len(m) != 4 {
		t.Fatalf("server saw %d /mark requests, want 4 (start + 3 tasks)", len(m))
	}
	start, last := m[0], m[0]
	for i, at := range m[1:] {
		if gap := at.Sub(start); gap < nap {
			t.Errorf("task mark %d came %s after start, before its %s sleep ended", i, gap, nap)
		}
		if at.After(last) {
			last = at
		}
	}
	if span, limit := last.Sub(start), 5*nap/2; span >= limit {
		t.Fatalf("three %s sleeps took %s from start to last mark (limit %s): tasks did not run in parallel",
			nap, span, limit)
	}
}

func TestAwait(t *testing.T) {
	t.Parallel()
	const secret = "s3cret-A"
	runAll(t, []spec{
		{
			name: "returns the task's value", script: "await_value.arv",
			stdout: "5\nhello arv\n[1, two, true]\n15\n",
		},
		{
			// I2 too: with no with clause the child holds nothing, even
			// though the root was granted env.
			name: "awaiting a failed task fails the program", script: "await_error.arv",
			vars:  map[string]string{"NAME": "ARV_E2E_A"},
			env:   []string{"ARV_E2E_A=" + secret},
			allow: []string{"env=ARV_E2E_A"},
			code:  1,
			stderrHas: []string{
				`capability denied: env("ARV_E2E_A") in task root/0 (holds: [])`,
			},
			stderrNot: []string{secret},
		},
	})
}

// I2: a child holds only what it was given, and can only be given what its
// parent holds.
func TestAttenuation(t *testing.T) {
	t.Parallel()
	const secret = "s3cret-A"
	name := map[string]string{"NAME": "ARV_E2E_A"}
	env := []string{"ARV_E2E_A=" + secret}
	rootCaps := []string{"env=ARV_E2E_A", "net=127.0.0.1"}
	okHits := func(want int) func(t *testing.T) {
		return func(t *testing.T) {
			if got := hits(t, "/ok"); got != want {
				t.Errorf("server saw %d requests for /ok, want %d", got, want)
			}
		}
	}
	grandchild := func(label, gcaps, denial string) spec {
		return spec{
			name: "grandchild " + label, script: "attenuate_grandchild.arv",
			vars: map[string]string{"NAME": "ARV_E2E_A", "GCAPS": gcaps},
			env:  env, allow: rootCaps,
			code: 1, stderrHas: []string{denial}, stderrNot: []string{secret},
			verify: okHits(1),
		}
	}
	noncap := func(caps, why string) spec {
		return spec{
			name: "with " + caps + " is an error", script: "attenuate_noncap.arv",
			vars: map[string]string{"CAPS": caps}, allow: loopback,
			code: 1, stderrHas: []string{why}, verify: okHits(0),
		}
	}
	runAll(t, []spec{
		{
			// The child's network works (status 200), its env read is denied.
			name: "child loses what it was not given", script: "attenuate_child.arv",
			vars: name, env: env, allow: rootCaps,
			code:   1,
			stdout: secret + "\n200\n",
			stderrHas: []string{
				`capability denied: env("ARV_E2E_A") in task root/0 (holds: [net("127.0.0.1")])`,
			},
			verify: okHits(1),
		},
		{
			// The granted half must not run either: the spawn fails as a whole,
			// and the root stops at the spawn (its /mark never arrives).
			name: "asking for more than the parent holds fails at spawn", script: "attenuate_spawn_denied.arv",
			allow: loopback,
			code:  1,
			stderrHas: []string{
				`spawn in task root: capability denied: net("other.invalid") (holds: [net("127.0.0.1")])`,
			},
			verify: func(t *testing.T) {
				okHits(0)(t)
				if n := hits(t, "/mark"); n != 0 {
					t.Errorf("server saw %d /mark requests after a failed spawn, want 0", n)
				}
			},
		},
		grandchild("cannot regain a dropped capability", `[env("ARV_E2E_A")]`,
			`spawn in task root/1: capability denied: env("ARV_E2E_A") (holds: [net("127.0.0.1")])`),
		grandchild("given net cannot read env", `[net("127.0.0.1")]`,
			`capability denied: env("ARV_E2E_A") in task root/1/0 (holds: [net("127.0.0.1")])`),
		grandchild("given nothing holds nothing", `[]`,
			`capability denied: env("ARV_E2E_A") in task root/1/0 (holds: [])`),
		{
			name: "capability values from variables", script: "attenuate_var.arv",
			allow: loopback, stdout: "200\n200\n", verify: okHits(2),
		},
		noncap(`["net"]`, "spawn ... with: want CAPABILITY, got STRING"),
		noncap(`[net("127.0.0.1"), 5]`, "spawn ... with: want CAPABILITY, got INTEGER"),
		noncap(`5`, "spawn ... with: want ARRAY of capabilities, got INTEGER"),
	})
}

// I4: a scope, a task body and the whole program wait for every task
// started inside them, awaited or not.
func TestStructure(t *testing.T) {
	t.Parallel()
	marks := func(want int) func(t *testing.T) {
		return func(t *testing.T) {
			if got := hits(t, "/mark"); got != want {
				t.Errorf("server saw %d /mark requests, want %d", got, want)
			}
		}
	}
	runAll(t, []spec{
		{
			name: "scope waits for an unawaited child", script: "scope_waits.arv",
			allow: loopback, stdout: "child finished\nscope value\n", verify: marks(1),
		},
		{
			// /mark is sent before "late task done" is printed, so both prove
			// the task ran to the end before the process exited.
			name: "program waits for an unawaited top-level task", script: "program_waits.arv",
			allow: loopback, stdout: "main body done\nlate task done\n", verify: marks(1),
		},
		{
			name: "nested scopes", script: "nested_scopes.arv",
			stdout: "inner child done\nafter inner scope\n25\n30\n",
		},
	})
}

// 200 tasks at once: exact results, no crash. Cheap enough to run under
// ARV_E2E_RACE=1, which builds the binary itself with -race.
func TestManyTasks(t *testing.T) {
	t.Parallel()
	const n = 200
	sum := 0
	for i := 0; i < n; i++ {
		sum += i * i
	}
	check(t, spec{
		script: "scale.arv", vars: map[string]string{"N": strconv.Itoa(n)},
		stdout:    strconv.Itoa(n) + "\n" + strconv.Itoa(sum) + "\n",
		stderrNot: []string{"WARNING: DATA RACE"},
	})
}

func TestConcurrencyErrors(t *testing.T) {
	t.Parallel()
	noncall := func(expr string) spec {
		return spec{
			name: "spawn " + expr + " is a parse error", script: "spawn_noncall.arv",
			vars: map[string]string{"EXPR": expr},
			code: 1, stderrHas: []string{"error: parse error: spawn expects a function call, e.g. spawn f(x)"},
		}
	}
	runAll(t, []spec{
		{
			name: "await on a non-task", script: "await_nontask.arv",
			code: 1, stderrHas: []string{"error: await: want TASK, got INTEGER"},
		},
		noncall("5"),
		noncall("f"),
		noncall("f() + 1"),
		noncall("fn() { 1 }"),
	})
}
