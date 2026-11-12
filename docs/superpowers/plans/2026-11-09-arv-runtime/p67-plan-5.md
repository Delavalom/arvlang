# Phase 6x7, task p67.5: a replay of an agent run runs no tool effect

> **For agentic workers:** this file is one task, p67.5. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p67.1 to p67.4 are committed on `phase/p67`. Work in `/Users/delavalom/delavalom-labs/arvlang-wt/p67` on branch `phase/p67`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 &&`: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push, never touch remotes; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `min`, `max` and `t.Chdir` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report status "blocked", reason "disk" (only `go clean -cache` may be deleted). The machine is shared and heavily loaded: keep wall-clock bounds generous and order events with server gates, not sleeps. No test may reach the network or spend money: every test uses the fake provider or a local `httptest` server; never set `ARV_LIVE_ANTHROPIC`, never put a real API key anywhere. If a test exposes a real bug outside this task, mark it `t.Skip("BUG: ...")` with a reproduction and report it; never weaken a test to make it pass.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` at `b516ca2` (Phases 3 to 8.3 and 5E merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`); each "expected failure" was seen. Designs: `13-phase6-durable-replay.md`, `14-phase7-agents.md`; what was built overrides them (`16-implementation-notes.md`, `16o-phase-index.md`). Phase overview and deviations: `p67-plan-1.md`.

### Task p67.5: a replay of an agent run runs no tool effect

The design's acceptance test, "replay (with Phase 6): 0 provider calls, same answer", for an agent with every kind of tool. `agentScript` is a fake model whose reply depends only on the request (the goal and the turn), so p67.6 can reuse it for resumed runs: each turn calls the planned tools, and the final answer quotes every tool result in the conversation. The run has one agent that uses every tool (a request, a file written and read back, `getenv`, a host effect, `random` and `now`, a tool it does not hold, a tool that fails), a spawned agent with fewer grants (it holds `k8s("staging")`, so this needs p67.2), and an agent stopped by `max_turns`, all under a `tokens` limit. The replay runs with no model host, no grants, a different `ARV_P67_HOME` and the written file deleted; it must print the same bytes and make no call, no request, no host call and no file. This pins what Phase 6 round 3 built: expect it to pass at once. If it fails, the gap is real: stop and report it.

**Files:**
- Create (test): `arv/agent_script_test.go`
- Create (test): `arv/replay_agent_test.go`

- [ ] **Step 1: Write the helper and the test**

Create `arv/agent_script_test.go`:

```go
package arv

import (
	"context"
	"fmt"
	"strings"

	"github.com/delavalom/arvlang/arv/llm"
)

// agentScript is a fake model for agent runs whose reply depends only on
// the request, never on the order in which calls arrive: plan[goal][k] is
// the tool calls of turn k+1, each "name:arg" (every tool takes one
// parameter, arg). The turn after the last is a final answer that quotes
// every tool result in the request, in order. A resumed run's live calls
// therefore get the replies an uninterrupted run got, and the output
// shows every tool result.
type agentScript map[string][][]string

func (s agentScript) reply(ctx context.Context, req llm.Request) (llm.Response, error) {
	goal := req.Messages[0].Content[0].Text
	turn := 0
	for _, m := range req.Messages {
		if m.Role == llm.RoleAssistant {
			turn++
		}
	}
	if turn < len(s[goal]) {
		var uses []llm.Block
		for i, call := range s[goal][turn] {
			name, arg, _ := strings.Cut(call, ":")
			uses = append(uses, llm.ToolUse(fmt.Sprintf("u%d_%d", turn, i), name, fmt.Sprintf(`{"arg": %q}`, arg)))
		}
		return llm.ToolUseReply(100, 10, uses...).Response, nil
	}
	var parts []string
	for _, m := range req.Messages {
		for _, b := range m.Content {
			if b.Type == llm.BlockToolResult {
				tag := "ok"
				if b.IsError {
					tag = "error"
				}
				parts = append(parts, tag+": "+b.Text)
			}
		}
	}
	return llm.TextReply(goal+" => "+strings.Join(parts, " | "), 100, 10).Response, nil
}

// fake is a provider that answers by the script.
func (s agentScript) fake() *llm.Fake { return &llm.Fake{Then: s.reply} }
```

Create `arv/replay_agent_test.go`:

```go
package arv

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// agentTools is every kind of tool an agent can have: each one's effect
// (a request, a file written and read, the environment, a host effect,
// randomness and time), one the agent does not hold, and one that fails.
const agentTools = `let m = "anthropic:claude-haiku-5-5";
let tools = {
  "fetch": fn(arg) { http_get("{{SRV}}" + arg)["body"] },
  "save": fn(arg) { write_file("{{DIR}}/" + arg, "saved " + arg); "wrote " + arg },
  "load": fn(arg) { read_file("{{DIR}}/" + arg) },
  "home": fn(arg) { getenv(arg) },
  "deploy": fn(arg) { k8s_apply("staging", arg) },
  "dice": fn(arg) { json(random(1000000)) + " at " + json(now()) },
  "secret": fn(arg) { "never runs" },
  "broken": fn(arg) { error("tool broke on " + arg) }
};
`

// agentRun is an agent run that uses every tool, a spawned agent with
// fewer grants, and an agent stopped by max_turns.
const agentRun = agentTools + `let worker = fn(goal) { agent(m, goal, tools) };
scope (tokens: 100000, max_turns: 4) {
  puts(agent(m, "main", tools));
  let t = spawn worker("child") with [llm(m), tool("fetch"), tool("deploy"), net("127.0.0.1"), k8s("staging")];
  puts(await t);
  puts(match scope { agent(m, "loop", tools) } { ok(v) => v, err(e) => e["kind"] + ": " + e["message"] });
  "end"
}`

var agentPlan = agentScript{
	"main":  {{"fetch:/a", "save:out.txt", "home:ARV_P67_HOME", "deploy:web.yaml", "dice:x", "secret:x", "broken:x"}, {"load:out.txt"}},
	"child": {{"fetch:/b", "deploy:api.yaml", "save:denied.txt"}},
	"loop":  {{"dice:x"}, {"dice:x"}, {"dice:x"}, {"dice:x"}, {"dice:x"}},
}

// agentWorld is what agentRun needs: a server, a directory, the grants,
// and the host effect, with counters for each.
type agentWorld struct {
	src     string
	dir     string
	hits    *atomic.Int64
	applied *atomic.Int64
	effects map[string]Effect
	caps    []runtime.Capability
}

func newAgentWorld(t *testing.T, src string) *agentWorld {
	srv, hits := countedServer(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := &agentWorld{dir: dir, hits: hits, applied: new(atomic.Int64)}
	w.src = strings.NewReplacer("{{SRV}}", srv.URL, "{{DIR}}", dir).Replace(src)
	w.effects = map[string]Effect{"k8s_apply": counted(w.applied, applied)}
	w.caps = []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}, {Kind: runtime.Net, Scope: "127.0.0.1"},
		{Kind: runtime.FSRead, Scope: dir}, {Kind: runtime.FSWrite, Scope: dir}, {Kind: runtime.Env, Scope: "ARV_P67_HOME"}, staging}
	for _, name := range []string{"fetch", "save", "load", "home", "deploy", "dice", "broken"} {
		w.caps = append(w.caps, runtime.Capability{Kind: runtime.Tool, Scope: name})
	}
	t.Setenv("ARV_P67_HOME", "/home/recorded")
	return w
}

// I7 for agents: a replay of an agent run prints byte for byte what the
// recording printed and ends the same, with no model host, no grants, a
// different environment and the files gone. It makes no provider call
// and runs no tool effect: no request, no file, no host call.
func TestReplayOfAnAgentRunsNoToolEffect(t *testing.T) {
	w := newAgentWorld(t, agentRun)
	fake := agentPlan.fake()
	out, res, l := recordOpts(t, w.src, Options{LLM: &llm.Host{Providers: map[string]llm.Provider{"anthropic": fake}}, Caps: w.caps, Effects: w.effects})
	for _, want := range []string{"ok: body of /a", "ok: wrote out.txt", "ok: /home/recorded", "ok: applied to staging", " at ",
		`error: capability denied: tool("secret")`, "error: tool broke on x", "ok: saved out.txt",
		"child => ok: body of /b | ok: applied to staging | error: capability denied: tool(\"save\")",
		"budget: budget exceeded: max_turns (limit 4)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the recording lacks %q:\n%s", want, out)
		}
	}
	calls, hits, applied := len(fake.Calls()), w.hits.Load(), w.applied.Load()
	if calls != 9 || hits != 2 || applied != 2 || res != `end` {
		t.Fatalf("recording: %d calls, %d requests, %d host calls, result %s", calls, hits, applied, res)
	}
	if err := os.Remove(filepath.Join(w.dir, "out.txt")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARV_P67_HOME", "/home/elsewhere")
	var got bytes.Buffer
	rres, err := Run(w.src, Options{Replay: l, Out: &got, Effects: w.effects})
	if err != nil || got.String() != out || resultText(rres, err) != res {
		t.Fatalf("replay: %v\nprinted %q\nwant    %q", err, got.String(), out)
	}
	if len(fake.Calls()) != calls || w.hits.Load() != hits || w.applied.Load() != applied {
		t.Fatalf("the replay ran effects: %d calls, %d requests, %d host calls", len(fake.Calls())-calls, w.hits.Load()-hits, w.applied.Load()-applied)
	}
	if _, err := os.Stat(filepath.Join(w.dir, "out.txt")); !os.IsNotExist(err) {
		t.Fatalf("the replay wrote a file: %v", err)
	}
}
```

- [ ] **Step 2: Run it**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test ./arv -run TestReplayOfAnAgentRunsNoToolEffect -count=1`

Expected: PASS. Without p67.2 it would fail with `the log's grants for a spawn include k8s("staging")`.

- [ ] **Step 3: Check and commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && gofmt -l . && go vet ./... && go test -race ./...
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && git add -A && git commit -m "test(arv): a replay of an agent run runs no tool effect

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
