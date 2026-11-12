# Phase 6x7, task p67.7: e2e: offline replay of an agent run; no log holds the key

> **For agentic workers:** this file is one task, p67.7. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p67.1 to p67.6 are committed on `phase/p67`. Work in `/Users/delavalom/delavalom-labs/arvlang-wt/p67` on branch `phase/p67`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 &&`: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push, never touch remotes; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `min`, `max` and `t.Chdir` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report status "blocked", reason "disk" (only `go clean -cache` may be deleted). The machine is shared and heavily loaded: keep wall-clock bounds generous and order events with server gates, not sleeps. No test may reach the network or spend money: every test uses the fake provider or a local `httptest` server; never set `ARV_LIVE_ANTHROPIC`, never put a real API key anywhere. If a test exposes a real bug outside this task, mark it `t.Skip("BUG: ...")` with a reproduction and report it; never weaken a test to make it pass.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` at `b516ca2` (Phases 3 to 8.3 and 5E merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`); each "expected failure" was seen. Designs: `13-phase6-durable-replay.md`, `14-phase7-agents.md`; what was built overrides them (`16-implementation-notes.md`, `16o-phase-index.md`). Phase overview and deviations: `p67-plan-1.md`.

### Task p67.7: e2e: offline replay of an agent run; no log holds the key

Through the binary, with the local Messages API stand-in (`newLLMAPI`, `runLLMCase`, `e2e/p7_helpers_test.go`). A replay's error names the script's path and its header names the working directory, so the replay must run the same file in the same directory: `llmCase` gains `path` and `dir` (both optional, so every Phase 7 case is unchanged). `TestP67ReplayOfAnAgentRunIsOffline` records an agent that calls an allowed tool twice and a denied one, then an `ask`, `random` and `now`, under `--tokens` and `--usd-cents`; the log holds 2 `llm_model`, 3 `llm`, 3 `llm_reserve` (p67.3) and 2 `http_get` records and no key; `--replay` with no key and no `--allow` prints the same bytes, names the grants, sends no request and reaches no tool gate. `TestP67LogsNeverHoldTheKey` records each Phase 7 way of getting the key out (`env=*`, a reply echoing it, a tool call carrying it, provider errors echoing it, a 500 echoing it) and checks the log, then replays it without a key: the same stdout, the same ending, no request. Both pin built behaviour: expect them to pass.

**Files:**
- Modify (test): `e2e/p7_helpers_test.go`
- Create (test): `e2e/p67_replay_test.go`

- [ ] **Step 1: Let a case choose its path and directory**

In `e2e/p7_helpers_test.go` (edit 1 of 3), replace:

```go
	noKey   bool                       // run without ANTHROPIC_API_KEY

```

with:

```go
	noKey   bool                       // run without ANTHROPIC_API_KEY
	path    string                     // where the program is written; "" for a new temp file
	dir     string                     // the run's working directory; "" for a new temp dir

```

In `e2e/p7_helpers_test.go` (edit 2 of 3), replace:

```go
	api := newLLMAPI(t, c.respond, c.script...)
	path := filepath.Join(t.TempDir(), "prog.arv")
	mustWrite(t, path, expand(t, c.src, nil))
```

with:

```go
	api := newLLMAPI(t, c.respond, c.script...)
	path := c.path
	if path == "" {
		path = filepath.Join(t.TempDir(), "prog.arv")
	}
	mustWrite(t, path, expand(t, c.src, nil))
```

In `e2e/p7_helpers_test.go` (edit 3 of 3), replace:

```go
	s := spec{
		args: append(append([]string{"run"}, c.args...), path), env: env, stdout: c.stdout,
		stderrNot: append([]string{e2eKey}, goCrash...), stdoutNot: []string{e2eKey}, timeout: 90 * time.Second,
```

with:

```go
	s := spec{
		args: append(append([]string{"run"}, c.args...), path), env: env, dir: c.dir, stdout: c.stdout,
		stderrNot: append([]string{e2eKey}, goCrash...), stdoutNot: []string{e2eKey}, timeout: 90 * time.Second,
```

- [ ] **Step 2: Write the tests**

Create `e2e/p67_replay_test.go`:

```go
package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Phase 6x7 through the binary: an agent run is recorded, and a replay of
// it makes no provider call and runs no tool, without a key; no log ever
// holds the key.

// p67Agent uses an allowed tool twice and a tool it does not hold, asks
// once more under the same budget, and draws a random number and the time.
const p67Agent = "let m = " + sonnet + ";\n" + weatherTools + `puts("start");
puts(agent(m, "Weather in Lisbon and Porto?", tools));
puts(match scope { ask(m, "And tomorrow?") } { ok(v) => v, err(e) => e["kind"] });
puts(random(1000000)); puts(now());
`

var p67Script = []apiReply{
	okTools(40, 9, use("t1", "weather", `{"city":"Lisbon"}`), use("t2", "weather", `{"city":"Porto"}`), use("t3", "notes", `{"path":"/x"}`)),
	okText("Bike: sunny", 60, 5),
	okText("rain", 5, 5),
}

// recorded is where a recorded run keeps its log and program, and the
// directory it runs in, so its replay runs the same file in the same
// place (an error names the file; the log names the directory).
type recorded struct{ dir, log, path string }

func newRecorded(t *testing.T) recorded {
	dir := t.TempDir()
	return recorded{dir: dir, log: filepath.Join(dir, "run.jsonl"), path: filepath.Join(dir, "prog.arv")}
}

// replayLog replays r's log with no key and no grants, pointing
// ARV_ANTHROPIC_URL at api so any call would be counted.
func replayLog(t *testing.T, r recorded, api *llmAPI) result {
	t.Helper()
	res := run(t, spec{args: []string{"run", "--replay", r.log, r.path}, dir: r.dir,
		env: []string{"ARV_ANTHROPIC_URL=" + api.URL + "/v1/messages"}, timeout: 90 * time.Second})
	if strings.Contains(res.stderr, "panic") || strings.Contains(res.stderr, "goroutine ") {
		t.Fatalf("the replay crashed\n%s", res)
	}
	return res
}

// noKeyIn fails if the file at path holds the API key.
func noKeyIn(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), e2eKey) {
		t.Fatalf("the log holds the API key:\n%s", data)
	}
	return string(data)
}

// I7 for agents, through the binary: the replay prints byte for byte what
// the recording printed, needs no key and no --allow, and sends nothing:
// the provider sees no request and no tool reaches the server.
func TestP67ReplayOfAnAgentRunIsOffline(t *testing.T) {
	t.Parallel()
	r := newRecorded(t)
	rec, api := runLLMCase(t, llmCase{src: p67Agent, script: p67Script, anyStdout: true, calls: 3, path: r.path, dir: r.dir,
		args: []string{"--record", r.log, "--allow", allowLLM, "--allow", "tool=weather", "--allow", "net=127.0.0.1",
			"--tokens", "100000", "--usd-cents", "100"},
		verify: wantArrivals(map[string]int{"w-Lisbon": 1, "w-Porto": 1, "notes": 0})})
	if !strings.HasPrefix(rec.stdout, "start\nBike: sunny\nrain\n") {
		t.Fatalf("recording\n%s", rec)
	}
	data := noKeyIn(t, r.log)
	// One llm_model per ask or agent; one llm and one llm_reserve per call.
	for kind, n := range map[string]int{"llm_model": 2, "llm": 3, "llm_reserve": 3, "http_get": 2} {
		if got := strings.Count(data, `"kind":"`+kind+`","req"`); got != n {
			t.Errorf("the log has %d %s records, want %d", got, kind, n)
		}
	}
	rep := replayLog(t, r, api)
	if rep.code != 0 || rep.stdout != rec.stdout || rep.stderr != `replay: grants: llm("anthropic:*"), tool("weather"), net("127.0.0.1")`+"\n" {
		t.Fatalf("replay: want stdout %q\n%s", rec.stdout, rep)
	}
	if n := len(api.requests()); n != 3 {
		t.Fatalf("the replay sent %d requests", n-3)
	}
	wantArrivals(map[string]int{"w-Lisbon": 1, "w-Porto": 1, "notes": 0})(t, rep, api)
}

// Every Phase 7 way of getting the key out, recorded: the log holds no
// key, and a replay without one prints and ends the same and sends
// nothing.
func TestP67LogsNeverHoldTheKey(t *testing.T) {
	t.Parallel()
	agent := "let m = " + sonnet + ";\n" + weatherTools + "puts(agent(m, \"g\", tools))\n"
	cases := []llmCase{
		{name: "env=* reads no key", src: `puts("[" + getenv("ANTHROPIC_API_KEY") + "]")`, args: []string{"--allow", "env=*"}, stdout: "[]\n"},
		{name: "a reply echoing the key", src: tryAsk + "puts(try(\"q\"))\n", args: []string{"--allow", allowLLM},
			script: []apiReply{okText("debug: $KEY", 5, 5)}, calls: 1, anyStdout: true},
		{name: "a tool call carrying the key", src: agent, args: []string{"--allow", allowLLM, "--allow", "tool=*", "--allow", "net=127.0.0.1"},
			script: []apiReply{okTools(40, 9, use("t1", "weather", `{"city":"$KEY"}`))}, calls: 1, err: "the reply contains the API key"},
		{name: "a provider error echoing the key", src: tryAsk + "puts(try(\"q\"))\n", args: []string{"--allow", allowLLM},
			script: []apiReply{apiError(401, "authentication_error", "invalid x-api-key: $KEY")}, calls: 1, anyStdout: true},
		{name: "an uncaught provider error", src: "puts(ask(" + haiku + ", \"q\"))\n", args: []string{"--allow", allowLLM},
			script: []apiReply{apiError(403, "permission_error", "key $KEY may not")}, calls: 1, err: "[redacted]"},
		{name: "a server error echoing the key", src: tryAsk + "puts(try(\"q\"))\n", args: []string{"--allow", allowLLM},
			script: []apiReply{{status: 500, body: "boom $KEY"}}, calls: 1, anyStdout: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := newRecorded(t)
			c.path, c.dir = r.path, r.dir
			c.args = append([]string{"--record", r.log}, c.args...)
			rec, api := runLLMCase(t, c)
			noKeyIn(t, r.log)
			rep := replayLog(t, r, api)
			// The replay names the grants, then ends as the recording did.
			grants, ended, _ := strings.Cut(rep.stderr, "\n")
			if rep.code != rec.code || rep.stdout != rec.stdout || ended != rec.stderr ||
				!strings.HasPrefix(grants, "replay: grants: ") || len(api.requests()) != c.calls {
				t.Fatalf("replay: %d requests; recording\n%s\nreplay\n%s", len(api.requests())-c.calls, rec, rep)
			}
			if strings.Contains(rep.stdout+rep.stderr, e2eKey) {
				t.Fatalf("the replay printed the key\n%s", rep)
			}
		})
	}
}
```

- [ ] **Step 3: Run them, and the Phase 7 cases**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test ./e2e -run 'TestP67ReplayOfAnAgentRunIsOffline|TestP67LogsNeverHoldTheKey|TestPhase7' -count=1`

Expected: PASS. (Without p67.3 the offline test reports `the log has 0 llm_reserve records, want 3`.)

- [ ] **Step 4: Check and commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && gofmt -l . && go vet ./... && go test -race ./...
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && git add -A && git commit -m "test(e2e): agent runs replay offline; no log holds the API key

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
