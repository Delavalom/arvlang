package e2e

import (
	"strings"
	"testing"
)

// I2 for host kinds, through the binary: a child holds only what its
// spawn names, never more than its parent, and no Fn runs for a call its
// task may not make.
func TestP82Attenuation(t *testing.T) {
	t.Parallel()
	runHosts(t, []hostSpec{
		{name: "a child handed the grant may call", caps: []string{"k8s=staging"},
			src:    `let f = fn() { k8s_apply("staging", "m")["applied"] }; puts(await spawn f() with [k8s("staging")])`,
			stdout: "m\n", calls: []string{"k8s_apply(staging) task=root/0"}},
		{name: "a child spawned without with holds nothing", caps: []string{"k8s=staging"},
			src: `let f = fn() { k8s_apply("staging", "m") }; await spawn f()`, code: 1, kind: "capability",
			errHas: []string{`capability denied: k8s("staging") in task root/0 (holds: [])`}},
		{name: "a child cannot reach past its grant", caps: []string{"k8s=staging", "k8s=prod"},
			src: `let f = fn() { k8s_apply("prod", "m") }; await spawn f() with [k8s("staging")]`, code: 1, kind: "capability",
			errHas: []string{`capability denied: k8s("prod") in task root/0 (holds: [k8s("staging")])`}},
		{name: "a parent cannot hand on what it lacks", caps: []string{"k8s=staging"},
			src: `let f = fn() { k8s_apply("prod", "m") }; await spawn f() with [k8s("prod")]`, code: 1, kind: "capability",
			errHas: []string{`spawn in task root: capability denied: k8s("prod") (holds: [k8s("staging")])`}},
		{name: "a subtree grant narrows", caps: []string{"team=infra"},
			src: `let g = fn(s) { team_read(s) };
let f = fn() { [g("infra/db/eu"), match g("infra/web") { ok(v) => v, err(e) => e["kind"] }] };
puts(await spawn f() with [team("infra/db")])`,
			stdout: "[read infra/db/eu, capability]\n", calls: []string{"team_read(infra/db/eu) task=root/0"}},
		{name: "a grandchild cannot widen back", caps: []string{"team=infra"},
			src: `let h = fn() { team_read("infra/web") };
let f = fn() { await spawn h() with [team("infra")] };
await spawn f() with [team("infra/db")]`, code: 1, kind: "capability",
			errHas: []string{`spawn in task root/0: capability denied: team("infra") (holds: [team("infra/db")])`}},
		{name: "three levels narrow step by step", caps: []string{"team=a"},
			src: `let leaf = fn() { team_read("a/b/c/d") };
let mid = fn() { await spawn leaf() with [team("a/b/c")] };
let top = fn() { await spawn mid() with [team("a/b")] };
puts(await spawn top() with [team("a")])`,
			stdout: "read a/b/c/d\n", calls: []string{"team_read(a/b/c/d) task=root/0/0/0"}},
		{name: "a sibling's grant is not shared", caps: []string{"k8s=staging", "k8s=prod"},
			src: `let a = fn() { k8s_apply("staging", "m")["namespace"] };
let b = fn() { match k8s_apply("staging", "m") { ok(v) => "applied", err(e) => e["kind"] } };
puts([await spawn a() with [k8s("staging")], await spawn b() with [k8s("prod")]])`,
			stdout: "[staging, capability]\n", calls: []string{"k8s_apply(staging) task=root/0"}},
	})
}

// Errors from Fn and Scope become Arv errors with a kind, a task and a
// position; a panic in Fn is an error, never a crash of the host.
func TestP82Errors(t *testing.T) {
	t.Parallel()
	ok := []string{"k8s=staging"}
	runHosts(t, []hostSpec{
		{name: "an error from Fn is an effect error", caps: ok,
			src: `k8s_fail("staging", "the cluster said no")`, code: 1, kind: "effect",
			errHas: []string{"k8s_fail: the cluster said no"}, calls: []string{"k8s_fail(staging) task=root"}},
		{name: "a program matches it", caps: ok,
			src:    `puts(match k8s_fail("staging", "no") { ok(v) => v, err(e) => [e["kind"], e["message"], e["task"], e["pos"]] })`,
			stdout: "[effect, k8s_fail: no, root, 1:12]\n", calls: []string{"k8s_fail(staging) task=root"}},
		{name: "a DeniedError from Fn is a capability error", caps: ok,
			src: `k8s_deny("staging")`, code: 1, kind: "capability",
			errHas: []string{`k8s("upstream")`}, calls: []string{"k8s_deny(staging) task=root"}},
		{name: "a panic in Fn is an error", caps: ok,
			src: `k8s_panic("staging")`, code: 1, errHas: []string{"host bug in k8s_panic"},
			calls: []string{"k8s_panic(staging) task=root"}},
		{name: "a panic in a child's Fn fails the scope", caps: ok,
			src:    `let f = fn() { k8s_panic("staging") }; puts(match scope { await spawn f() with [k8s("staging")] } { ok(v) => v, err(e) => e["task"] })`,
			stdout: "root/0\n", calls: []string{"k8s_panic(staging) task=root/0"}},
		{name: "a long error is cut", caps: ok,
			src: `let grow = fn(s, n) { if (n == 0) { s } else { grow(s + s, n - 1) } };
let m = match k8s_fail("staging", grow("é", 12)) { ok(v) => v, err(e) => e["message"] };
puts([len(m) < 1100, len(m) > 500])`,
			stdout: "[true, true]\n", calls: []string{"k8s_fail(staging) task=root"}},
		{name: "the wrong arguments for Fn are its own error", caps: ok,
			src: `k8s_apply("staging")`, code: 1, kind: "effect",
			errHas: []string{"k8s_apply: want (namespace, manifest), got 1 arguments"},
			calls:  []string{"k8s_apply(staging) task=root"}},
		{name: "a restart retries an effect error", caps: ok,
			src: `let f = fn() { k8s_fail("staging", "flaky") };
scope (supervise: "restart", retries: 2, backoff_ms: 1, retry_on: ["effect"]) { await spawn f() with [k8s("staging")] }`,
			code: 1, kind: "effect", errHas: []string{"k8s_fail: flaky"},
			calls: []string{"k8s_fail(staging) task=root/0", "k8s_fail(staging) task=root/0#2", "k8s_fail(staging) task=root/0#3"}},
		{name: "a restart never retries a denial, and Fn never runs", caps: ok,
			src: `let f = fn() { k8s_apply("prod", "m") };
scope (supervise: "restart", retries: 3, backoff_ms: 1, retry_on: ["effect", "user"]) { await spawn f() with [k8s("staging")] }`,
			code: 1, kind: "capability", errHas: []string{`k8s("prod") in task root/0`}},
		{name: "collect gathers host outcomes", caps: ok,
			src: `let f = fn(m) { if (m == "bad") { k8s_fail("staging", m) } else { k8s_apply("staging", m)["applied"] } };
puts(scope (supervise: "collect") {
  let ts = for m in ["a", "bad", "c"] { spawn f(m) with [k8s("staging")] };
  for t in ts { match await t { ok(v) => v, err(e) => e["kind"] } }
})`,
			stdout: "[a, effect, c]\n", anyCalls: true,
			verify: func(t *testing.T, r hostResult) {
				if len(r.calls) != 3 {
					t.Fatalf("calls = %q, want 3", r.calls)
				}
			}},
	})
}

// Budgets (I6): every call settles the steps before it; a result is
// charged to alloc_mb, in a scope and in the run; a child's budget bounds
// its calls.
func TestP82Budgets(t *testing.T) {
	t.Parallel()
	ok := []string{"k8s=staging"}
	const loop = `let loop = fn(i) { k8s_apply("staging", "m"); loop(i + 1) };` + "\n"
	countCalls := func(max int) func(t *testing.T, r hostResult) {
		return func(t *testing.T, r hostResult) {
			if len(r.calls) == 0 || len(r.calls) > max {
				t.Fatalf("%d calls reached Fn, want 1 to %d", len(r.calls), max)
			}
		}
	}
	runHosts(t, []hostSpec{
		{name: "a run's steps stop the calls", caps: ok, flags: []string{"-steps", "2000"},
			src: loop + "loop(0)", code: 1, kind: "budget", errHas: []string{"budget exceeded: steps (limit 2000)"},
			anyCalls: true, verify: countCalls(2000)},
		{name: "a scope's steps stop the calls", caps: ok,
			src: loop + "scope (steps: 1000) { loop(0) }", code: 1, kind: "budget",
			errHas: []string{"budget exceeded: steps (limit 1000)"}, anyCalls: true, verify: countCalls(1000)},
		{name: "a child's steps stop its calls", caps: ok,
			src:  loop + `let f = fn() { loop(0) }; scope (steps: 1500) { await spawn f() with [k8s("staging")] }`,
			code: 1, kind: "budget", errHas: []string{"budget exceeded: steps (limit 1500)"},
			anyCalls: true, verify: countCalls(1500)},
		{name: "a result is charged in a scope", caps: ok,
			src: `scope (alloc_mb: 1) { k8s_big("staging", 2097152) }`, code: 1, kind: "budget",
			errHas: []string{"budget exceeded: alloc"}, calls: []string{"k8s_big(staging) task=root"}},
		{name: "a result is charged in the run", caps: ok, flags: []string{"-alloc-mb", "1"},
			src: `k8s_big("staging", 2097152)`, code: 1, kind: "budget",
			errHas: []string{"budget exceeded: alloc"}, calls: []string{"k8s_big(staging) task=root"}},
		{name: "results add up", caps: ok, flags: []string{"-alloc-mb", "1"},
			src: `let a = for i in [1, 2, 3, 4] { k8s_big("staging", 300000) }; puts(len(a))`, code: 1, kind: "budget",
			errHas: []string{"budget exceeded: alloc"}, anyCalls: true, verify: countCalls(4)},
		{name: "a result under the budget is fine", caps: ok, flags: []string{"-alloc-mb", "1"},
			src: `puts(len(k8s_big("staging", 100000)))`, stdout: "100000\n", calls: []string{"k8s_big(staging) task=root"}},
		{name: "an echoed DAG is charged per node, not per path", caps: ok, flags: []string{"-alloc-mb", "1"},
			src: `let dag = fn(x, n) { if (n == 0) { x } else { dag([x, x], n - 1) } };
let d = dag(["leaf"], 40);
puts(len(k8s_echo("staging", d)))`,
			stdout: "2\n", calls: []string{"k8s_echo(staging) task=root"}, maxRunFor: 60e9},
	})
}

// Cancellation (I4, I5): a failing sibling or a timeout cancels a task
// waiting in Fn, the scope waits for Fn to return, and no goroutine
// outlives arv.Run (p82host exits 3 if one does). Events are ordered by
// the server's gates, not by sleeps.
func TestP82Cancellation(t *testing.T) {
	t.Parallel()
	caps := []string{"k8s=staging", "net=127.0.0.1"}
	const wait = `let w = fn() { k8s_wait("staging") };` + "\n"
	runHosts(t, []hostSpec{
		{name: "a failing sibling cancels Fn", caps: caps, gates: true,
			src: wait + `scope {
  spawn w() with [k8s("staging")];
  http_get("{{SERVER}}/wait?k=fn&n=1&id={{ID}}");
  error("stop")
}`, code: 1, kind: "user", errHas: []string{"stop"},
			calls: []string{"k8s_wait cancelled task=root/0", "k8s_wait(staging) task=root/0"}},
		{name: "many waiting calls are all cancelled", caps: caps, gates: true,
			src: wait + `scope {
  for i in [1, 2, 3, 4, 5, 6, 7, 8] { spawn w() with [k8s("staging")] };
  http_get("{{SERVER}}/wait?k=fn&n=8&id={{ID}}");
  1 / 0
}`, code: 1, errHas: []string{"division by zero"}, anyCalls: true,
			verify: func(t *testing.T, r hostResult) {
				started, cancelled := 0, 0
				for _, c := range r.calls {
					if strings.Contains(c, "cancelled") {
						cancelled++
					} else {
						started++
					}
				}
				if started != 8 || cancelled != 8 {
					t.Fatalf("%d calls started, %d saw the cancellation; want 8 and 8", started, cancelled)
				}
			}},
		{name: "a scope timeout cancels Fn", caps: caps, gates: true,
			src:    wait + `puts(match scope (timeout_ms: 300) { w() } { ok(v) => v, err(e) => e["kind"] })`,
			stdout: "budget\n", anyCalls: true, verify: pairedWaits},
		{name: "the run's timeout cancels Fn", caps: caps, gates: true, flags: []string{"-timeout-ms", "300"},
			src: wait + `w()`, code: 1, kind: "budget", errHas: []string{"timeout"}, anyCalls: true,
			verify: pairedWaits, maxRunFor: 60e9},
		{name: "a panic in one child's Fn cancels the others' Fn", caps: caps, gates: true,
			src: wait + `let p = fn() { k8s_panic("staging") };
scope {
  spawn w() with [k8s("staging")];
  spawn w() with [k8s("staging")];
  http_get("{{SERVER}}/wait?k=fn&n=2&id={{ID}}");
  await spawn p() with [k8s("staging")]
}`, code: 1, errHas: []string{"host bug in k8s_panic"},
			calls: []string{"k8s_panic(staging) task=root/2", "k8s_wait cancelled task=root/0", "k8s_wait cancelled task=root/1",
				"k8s_wait(staging) task=root/0", "k8s_wait(staging) task=root/1"}},
	})
}

// pairedWaits: under load a timeout may fire before the call reaches Fn;
// either way every call that reached Fn saw its cancellation.
func pairedWaits(t *testing.T, r hostResult) {
	t.Helper()
	switch len(r.calls) {
	case 0:
	case 2:
		if !strings.Contains(r.calls[0], "cancelled") || strings.Contains(r.calls[1], "cancelled") {
			t.Fatalf("calls = %q, want a wait and its cancellation", r.calls)
		}
	default:
		t.Fatalf("calls = %q, want none, or a wait and its cancellation", r.calls)
	}
}

// Many tasks call one effect at once (the host is built with -race under
// ARV_E2E_RACE), and the results are each task's own.
func TestP82Concurrent(t *testing.T) {
	t.Parallel()
	runHosts(t, []hostSpec{
		{name: "fifty tasks", caps: []string{"k8s=staging"},
			src: `let d = fn(i) { k8s_apply("staging", json(i))["applied"] };
let upto = fn(acc, n) { if (len(acc) == n) { acc } else { upto(push(acc, len(acc)), n) } };
let ts = for i in upto([], 50) { spawn d(i) with [k8s("staging")] };
let rs = await ts;
puts([len(rs), rs[0], rs[49]])`,
			stdout: "[50, 0, 49]\n", anyCalls: true,
			verify: func(t *testing.T, r hostResult) {
				if len(r.calls) != 50 {
					t.Fatalf("%d calls reached Fn, want 50", len(r.calls))
				}
			}},
		{name: "an echoed value is the program's, unchanged", caps: []string{"k8s=staging"},
			src: `let v = {"a": [1, 2], "b": "s"};
let e = k8s_echo("staging", v);
let f = fn() { k8s_echo("staging", v)["a"] };
puts([e["a"], e["b"], await spawn f() with [k8s("staging")], v["a"]])`,
			stdout: "[[1, 2], s, [1, 2], [1, 2]]\n",
			calls:  []string{"k8s_echo(staging) task=root", "k8s_echo(staging) task=root/0"}},
	})
}
