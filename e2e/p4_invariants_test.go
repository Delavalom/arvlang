package e2e

import (
	"testing"
	"time"
)

// The invariants Phase 4 touches, through the binary: I5 (fail_fast
// cancels siblings and passes on the first error with its kind), errors
// keeping kind, task and position across task boundaries, I6 (a restart
// never resets or escapes a budget) and I8 (a spawned task never sees a
// later rebinding, transitively, on every restart attempt).

const showErr = `{ ok(v) => v, err(e) => [e["kind"], e["message"], e["task"], e["pos"]] }`

// burnRestart restarts f, which arrives at /arrive and burns steps, under
// the scope options opts; f fails with a user error unless a budget stops
// it first.
func burnRestart(opts, work string) string { return burnRestartIn("", opts, work) }

// burnRestartIn is burnRestart with the restart scope inside a scope with
// the options outer, unless outer is "".
func burnRestartIn(outer, opts, work string) string {
	body := "scope (" + opts + ") { await spawn f() " + grant + " }"
	if outer != "" {
		body = "scope (" + outer + ") { " + body + " }"
	}
	return "let burn = fn(n) { if (n == 0) { 0 } else { burn(n - 1) } };\n" +
		"let grow = fn(n, s) { if (n == 0) { len(s) } else { grow(n - 1, s + s) } };\n" +
		"let f = fn() {\n  http_get(\"{{SERVER}}/arrive?id={{ID}}&k=try\");\n  " + work + ";\n  error(\"u\")\n};\n" +
		"puts(match " + body + " " +
		`{ ok(v) => v, err(e) => [e["kind"], e["message"], e["task"]] })`
}

// arrivals checks /arrive saw between lo and hi requests from this test.
func arrivals(lo, hi int) func(*testing.T, result) {
	return func(t *testing.T, res result) {
		t.Helper()
		if n := hits(t, "/arrive"); n < lo || n > hi {
			t.Fatalf("/arrive hits = %d, want %d to %d\n%s", n, lo, hi, res)
		}
	}
}

func TestPhase4Invariants(t *testing.T) {
	t.Parallel()
	const restart = `supervise: "restart", retries: 100, backoff_ms: 1, retry_on: ["user"]`
	runP4(t, []p4case{
		{name: "I5: fail_fast cancels a sibling and passes on the first error",
			stdout: "[user, first, root/1, 4:3]\n",
			src: "let slow = fn() { http_get(\"{{SERVER}}/slow?id={{ID}}&ms=30000\") };\n" +
				"let bad = fn() {\n  http_get(\"{{SERVER}}/wait?id={{ID}}&k=slow&n=1\");\n  error(\"first\")\n};\n" +
				"puts(match scope {\n  spawn slow() " + grant + ";\n  spawn bad() " + grant + ";\n  0\n} " + showErr + ")",
			verify: func(t *testing.T, res result) {
				end, ok := waitSlowEnd(t, 30*time.Second)
				if !ok || !end.cancelled {
					t.Fatalf("the sibling's /slow request was not cancelled (seen %v, %+v)\n%s", ok, end, res)
				}
			}},
		{name: "fail_fast passes on an effect error from a grandchild with its kind, task and position",
			stdout: "[effect, root/0/0, 1:16]\n",
			src: "let g = fn() { http_get(\"http://127.0.0.1:1/\") };\n" +
				"let c = fn() { await spawn g() " + grant + " };\n" +
				`puts(match scope { await spawn c() ` + grant + ` } { ok(v) => v, err(e) => [e["kind"], e["task"], e["pos"]] })`},
		{name: "an uncaught grandchild error keeps its position",
			src: "let g = fn() { getenv(\"ARV_P4_NOPE\") };\nlet c = fn() { await spawn g() };\nawait spawn c()",
			err: `capability denied: env("ARV_P4_NOPE") in task root/0/0 (holds: [])`, at: "1:16"},
		{name: "a collected error keeps kind, task and position across two task boundaries",
			stdout: "[user, deep, root/0/0, 1:31]\n",
			src: "let g = fn() { let h = fn() { error(\"deep\") }; await spawn h() };\n" +
				"puts(match scope (supervise: \"collect\") { await spawn g() } " + showErr + ")"},
		{name: "I6: restart does not reset the steps budget",
			stdout: "[budget, budget exceeded: steps (limit 3000), root/0#2]\n",
			src:    burnRestart("steps: 3000, "+restart, "burn(150)"), verify: arrivals(2, 2)},
		{name: "I6: an inner restart scope cannot escape its parent's steps",
			stdout: "[budget, budget exceeded: steps (limit 3000), root/0#2]\n",
			src:    burnRestartIn("steps: 3000", "steps: 1000000, "+restart, "burn(150)"), verify: arrivals(2, 2)},
		{name: "I6: restart does not reset the alloc budget",
			stdout: "[budget, budget exceeded: alloc (limit 1048576 bytes), root/0#2]\n",
			src:    burnRestart("alloc_mb: 1, "+restart, "grow(17, \"ab\")"), verify: arrivals(2, 2)},
		{name: "I6: a timeout cuts a restart backoff short",
			stdout: "[budget, budget exceeded: timeout (limit 300 ms), root]\n",
			src:    burnRestart(`timeout_ms: 300, supervise: "restart", retries: 100, backoff_ms: 30000, retry_on: ["user"]`, "1"),
			verify: func(t *testing.T, res result) {
				arrivals(1, 1)(t, res)
				if res.duration > 20*time.Second {
					t.Fatalf("the run waited out the backoff: %s\n%s", res.duration, res)
				}
			}},
		{name: "I6: restart attempts reuse the task's slot",
			stdout: "[user, u, root/0#3]\n",
			src:    burnRestart(`tasks: 1, supervise: "restart", retries: 2, backoff_ms: 1, retry_on: ["user"]`, "1"),
			verify: arrivals(3, 3)},
		{name: "I8: every restart attempt sees the bindings as of the spawn",
			stdout: "1\n1\n1\n", err: "again", at: "4:3",
			src: "scope (supervise: \"restart\", retries: 2, backoff_ms: 1, retry_on: [\"user\"]) {\n" +
				"  let x = 1;\n  let f = fn() { http_get(\"{{SERVER}}/wait?id={{ID}}&k=rebound&n=1\"); puts(x);\n" +
				"  error(\"again\") };\n  let t = spawn f() " + grant + ";\n  let x = 2;\n" +
				"  http_get(\"{{SERVER}}/arrive?id={{ID}}&k=rebound\");\n  await t\n}"},
		{name: "I8: a grandchild does not see its grandparent rebind a name", stdout: "[1, 2]\n",
			src: "let x = 1;\nlet f = fn() {\n  let g = fn() { http_get(\"{{SERVER}}/wait?id={{ID}}&k=rebound&n=1\"); x };\n" +
				"  await spawn g() " + grant + "\n};\nlet t = spawn f() " + grant + ";\nlet x = 2;\n" +
				"http_get(\"{{SERVER}}/arrive?id={{ID}}&k=rebound\");\nputs([await t, x])"},
		{name: "I8: a grandchild does not see its parent rebind a name", stdout: "[1, 2]\n",
			src: "let f = fn() {\n  let y = 1;\n  let g = fn() { http_get(\"{{SERVER}}/wait?id={{ID}}&k=rebound&n=1\"); y };\n" +
				"  let t = spawn g() " + grant + ";\n  let y = 2;\n  http_get(\"{{SERVER}}/arrive?id={{ID}}&k=rebound\");\n" +
				"  [await t, y]\n};\nputs(await spawn f() " + grant + ")"},
		{name: "I8: a task spawned after the rebinding sees it", stdout: "[1, 2]\n",
			src: "let x = 1;\nlet f = fn() { x };\nlet a = spawn f();\nlet x = 2;\nlet b = spawn f();\nputs([await a, await b])"},
	})
}
