package e2e

import "testing"

// Phase 4 through the binary: error(msg), match on an error's kind,
// supervise: fail_fast, collect and restart, and I8 (a spawned task reads
// its parent's bindings as of the spawn). Each is a small inline program.
func TestSupervision(t *testing.T) {
	t.Parallel()
	runInline(t, []inline{
		{name: "an uncaught user error", src: "let x = 1;\nerror(\"boom\")", err: "boom", at: "2:1"},
		{name: "match reads a user error", stdout: "[user, boom, root, 1:12]\n",
			src: `puts(match error("boom") { ok(v) => v, err(e) => [e["kind"], e["message"], e["task"], e["pos"]] })`},
		{name: "match dispatches on the kind", stdout: "denied\n",
			src: `puts(match getenv("ARV_P4_NOPE") { ok(v) => v, err(e) => if (e["kind"] == "capability") { "denied" } else { "other" } })`},
		{name: "fail_fast: the first error leaves the scope with its kind, task and position",
			stdout: "[user, x, root/0, 2:18]\n",
			src:    "puts(match scope {\n  let f = fn() { error(\"x\") };\n  spawn f();\n  sleep(60000)\n} { ok(v) => 0, err(e) => [e[\"kind\"], e[\"message\"], e[\"task\"], e[\"pos\"]] })"},
		{name: "collect: every outcome", stdout: "[1, user, 3]\n",
			src: "let f = fn(n) { if (n == 2) { error(\"two\") } else { n } };\n" +
				"puts(scope (supervise: \"collect\") {\n  let ts = [spawn f(1), spawn f(2), spawn f(3)];\n" +
				"  for t in ts { match await t { ok(v) => v, err(e) => e[\"kind\"] } }\n})"},
		{name: "collect: an unawaited failure is reported",
			src: "scope (supervise: \"collect\") {\n  let bad = fn() { error(\"x\") };\n  spawn bad();\n  1\n}",
			err: "1 unawaited task failures: x", at: "2:20"},
		{name: "restart: retries, then the last error", stdout: "try\ntry\ntry\n",
			src: "scope (supervise: \"restart\", retries: 2, backoff_ms: 1, retry_on: [\"user\"]) {\n" +
				"  let f = fn() { puts(\"try\"); error(\"no\") };\n  await spawn f()\n}",
			err: "no", at: "2:31"},
		{name: "restart: a capability error runs once", stdout: "try\n",
			src: "let f = fn() { puts(\"try\"); getenv(\"ARV_P4_NOPE\") };\n" +
				"scope (supervise: \"restart\", retries: 3, backoff_ms: 1) { await spawn f() }",
			err: `capability denied: env("ARV_P4_NOPE") in task root/0 (holds: [])`, at: "1:29"},
		{name: "I8: a child does not see a later rebinding", stdout: "[1, 2]\n",
			src: "let x = 1;\nlet f = fn() { x };\nlet t = spawn f();\nlet x = 2;\nputs([await t, x])"},
	})
}
