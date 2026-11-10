package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// I5: the first failure in a scope cancels every sibling task and the code
// inside the scope, and the scope then evaluates to that failure.
//
// Every case blocks something for blockMS, far longer than it may take:
//   - failFastKill kills a run that was not cancelled (the harness then
//     fails the test), long before blockMS or the 30 s HTTP client timeout.
//   - failFastLimit is the wall time a run may take, start-up included. A
//     cancelled run takes well under a second on an idle machine; 10 s
//     leaves room for a heavily loaded one and is still a third of the
//     fastest uncancelled run (30 s).
//
// Ordering is enforced on the server: the failing task waits at a /wait
// gate until the sibling has reached the point where it blocks.
const (
	blockMS       = 60_000
	failFastKill  = 25 * time.Second
	failFastLimit = 10 * time.Second
)

// failFast runs s, which must fail with first (the first error) and finish
// within failFastLimit. No sibling may get past its blocking call: their
// follow-up /mark requests must never reach the server.
func failFast(t *testing.T, s spec, first string) {
	t.Helper()
	s.allow = loopback
	s.timeout = failFastKill
	if s.vars == nil {
		s.vars = map[string]string{}
	}
	s.vars["MS"] = strconv.Itoa(blockMS)
	s.code = 1
	s.stderrHas = append(s.stderrHas, first)
	res := run(t, s)
	expect(t, s, res)
	if res.duration > failFastLimit {
		t.Errorf("fail-fast run took %s, limit %s\n%s", res.duration, failFastLimit, res)
	}
	if n := hits(t, "/mark"); n != 0 {
		t.Errorf("server saw %d /mark requests from code after a blocking call, want 0", n)
	}
}

// What a cancelled sibling reports must never replace the first failure.
var cancellationNoise = []string{"sleep cancelled", "context canceled", "cancelled while awaiting", "cancelled:"}

func TestFailFastSleepingSiblings(t *testing.T) {
	t.Parallel()
	failFast(t, spec{
		script:    "failfast_sleep.arv",
		stderrNot: cancellationNoise,
		verify: func(t *testing.T) {
			if n := hits(t, "/arrive"); n != 2 {
				t.Errorf("server saw %d siblings fall asleep, want 2", n)
			}
		},
	}, "error: identifier not found: boom_while_sleeping")
}

// The sibling's request is in flight (the server is holding it) when the
// other task fails. Cancelling the task must cancel the request, and the
// server must see the client go away.
func TestFailFastHTTPSibling(t *testing.T) {
	t.Parallel()
	failFast(t, spec{script: "failfast_slow.arv", stderrNot: cancellationNoise},
		"error: identifier not found: boom_while_fetching")
	end, ok := waitSlowEnd(t, failFastLimit)
	switch {
	case !ok:
		t.Fatal("server never saw its /slow request end")
	case !end.cancelled:
		t.Fatalf("/slow ran to completion (%s) instead of being cancelled", end.after)
	case end.after > failFastLimit:
		t.Fatalf("/slow was cancelled only after %s, limit %s", end.after, failFastLimit)
	}
}

// A sibling that never makes an effect is stopped by Tick.
func TestFailFastCPUSibling(t *testing.T) {
	t.Parallel()
	failFast(t, spec{
		script:    "failfast_cpu.arv",
		stderrNot: cancellationNoise,
		verify: func(t *testing.T) {
			if n := hits(t, "/arrive"); n != 1 {
				t.Errorf("server saw %d spinning siblings start, want 1", n)
			}
		},
	}, "error: identifier not found: boom_while_spinning")
}

// The body of the scope (here the whole program) is cancelled too. The
// scope then fails with the body's own error, which carries the first
// failure as its cause.
func TestFailFastScopeBody(t *testing.T) {
	t.Parallel()
	const first = "identifier not found: boom_in_child"
	cases := []struct {
		name string
		body func(t *testing.T) string // the body's blocking statement
		gate string                    // the child fails after 1 arrival here; "" = at once
		err  func(t *testing.T) string // the error stderr must carry
	}{
		{
			// Before and inside fib the body runs pure code: only Tick can stop it.
			name: "computing",
			body: func(*testing.T) string { return "fib(40)" },
			err:  func(*testing.T) string { return "error: task root cancelled: " + first },
		},
		{
			// Without a gate the body may be stopped by Tick just before sleep
			// ("task root cancelled: ...") or inside it ("sleep cancelled: ...").
			name: "sleeping",
			body: func(*testing.T) string { return "sleep(" + strconv.Itoa(blockMS) + ")" },
			err:  func(*testing.T) string { return "cancelled: " + first },
		},
		{
			// The gate holds the child until the body's request is on the server.
			name: "fetching",
			body: func(t *testing.T) string { return `http_get("` + slowURL(t) + `")` },
			gate: "slow",
			err:  func(t *testing.T) string { return "error: http_get: Get \"" + slowURL(t) + "\": " + first },
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			n := 0
			if c.gate != "" {
				n = 1
			}
			failFast(t, spec{
				script: "failfast_body.arv",
				vars:   map[string]string{"BODY": c.body(t), "GATE": c.gate, "N": strconv.Itoa(n)},
			}, c.err(t))
			if c.gate == "slow" {
				if end, ok := waitSlowEnd(t, failFastLimit); !ok || !end.cancelled {
					t.Fatalf("/slow was not cancelled: seen=%v end=%+v", ok, end)
				}
			}
		})
	}
}

// slowURL is a /slow request for this test that blocks for blockMS.
func slowURL(t *testing.T) string {
	return srv.URL + "/slow?id=" + testID(t) + "&ms=" + strconv.Itoa(blockMS)
}

// A failed explicit scope evaluates to the child's error, so nothing after
// it runs.
func TestFailedScopeIsAnError(t *testing.T) {
	t.Parallel()
	res := check(t, spec{
		script: "scope_error_value.arv",
		code:   1, stderrHas: []string{"error: identifier not found: boom_in_scope"},
		stdoutNot: []string{"never the value", "unreachable"},
	})
	if strings.Count(res.stderr, "\n") != 1 {
		t.Fatalf("want exactly one error line\n%s", res)
	}
}
