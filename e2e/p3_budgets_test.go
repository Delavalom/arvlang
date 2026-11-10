package e2e

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Phase 3: budgets, through the real binary. Each test here is a row of
// the acceptance table in 10-phase3-budgets.md, or an invariant the phase
// touches (I4, I6). Error lines end with "(at FILE:LINE:COL)"; see the
// first deviation in p3-plan-1.md.

// budgetBound is how long a budget may take to stop a run, measured from
// inside the script. The design says 300 ms; 16-implementation-notes
// records 3 s so the tests survive load averages above 300. It still sits
// far below the 5 s sleep and the 10 s inner timeout.
const budgetBound = 3 * time.Second

// timedFromMark runs s, whose script hits /mark once before the part being
// timed, and returns the time from that mark to the end of the process.
func timedFromMark(t *testing.T, s spec) time.Duration {
	t.Helper()
	res := run(t, s)
	end := time.Now()
	expect(t, s, res)
	m := markTimes(t)
	if len(m) != 1 {
		t.Fatalf("server saw %d /mark requests, want 1\n%s", len(m), res)
	}
	return end.Sub(m[0])
}

func TestBudgetTimeout(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, outer, inner, col string }{
		{"timeout", "100", "sleep(5000)", "27"},
		// I6: the inner 10 s timeout cannot outlast the outer 100 ms one.
		{"nested timeout", "100", "scope (timeout_ms: 10000) { sleep(5000) }", "55"},
		// I6 the other way: the smaller inner limit is the one that trips.
		{"smaller inner timeout", "10000", "scope (timeout_ms: 100) { sleep(5000) }", "55"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			took := timedFromMark(t, spec{
				script: "p3_timeout.arv", vars: map[string]string{"OUTER": c.outer, "INNER": c.inner},
				allow: loopback, code: 1,
				stderrHas: []string{"error: budget exceeded: timeout (limit 100 ms) (at ", "p3_timeout.arv:4:" + c.col + ")\n"},
				stderrNot: cancellationNoise,
			})
			if took > budgetBound {
				t.Fatalf("a 100 ms timeout stopped the run after %s (bound %s)", took, budgetBound)
			}
		})
	}
}

// I4: when a scope's timeout fires, the task blocked in an effect is
// cancelled (the server sees its request end early) and runs nothing
// more. If the task's request starts only after the deadline, as can
// happen on a starved machine, the attempt says nothing about I4 and is
// retried with a longer timeout.
func TestBudgetTimeoutCancelsTasks(t *testing.T) {
	t.Parallel()
	for _, ms := range []int{300, 2000, 8000} {
		var started bool
		t.Run(strconv.Itoa(ms)+"ms", func(t *testing.T) {
			script := "p3_timeout_task.arv"
			res := run(t, spec{script: script, allow: loopback, timeout: 60 * time.Second,
				vars: map[string]string{"MS": strconv.Itoa(ms)}})
			if started = hits(t, "/slow") == 1; !started {
				t.Logf("the task started after the deadline; retrying with a longer timeout\n%s", res)
				return
			}
			expect(t, spec{code: 1, stderrHas: []string{
				"error: budget exceeded: timeout (limit " + strconv.Itoa(ms) + " ms) (at ", script + ":",
			}, stderrNot: cancellationNoise}, res)
			end, ok := waitSlowEnd(t, failFastLimit)
			switch {
			case !ok:
				t.Fatal("server never saw its /slow request end")
			case !end.cancelled:
				t.Fatalf("/slow ran to completion (%s) instead of being cancelled", end.after)
			case end.after > time.Duration(ms)*time.Millisecond+failFastLimit:
				t.Fatalf("/slow was cancelled only after %s", end.after)
			}
			if n := hits(t, "/mark"); n != 0 {
				t.Fatalf("the cancelled task ran on: %d /mark requests", n)
			}
		})
		if started {
			return
		}
	}
	t.Fatal("the task never started before its scope's deadline")
}

func TestBudgetSteps(t *testing.T) {
	t.Parallel()
	check(t, spec{
		script: "p3_steps.arv", code: 1,
		stderrHas: []string{"error: budget exceeded: steps (limit 1000) (at ", "p3_steps.arv:2:"},
	})
}

// Steps are one pool for every task under the scope: four tasks of about
// 400 steps fit in 5000 and not in 1000, though one alone fits in 1000.
func TestBudgetStepsSharedByTasks(t *testing.T) {
	t.Parallel()
	runAll(t, []spec{
		{name: "one task fits", script: "p3_steps_shared.arv",
			vars: map[string]string{"STEPS": "1000", "TASKS": "[1]"}, stdout: "[0]\n"},
		{name: "four tasks do not", script: "p3_steps_shared.arv",
			vars: map[string]string{"STEPS": "1000", "TASKS": "[1, 2, 3, 4]"}, code: 1,
			stderrHas: []string{"error: budget exceeded: steps (limit 1000) (at ", "p3_steps_shared.arv:"}},
		{name: "four tasks fit a larger budget", script: "p3_steps_shared.arv",
			vars: map[string]string{"STEPS": "5000", "TASKS": "[1, 2, 3, 4]"}, stdout: "[0, 0, 0, 0]\n"},
	})
}

// items is an Arv array literal of n integers.
func items(n int) string {
	s := make([]string, n)
	for i := range s {
		s[i] = strconv.Itoa(i)
	}
	return "[" + strings.Join(s, ", ") + "]"
}

// Acceptance: 50 tasks of 100 ms under tasks: 5. spawn blocks while the
// scope is full, so the server never holds more than 5 requests, and the
// 50 take at least 10 rounds of 100 ms.
func TestBudgetTasksBackpressure(t *testing.T) {
	t.Parallel()
	check(t, spec{
		script: "p3_backpressure.arv", allow: loopback, stdout: "done\n",
		vars: map[string]string{"TASKS": "5", "ITEMS": items(50)},
	})
	f := flightStats(t, "bp")
	if f.total != 50 {
		t.Fatalf("server saw %d requests, want 50", f.total)
	}
	if f.peak > 5 {
		t.Fatalf("%d requests in flight at once under tasks: 5", f.peak)
	}
	if span := f.last.Sub(f.first); span < time.Second {
		t.Fatalf("50 tasks of 100 ms, 5 at a time, took %s on the server; want at least 1 s", span)
	}
}

// I6 for task slots: nested in the same task, the smaller limit holds,
// whichever scope sets it.
func TestBudgetNestedTaskSlots(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ outer, inner, want int }{{2, 50, 2}, {50, 3, 3}} {
		t.Run(fmt.Sprintf("outer %d inner %d", c.outer, c.inner), func(t *testing.T) {
			t.Parallel()
			check(t, spec{
				script: "p3_nested_slots.arv", allow: loopback, stdout: "done\n",
				vars: map[string]string{"OUTER": strconv.Itoa(c.outer), "INNER": strconv.Itoa(c.inner), "ITEMS": items(12)},
			})
			if f := flightStats(t, "nest"); f.total != 12 || f.peak > c.want {
				t.Fatalf("server saw %d requests, at most %d at once; want 12, at most %d", f.total, f.peak, c.want)
			}
		})
	}
}

func TestBudgetTasksNoDeadlock(t *testing.T) {
	t.Parallel()
	check(t, spec{script: "p3_no_deadlock.arv", allow: loopback, stdout: "[1, 1]\n"})
}

func TestBudgetAlloc(t *testing.T) {
	t.Parallel()
	runAll(t, []spec{
		{name: "1 MiB is too little", script: "p3_alloc.arv", vars: map[string]string{"MB": "1"}, code: 1,
			stderrHas: []string{"error: budget exceeded: alloc (limit 1048576 bytes) (at ", "p3_alloc.arv:2:"}},
		{name: "1 GiB is enough", script: "p3_alloc.arv", vars: map[string]string{"MB": "1024"}, stdout: "5000\n"},
	})
}

// Acceptance: positions. The design's "<file>:3:" prefix became a
// "(at <file>:3:" suffix (p3-plan-1.md, deviation 1).
func TestErrorPosition(t *testing.T) {
	t.Parallel()
	check(t, spec{
		script: "p3_position.arv", env: []string{"HOME=/nowhere"}, code: 1,
		stderrHas: []string{"error: capability denied: env(\"HOME\")", "p3_position.arv:3:1)\n"},
		stderrNot: []string{"/nowhere"},
	})
}

// The phase's done-when: fan-out is one expression, with no recursion.
func TestFanoutWithFor(t *testing.T) {
	t.Parallel()
	check(t, spec{script: "p3_fanout.arv", allow: loopback, stdout: "[200, 404, 200]\n"})
}

// I4 for steps and bytes: once a task has spent the scope's budget, the
// sibling blocked in an effect is cancelled, and the error is the budget's.
func TestSpentBudgetCancelsSiblings(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, opt, work, want string }{
		{"steps", "steps: 5000", "spin(0)", "budget exceeded: steps (limit 5000)"},
		{"alloc", "alloc_mb: 1", "grow([])", "budget exceeded: alloc (limit 1048576 bytes)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			failFast(t, spec{
				script: "p3_spent_cancels.arv", vars: map[string]string{"OPT": c.opt, "WORK": c.work},
				stderrHas: []string{"p3_spent_cancels.arv:"}, stderrNot: cancellationNoise,
			}, "error: "+c.want+" (at ")
			end, ok := waitSlowEnd(t, failFastLimit)
			if !ok || !end.cancelled {
				t.Fatalf("the blocked sibling's request was not cancelled (seen: %v, %+v)", ok, end)
			}
		})
	}
}
