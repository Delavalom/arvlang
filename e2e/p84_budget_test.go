package e2e

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Phase 8.4, S2 and S3 through the binary: --steps cuts a program at the
// same node on both engines, --alloc-mb at the same value, and a timeout,
// a scope's timeout_ms and a failing sibling stop compiled CPU-bound code
// as promptly as they stop the tree-walker (I5, I6).

// p84Spin nests nine loops over ten elements (10^9 bodies): only a
// deadline or a cancel ends it.
const p84Spin = `let ten = [0, 0, 0, 0, 0, 0, 0, 0, 0, 0];
let spin = fn() { for a in ten { for b in ten { for c in ten { for d in ten { for e in ten { for f in ten { for g in ten { for h in ten { for i in ten { a + i } } } } } } } } } };
`

// stepsFit is the smallest --steps under which path runs to the end on
// engine e, found by bisection through the binary, and the stderr of a
// run with one step fewer.
func stepsFit(t *testing.T, e, path string) (int, string) {
	t.Helper()
	runN := func(n int) result {
		return onEngine(t, e, spec{args: []string{"run", "--steps", strconv.Itoa(n), path}})
	}
	fits := func(n int) bool {
		res := runN(n)
		switch {
		case res.code == 0:
			return true
		case strings.Contains(res.stderr, "budget exceeded: steps (limit "+strconv.Itoa(n)+")"):
			return false
		}
		t.Fatalf("%s, --steps %d: neither success nor the steps budget\n%s", e, n, res)
		return false
	}
	lo, hi := 1, 1<<20
	if !fits(hi) {
		t.Fatalf("%s: %s does not fit in %d steps", e, path, hi)
	}
	for lo < hi {
		if mid := (lo + hi) / 2; fits(mid) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo, runN(lo - 1).stderr
}

// S2: each program is charged the same steps on both engines, to the
// node; one step fewer stops both at the same place with the same
// message. In "tasks" the tasks share the budget, so which task runs out
// is a race and only the total is compared.
func TestP84StepsExactOnBothEngines(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, src string
		sameCut   bool
	}{
		{"calls", `let fib = fn(n) { if (n < 2) { n } else { fib(n - 1) + fib(n - 2) } }; puts(fib(9))`, true},
		{"loops and hashes", `let h = {"n": 2, "xs": [1, 2, 3]};
let rs = for x in h.xs { for y in h.xs { let k = x * y; {"k": k, "big": k >= h.n && k <= 6}["big"] } };
puts(rs)`, true},
		{"ergonomics and match", `let r = {"name": "bob", "n": 4};
let f = fn(i) { match (if (i == 2) { error("two") } else { i }) { ok(v) => "v=${v} ${r.name} ${v <= r.n || false}", err(e) => e.message } };
puts(for i in [1, 2, 3] { f(i) })`, true},
		{"many names", `let f = fn(a, b, c, d, e) { let g = a + b; let h = c + d; let k = fn(z) { z + g + h + e }; k(1) + k(2) };
puts(f(1, 2, 3, 4, 5) + f(5, 4, 3, 2, 1))`, true},
		{"tasks", `let f = fn(x) { for i in [1, 2, 3, 4, 5] { x * i } };
puts(scope { let ts = for i in [1, 2, 3] { spawn f(i) }; for t in ts { await t } })`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			path := writeProg(t, c.src)
			nw, ew := stepsFit(t, walkEngine, path)
			nc, ec := stepsFit(t, compiledEngine, path)
			if nw != nc {
				t.Fatalf("steps: tree-walker %d, compiled %d", nw, nc)
			}
			if c.sameCut && ew != ec {
				t.Fatalf("with %d steps:\ntree-walker %q\ncompiled    %q", nw-1, ew, ec)
			}
		})
	}
}

// S2: --alloc-mb stops a growing program at the same value on both
// engines: the same lines printed, the same error at the same place.
func TestP84AllocCutOnBothEngines(t *testing.T) {
	t.Parallel()
	path := writeProg(t, `let grow = fn(i, acc) { puts(i); grow(i + 1, push(acc, "abcdefghij")) };
grow(0, [])`)
	for _, mb := range []string{"1", "2"} {
		res := bothEngines(t, spec{args: []string{"run", "--alloc-mb", mb, path}})
		want := "error: budget exceeded: alloc (limit " + strconv.Itoa(mustAtoi(t, mb)<<20) + " bytes) (at " + path + ":1:46)\n"
		if res.code != 1 || res.stderr != want || !strings.HasPrefix(res.stdout, "0\n1\n2\n") {
			t.Fatalf("--alloc-mb %s: want stderr %q\n%s", mb, want, res)
		}
	}
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// The position a timeout is reported at is wherever the code was; only
// the message must match.
var p84At = regexp.MustCompile(` \(at [^)]*\)\n$`)

// S3: --timeout stops endless compiled code, in the root and in four
// tasks, as it stops the tree-walker: the same message, well within the
// (generous, the machine may be loaded) bound.
func TestP84TimeoutStopsCompiledCode(t *testing.T) {
	t.Parallel()
	path := writeProg(t, p84Spin+"let ts = for i in [1, 2, 3, 4] { spawn spin() };\nspin()")
	for _, e := range engines {
		res := onEngine(t, e, spec{args: []string{"run", "--timeout", "300ms", path}, timeout: 2 * time.Minute})
		msg := p84At.ReplaceAllString(res.stderr, "")
		if res.code != 1 || msg != "error: budget exceeded: timeout (limit 300 ms)" || res.stdout != "" {
			t.Fatalf("%s: want the timeout\n%s", e, res)
		}
		if res.duration > 30*time.Second {
			t.Fatalf("%s: a 300 ms timeout took %v", e, res.duration)
		}
	}
}

// S3 and I6: a scope's timeout_ms ends compiled code inside it, and the
// program goes on after the scope, on both engines alike.
func TestP84ScopeTimeoutStopsCompiledCode(t *testing.T) {
	t.Parallel()
	path := writeProg(t, p84Spin+`puts(match scope (timeout_ms: 200) { spin() } { ok(v) => v, err(e) => [e.kind, e.message] });
puts(match scope (steps: 5000) { spin() } { ok(v) => v, err(e) => [e.kind, e.message] });
puts("after")`)
	res := bothEngines(t, spec{args: []string{"run", path}, timeout: 2 * time.Minute})
	want := "[budget, budget exceeded: timeout (limit 200 ms)]\n[budget, budget exceeded: steps (limit 5000)]\nafter\n"
	expect(t, spec{stdout: want}, res)
}

// S3 and I5: a sibling's failure cancels tasks spinning in compiled
// code. Each spinner arrives at a gate before it spins; the failing task
// waits for all three, so it fails while they spin.
func TestP84FailFastStopsCompiledCode(t *testing.T) {
	t.Parallel()
	src := p84Spin + `let caps = [net("127.0.0.1")];
let go = fn() { http_get("{{SERVER}}/arrive?k=spin&id={{ID}}"); spin() };
let boom = fn() { http_get("{{SERVER}}/wait?k=spin&n=3&id={{ID}}");
  error("boom") };
let ts = [spawn go() with caps, spawn go() with caps, spawn go() with caps, spawn boom() with caps];
for t in ts { await t }`
	for _, e := range engines {
		t.Run(e, func(t *testing.T) { // its own test ID, so its own gate
			path := writeProg(t, src)
			res := onEngine(t, e, spec{args: []string{"run", "--allow", "net=127.0.0.1", path}, timeout: 2 * time.Minute})
			want := "error: boom (at " + path + ":6:3)\n"
			if res.code != 1 || res.stderr != want || hits(t, "/arrive") != 3 {
				t.Fatalf("want %q after 3 arrivals (got %d)\n%s", want, hits(t, "/arrive"), res)
			}
			if res.duration > 60*time.Second {
				t.Fatalf("the failure took %v to stop the spinners", res.duration)
			}
		})
	}
}
