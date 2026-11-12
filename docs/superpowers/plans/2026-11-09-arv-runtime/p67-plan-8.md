# Phase 6x7, task p67.8: e2e: resume after SIGKILL in a model call or a tool

> **For agentic workers:** this file is one task, p67.8. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p67.1 to p67.7 are committed on `phase/p67`. Work in `/Users/delavalom/delavalom-labs/arvlang-wt/p67` on branch `phase/p67`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 &&`: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push, never touch remotes; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `min`, `max` and `t.Chdir` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report status "blocked", reason "disk" (only `go clean -cache` may be deleted). The machine is shared and heavily loaded: keep wall-clock bounds generous and order events with server gates, not sleeps. No test may reach the network or spend money: every test uses the fake provider or a local `httptest` server; never set `ARV_LIVE_ANTHROPIC`, never put a real API key anywhere. If a test exposes a real bug outside this task, mark it `t.Skip("BUG: ...")` with a reproduction and report it; never weaken a test to make it pass.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` at `b516ca2` (Phases 3 to 8.3 and 5E merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`); each "expected failure" was seen. Designs: `13-phase6-durable-replay.md`, `14-phase7-agents.md`; what was built overrides them (`16-implementation-notes.md`, `16o-phase-index.md`). Phase overview and deviations: `p67-plan-1.md`.

### Task p67.8: e2e: resume after SIGKILL in a model call or a tool

The acceptance test "resume after SIGKILL mid-agent-run equals an uninterrupted run", through the binary. The agent's one tool waits at a server gate (`/wait`, `e2e/gates_test.go`), so the test controls where the run is killed, with no sleeps. `weatherTurns` answers by the request body (a request holding a `tool_result` is the second turn), so the crashed run, the uninterrupted run and the resume each have their own stand-in and the test counts each one's requests. In a model call: the crashed run's stand-in never answers the second turn; the run is killed once it has received it; the resume sends exactly that request (byte for byte equal to the crashed run's and the uninterrupted run's) and the tool does not run again. In a tool: the run is killed while the tool's request waits at the gate; the resume sends no first turn again, makes the tool's request again, and sends the second turn as the uninterrupted run did. A replay of each resumed log then sends nothing. The logs never hold the key (`noKeyIn`, p67.7). Reuses `newRecorded`, `replayLog` (p67.7), `startArv`, `readLogLines`, `kinds` (`e2e/p6_accept_test.go`). Expect them to pass.

**Files:**
- Create (test): `e2e/p67_resume_test.go`

- [ ] **Step 1: Write the tests**

Create `e2e/p67_resume_test.go`:

```go
package e2e

import (
	"strings"
	"testing"
	"time"
)

// SIGKILL in the middle of an agent run, then --resume: the resumed run
// prints what an uninterrupted run prints, sends the provider only the
// call that was in flight (byte for byte the request the uninterrupted
// run sent) and the calls after it, and repeats no tool effect the log
// holds. A replay of the resumed log then sends nothing.

// weatherTurns answers an agent's first turn with one weather call for
// Lisbon and its second with the final text.
func weatherTurns(body string) apiReply {
	if strings.Contains(body, `"tool_result"`) {
		return okText("Bike: sunny", 60, 5)
	}
	return okTools(40, 9, use("t1", "weather", `{"city":"Lisbon"}`))
}

// p67Kill is an agent whose tool waits at gate {{K}} of the server.
const p67Kill = "let m = " + sonnet + `;
let tools = {"weather": fn(city) { http_get("{{SERVER}}/wait?id={{ID}}&k={{K}}&n=1")["body"] + " in " + city }};
puts("start");
puts(agent(m, "Weather in Lisbon?", tools));
puts("end")
`

// p67KillGrants are the grants; a recording also sets a tokens limit,
// which a resume takes from the log (it refuses limit flags).
var p67KillGrants = []string{"--allow", allowLLM, "--allow", "tool=weather", "--allow", "net=127.0.0.1"}

func apiEnv(api *llmAPI) []string {
	return []string{"ARV_ANTHROPIC_URL=" + api.URL + "/v1/messages", "ANTHROPIC_API_KEY=" + e2eKey}
}

// uninterrupted runs p67Kill to its end, its gate open.
func uninterrupted(t *testing.T) (result, *llmAPI) {
	t.Helper()
	arrive(testID(t), "open")
	return runLLMCase(t, llmCase{src: strings.ReplaceAll(p67Kill, "{{K}}", "open"), respond: weatherTurns,
		args: append([]string{"--tokens", "100000"}, p67KillGrants...), stdout: "start\nBike: sunny\nend\n", calls: 2})
}

// killAgent records p67Kill at gate "shut" against api, and kills the run
// once ready says so. It returns what the run printed.
func killAgent(t *testing.T, r recorded, api *llmAPI, ready func() bool) string {
	t.Helper()
	mustWrite(t, r.path, expand(t, p67Kill, map[string]string{"K": "shut"}))
	args := append(append([]string{"run", "--record", r.log, "--fsync", "--tokens", "100000"}, p67KillGrants...), r.path)
	cmd, out := startArv(t, r.dir, apiEnv(api), args...)
	deadline := time.Now().Add(gateTimeout)
	for !ready() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cmd.Process.Kill()
	cmd.Wait()
	if !ready() {
		t.Fatalf("the run was killed before it got there:\n%s", out)
	}
	noKeyIn(t, r.log)
	return out.String()
}

// resumeAgent resumes r against a provider that answers with weatherTurns.
func resumeAgent(t *testing.T, r recorded) (result, *llmAPI) {
	t.Helper()
	api := newLLMAPI(t, weatherTurns)
	res := run(t, spec{args: append(append([]string{"run", "--resume", r.log}, p67KillGrants...), r.path), dir: r.dir,
		env: apiEnv(api), timeout: 90 * time.Second})
	if res.code != 0 || res.stderr != "" || strings.Contains(res.stdout, e2eKey) {
		t.Fatalf("resume\n%s", res)
	}
	noKeyIn(t, r.log)
	return res, api
}

// Killed while the model's second turn is in flight: the resume sends
// that turn again, byte for byte, and nothing else; the tool, whose
// request the log holds, does not run again.
func TestP67ResumeAfterSIGKILLInAModelCall(t *testing.T) {
	t.Parallel()
	ref, refAPI := uninterrupted(t)
	arrive(testID(t), "shut")
	r := newRecorded(t)
	crashAPI := newLLMAPI(t, func(body string) apiReply {
		if strings.Contains(body, `"tool_result"`) {
			return apiReply{hang: true}
		}
		return weatherTurns(body)
	})
	if out := killAgent(t, r, crashAPI, func() bool { return len(crashAPI.requests()) == 2 }); out != "start\n" {
		t.Fatalf("before the kill: %q", out)
	}
	lines := readLogLines(t, r.log)
	if kinds(lines, "llm") != 1 || kinds(lines, "http_get") != 1 || lines[len(lines)-1].End != nil {
		t.Fatalf("the killed run's log: %d llm, %d http_get records", kinds(lines, "llm"), kinds(lines, "http_get"))
	}
	res, api := resumeAgent(t, r)
	sent := api.requests()
	if res.stdout != ref.stdout || len(sent) != 1 || sent[0] != crashAPI.requests()[1] || sent[0] != refAPI.requests()[1] {
		t.Fatalf("resume: %d requests, want the one in flight\n%s", len(sent), res)
	}
	if n := hits(t, "/wait"); n != 2 {
		t.Fatalf("the tool ran %d times, want 2 (the uninterrupted run and the killed one)", n)
	}
	rep := replayLog(t, r, api)
	if rep.code != 0 || rep.stdout != ref.stdout || len(api.requests()) != 1 || hits(t, "/wait") != 2 {
		t.Fatalf("replay of the resumed log\n%s", rep)
	}
}

// Killed while the tool's request is in flight: the resume sends no
// model call the log holds, makes the tool's request again, and then
// sends the second turn as the uninterrupted run sent it.
func TestP67ResumeAfterSIGKILLInATool(t *testing.T) {
	t.Parallel()
	ref, refAPI := uninterrupted(t)
	r := newRecorded(t)
	crashAPI := newLLMAPI(t, weatherTurns)
	if out := killAgent(t, r, crashAPI, func() bool { return hits(t, "/wait") == 2 }); out != "start\n" {
		t.Fatalf("before the kill: %q", out)
	}
	lines := readLogLines(t, r.log)
	if kinds(lines, "llm") != 1 || kinds(lines, "http_get") != 0 || len(crashAPI.requests()) != 1 {
		t.Fatalf("the killed run's log: %d llm, %d http_get records", kinds(lines, "llm"), kinds(lines, "http_get"))
	}
	arrive(testID(t), "shut")
	res, api := resumeAgent(t, r)
	sent := api.requests()
	if res.stdout != ref.stdout || len(sent) != 1 || sent[0] != refAPI.requests()[1] || hits(t, "/wait") != 3 {
		t.Fatalf("resume: %d requests, %d tool requests\n%s", len(sent), hits(t, "/wait"), res)
	}
	rep := replayLog(t, r, api)
	if rep.code != 0 || rep.stdout != ref.stdout || len(api.requests()) != 1 || hits(t, "/wait") != 3 {
		t.Fatalf("replay of the resumed log\n%s", rep)
	}
}
```

- [ ] **Step 2: Run it**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test ./e2e -run TestP67ResumeAfterSIGKILL -count=3`

Expected: PASS. Each run takes a few seconds; the kill waits for a request, never for time.

- [ ] **Step 3: Check and commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && gofmt -l . && go vet ./... && go test -race ./...
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && git add -A && git commit -m "test(e2e): resume after SIGKILL in a model call or a tool

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
