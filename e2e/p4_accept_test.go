package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// The acceptance tests of 11-phase4-supervision.md, through the binary.
// Attempts are counted, and backoffs timed, by the server.

// check500 turns a 500 from /flaky into error(...), as the design says.
const check500 = "let check = fn() {\n" +
	"  let r = http_get(\"{{SERVER}}/flaky?id={{ID}}&k=a&fail={{FAIL}}\");\n" +
	"  if (r[\"status\"] != 200) { error(\"status not ok\") } else { r[\"body\"] }\n" +
	"};\n"

func flaky(fail, opts string) string {
	return strings.ReplaceAll(check500, "{{FAIL}}", fail) +
		"puts(scope (supervise: \"restart\", " + opts + ") {\n  await spawn check() " + grant + "\n})"
}

func flakyCount(want int) func(*testing.T, result) {
	return func(t *testing.T, res result) {
		t.Helper()
		if got := len(flakyTimes(t, "a")); got != want {
			t.Fatalf("/flaky attempts = %d, want %d\n%s", got, want, res)
		}
	}
}

func TestPhase4Acceptance(t *testing.T) {
	t.Parallel()
	errAt := "3:" + strconv.Itoa(col(`  if (r["status"] != 200) { error("status not ok") } else { r["body"] }`, "error("))
	runP4(t, []p4case{
		{name: "restart recovers", stdout: "up\n",
			src:    flaky("2", `retry_on: ["effect", "user"], retries: 3, backoff_ms: 1`),
			verify: flakyCount(3)},
		{name: "restart runs at most retries+1 times",
			src: flaky("10", `retry_on: ["effect", "user"], retries: 3, backoff_ms: 1`),
			err: "status not ok", at: errAt, verify: flakyCount(4)},
		{name: "restart does not retry user errors by default",
			src: flaky("10", `retries: 3, backoff_ms: 1`),
			err: "status not ok", at: errAt, verify: flakyCount(1)},
		{name: "no retry on capability",
			src: "let f = fn() {\n  http_get(\"{{SERVER}}/arrive?id={{ID}}&k=try\");\n  getenv(\"ARV_P4_NOPE\")\n};\n" +
				"scope (supervise: \"restart\", retry_on: [\"effect\", \"user\"], retries: 3, backoff_ms: 1) {\n" +
				"  await spawn f() " + grant + "\n}",
			err: `capability denied: env("ARV_P4_NOPE") in task root/0 (holds: [net("127.0.0.1")])`, at: "3:3",
			verify: wantHits("/arrive", 1)},
		{name: "backoff", stdout: "up\n",
			src: flaky("2", `retry_on: ["effect", "user"], retries: 3, backoff_ms: 100`),
			verify: func(t *testing.T, res result) {
				at := flakyTimes(t, "a")
				if len(at) != 3 {
					t.Fatalf("/flaky attempts = %d, want 3\n%s", len(at), res)
				}
				// Waits of 100 and 200 ms (plus up to 10% jitter) between attempts.
				if d1, d2 := at[1].Sub(at[0]), at[2].Sub(at[1]); d1 < 100*time.Millisecond ||
					d2 < 200*time.Millisecond || at[2].Sub(at[0]) < 300*time.Millisecond {
					t.Fatalf("waits %s and %s, want at least 100 ms and 200 ms\n%s", d1, d2, res)
				}
			}},
		{name: "restart retries an effect error by default, with attempt IDs",
			stdout: "[effect, root/0#4]\n",
			src: "let f = fn() {\n  http_get(\"{{SERVER}}/arrive?id={{ID}}&k=try\");\n  http_get(\"http://127.0.0.1:1/\")\n};\n" +
				"puts(match scope (supervise: \"restart\", backoff_ms: 1) { await spawn f() " + grant + " } " +
				"{ ok(v) => v, err(e) => [e[\"kind\"], e[\"task\"]] })",
			verify: wantHits("/arrive", 4)},
		{name: "collect", stdout: "[1, capability, 3]\n",
			src: "let mid = fn() { http_get(\"{{SERVER}}/arrive?id={{ID}}&k=mid\"); getenv(\"ARV_P4_NOPE\") };\n" +
				"let side = fn(n) {\n  http_get(\"{{SERVER}}/wait?id={{ID}}&k=mid&n=1\");\n  sleep(100);\n" +
				"  http_get(\"{{SERVER}}/arrive?id={{ID}}&k=after\");\n  n\n};\n" +
				"puts(scope (supervise: \"collect\") {\n" +
				"  let ts = [spawn side(1) " + grant + ", spawn mid() " + grant + ", spawn side(3) " + grant + "];\n" +
				"  for t in ts { match await t { ok(v) => v, err(e) => e[\"kind\"] } }\n})",
			// mid's arrival and both siblings' arrivals after the failure
			verify: wantHits("/arrive", 3)},
		{name: "collect never drops",
			src: "scope (supervise: \"collect\") {\n  let bad = fn() { error(\"x\") };\n  spawn bad();\n  1\n}",
			err: "1 unawaited task failures: x", at: "2:20"},
		{name: "collect reports the first unawaited failure in spawn order",
			stdout: "[capability, 2 unawaited task failures: capability denied: env(\"ARV_P4_NOPE\") in task root/0 (holds: [net(\"127.0.0.1\")]), root/0, 3:3]\n",
			src: "let first = fn() {\n  http_get(\"{{SERVER}}/wait?id={{ID}}&k=second&n=1\");\n  getenv(\"ARV_P4_NOPE\")\n};\n" +
				"let second = fn() { http_get(\"{{SERVER}}/arrive?id={{ID}}&k=second\"); error(\"u\") };\n" +
				"let ok = fn() { 1 };\n" +
				"puts(match scope (supervise: \"collect\") {\n  spawn first() " + grant + ";\n  spawn second() " + grant + ";\n" +
				"  let t = spawn ok();\n  await t\n} { ok(v) => v, err(e) => [e[\"kind\"], e[\"message\"], e[\"task\"], e[\"pos\"]] })"},
		{name: "escalation", stdout: "[1, [user, in, root/1/0#3], 3]\n",
			src: "let g = fn() { http_get(\"{{SERVER}}/arrive?id={{ID}}&k=attempt\"); error(\"in\") };\n" +
				"let inner = fn() {\n  scope (supervise: \"restart\", retries: 2, backoff_ms: 1, retry_on: [\"user\"]) { await spawn g() " + grant + " }\n};\n" +
				"let side = fn(n) { http_get(\"{{SERVER}}/wait?id={{ID}}&k=attempt&n=3\"); n };\n" +
				"puts(scope (supervise: \"collect\") {\n" +
				"  let ts = [spawn side(1) " + grant + ", spawn inner() " + grant + ", spawn side(3) " + grant + "];\n" +
				"  for t in ts { match await t { ok(v) => v, err(e) => [e[\"kind\"], e[\"message\"], e[\"task\"]] } }\n})",
			verify: wantHits("/arrive", 3)},
		{name: "match passthrough", stdout: "6\n", src: "puts(match 5 { ok(v) => v + 1, err(e) => 0 })"},
	})
}
