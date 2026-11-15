# Phase 7, task p7.11: the `agent` loop

> **For agentic workers:** this file is one task, p7.11. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p7.10 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous. No test may reach the network or spend money: never set `ARV_LIVE_ANTHROPIC`, and never put a real API key anywhere.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3, 4 and 5 merged; Phase 6 is not) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `14-phase7-agents.md`. Deviations from it, and why: `p7-plan-1.md`.

### Task p7.11: the `agent` loop

`agent(model, goal, tools[, descriptions])` sends the goal (redacted) with the tool definitions, and loops: each turn is one `complete` call. If the model stopped to use tools, each `tool_use` block runs in order, in the agent's own task: an unknown tool or one the task does not hold as `tool(name)` is not run, and its result is the error `unknown tool "x"` or `capability denied: tool("x")`; otherwise the function runs with the model's arguments, and its own effects are checked against the task's grants as usual. Every result is redacted before it goes back. The reply goes back unchanged as the assistant turn. The agent returns the text of the first reply that asks for no tools, and fails with `budget exceeded: max_turns (limit N)` before a turn past `max_turns`.

In this task a tool must return a STRING; p7.12 sends other values as JSON. Tests check the design's acceptance rows: allowed tool, denied tool, ungranted effect, `max_turns: 3`, and an agent spawned `with` exactly its grants.

**Files:**
- Create: `arv/evaluator/agent.go`
- Modify: `arv/evaluator/evaluator.go`
- Create (test): `arv/evaluator/agent_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/agent_test.go`:

```go
package evaluator

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// toolResults is the tool_result blocks of the last message of req.
func toolResults(t *testing.T, req llm.Request) []llm.Block {
	t.Helper()
	last := req.Messages[len(req.Messages)-1]
	if last.Role != llm.RoleUser {
		t.Fatalf("last message is %s, want user", last.Role)
	}
	for _, b := range last.Content {
		if b.Type != llm.BlockToolResult {
			t.Fatalf("block %+v in a tool-result message", b)
		}
	}
	return last.Content
}

const weatherTools = `let tools = {
  "weather": fn(city) { puts("weather ran for " + city); "sunny in " + city },
  "notes":   fn(path) { puts("notes ran"); read_file(path) }
};
`

// Acceptance: the model calls an allowed tool; it runs once with the
// model's arguments, and the final text is returned.
func TestAgentRunsAnAllowedTool(t *testing.T) {
	f := llm.NewFake(
		llm.ToolUseReply(20, 10, llm.ToolUse("t1", "weather", `{"city": "Lisbon"}`)),
		llm.TextReply("Bike to work.", 40, 5),
	)
	src := weatherTools + `agent("anthropic:claude-sonnet-5-5", "Bike today?", tools)`
	got, out := runLLM(t, src, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, sonnet), grant(t, runtime.Tool, "weather"))
	if s, ok := got.(*value.String); !ok || s.Value != "Bike to work." {
		t.Fatalf("got %s", got.Inspect())
	}
	if out != "weather ran for Lisbon\n" {
		t.Fatalf("output %q: the tool must run exactly once", out)
	}
	calls := f.Calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	res := toolResults(t, calls[1])
	if len(res) != 1 || res[0].ID != "t1" || res[0].Text != "sunny in Lisbon" || res[0].IsError {
		t.Fatalf("tool results = %+v", res)
	}
}

// Acceptance: the model calls a tool the task does not hold. It gets an
// error result saying capability denied, naming no grant the task holds,
// the tool does not run, and the program goes on.
func TestAgentDeniedToolIsAToolResult(t *testing.T) {
	f := llm.NewFake(
		llm.ToolUseReply(20, 10, llm.ToolUse("t1", "notes", `{"path": "/etc/passwd"}`)),
		llm.TextReply("I could not read the notes.", 40, 5),
	)
	src := weatherTools + `let a = agent("anthropic:claude-sonnet-5-5", "Read my notes", tools); puts("after"); a`
	got, out := runLLM(t, src, fakeHost(f, nil), runtime.Limits{},
		grant(t, runtime.LLM, sonnet), grant(t, runtime.Tool, "weather"), grant(t, runtime.Env, "PRIVATE_GRANT"))
	if got.Inspect() != "I could not read the notes." || out != "after\n" {
		t.Fatalf("got %s, output %q", got.Inspect(), out)
	}
	res := toolResults(t, f.Calls()[1])
	if len(res) != 1 || !res[0].IsError || res[0].Text != `capability denied: tool("notes")` {
		t.Fatalf("tool results = %+v", res)
	}
}

// Acceptance: an allowed tool whose effect the task does not hold. Its
// http_get is denied, and the model gets that, redacted, as an error result.
func TestAgentToolEffectIsChecked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the tool's request reached the server")
	}))
	defer srv.Close()
	f := llm.NewFake(
		llm.ToolUseReply(20, 10, llm.ToolUse("t1", "fetch", fmt.Sprintf(`{"url": %q}`, srv.URL))),
		llm.TextReply("The fetch was refused.", 40, 5),
	)
	src := `agent("anthropic:claude-sonnet-5-5", "Fetch it", {"fetch": fn(url) { http_get(url)["body"] }})`
	got, _ := runLLM(t, src, fakeHost(f, nil), runtime.Limits{},
		grant(t, runtime.LLM, sonnet), grant(t, runtime.Tool, "fetch"), grant(t, runtime.Net, "api.weather.example"))
	if got.Inspect() != "The fetch was refused." {
		t.Fatalf("got %s", got.Inspect())
	}
	res := toolResults(t, f.Calls()[1])
	if len(res) != 1 || !res[0].IsError || res[0].Text != "capability denied: net" {
		t.Fatalf("tool results = %+v", res)
	}
}

// Acceptance: max_turns: 3 with a model that always calls tools stops
// after three calls with a budget error.
func TestAgentMaxTurns(t *testing.T) {
	f := llm.NewFake()
	f.Then = func(ctx context.Context, req llm.Request) (llm.Response, error) {
		return llm.ToolUseReply(1, 1, llm.ToolUse(fmt.Sprintf("t%d", len(req.Messages)), "weather", `{"city": "x"}`)).Response, nil
	}
	src := weatherTools + `scope (max_turns: 3) { agent("anthropic:claude-sonnet-5-5", "loop", tools) }`
	got, _ := runLLM(t, src, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, sonnet), grant(t, runtime.Tool, "*"))
	wantKindHas(t, got, runtime.KindBudget, "budget exceeded: max_turns (limit 3)")
	if n := len(f.Calls()); n != 3 {
		t.Fatalf("calls = %d, want 3", n)
	}
	f = llm.NewFake()
	f.Then = func(ctx context.Context, req llm.Request) (llm.Response, error) {
		return llm.ToolUseReply(1, 1, llm.ToolUse("t", "weather", `{"city": "x"}`)).Response, nil
	}
	got, _ = runLLM(t, weatherTools+`agent("anthropic:claude-sonnet-5-5", "loop", tools)`, fakeHost(f, nil), runtime.Limits{},
		grant(t, runtime.LLM, sonnet), grant(t, runtime.Tool, "*"))
	wantKindHas(t, got, runtime.KindBudget, fmt.Sprintf("max_turns (limit %d)", runtime.DefaultMaxTurns))
}

// The design's example: the agent is a task holding exactly the model,
// one tool and that tool's host, inside a scope with token and dollar
// budgets. It cannot use notes, though notes is in the hash.
func TestSpawnedAgentHoldsOnlyItsGrants(t *testing.T) {
	before := goruntime.NumGoroutine()
	f := llm.NewFake(
		llm.ToolUseReply(20, 10, llm.ToolUse("a", "notes", `{"path": "/tmp/x"}`), llm.ToolUse("b", "weather", `{"city": "Lisbon"}`)),
		llm.TextReply("Yes, bike.", 40, 5),
	)
	src := weatherTools + `scope (tokens: 50000, usd_cents: 25, timeout_ms: 60000) {
  await spawn agent("anthropic:claude-sonnet-5-5", "Should I bike to work in Lisbon today?", tools)
    with [llm("anthropic:claude-sonnet-5-5"), tool("weather"), net("api.weather.example")]
}`
	got, out := runLLM(t, src, fakeHost(f, nil), runtime.Limits{},
		grant(t, runtime.LLM, "anthropic:*"), grant(t, runtime.Tool, "*"), grant(t, runtime.Net, "api.weather.example"),
		grant(t, runtime.FSRead, "/"))
	if got.Inspect() != "Yes, bike." || out != "weather ran for Lisbon\n" {
		t.Fatalf("got %s, output %q", got.Inspect(), out)
	}
	res := toolResults(t, f.Calls()[1])
	if len(res) != 2 || res[0].ID != "a" || !res[0].IsError || !strings.Contains(res[0].Text, "capability denied") ||
		res[1].ID != "b" || res[1].IsError || res[1].Text != "sunny in Lisbon" {
		t.Fatalf("tool results = %+v", res)
	}
	settled(t, before)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run '^(TestAgentRunsAnAllowedTool|TestAgentDeniedToolIsAToolResult|TestAgentToolEffectIsChecked|TestAgentMaxTurns|TestSpawnedAgentHoldsOnlyItsGrants)$' ./arv/evaluator/`

Expected: FAIL: `identifier not found: agent`. The output includes:

```text
    agent_test.go:48: got ERROR: identifier not found: agent
    agent_test.go:75: got ERROR: identifier not found: agent, output ""
    agent_test.go:98: got ERROR: identifier not found: agent
    agent_test.go:115: error "identifier not found: agent" has kind "runtime", want "budget"
```

- [ ] **Step 3: Implement**

Create `arv/evaluator/agent.go`:

```go
package evaluator

import (
	"fmt"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

var agentBuiltin = interpreterBuiltin("agent")

func init() { builtins["agent"] = agentBuiltin }

// agent(model, goal, tools) runs a model in a loop with tools, and returns
// its final text. tools is a HASH from tool name to fn (see agentTools); an
// optional fourth argument, a HASH from tool name to STRING, describes
// them to the model.
//
// Each turn is one call (see complete). When the model stops to use tools,
// each tool_use block runs in order, in this task, and its result goes back
// as a tool_result: a tool the task does not hold as tool(name) is not run
// and the model gets "capability denied: tool(...)" as an error result; a
// tool's own effects are checked against the task's other grants as usual;
// an error from the tool goes back as an error result. Every result is
// redacted (runtime.RedactDenials) before the model sees it. A budget or
// cancellation error stops the agent with that error. The agent stops at
// any other stop reason, returning the text, or with a budget error once
// it would start a turn past max_turns.
func (in *Interpreter) agent(args []value.Object) value.Object {
	if len(args) != 3 && len(args) != 4 {
		return newError("agent: want 3 or 4 arguments (model, goal, tools[, descriptions]), got %d", len(args))
	}
	spec, ok1 := args[0].(*value.String)
	goal, ok2 := args[1].(*value.String)
	if !ok1 || !ok2 {
		return newError("agent: want (STRING, STRING, HASH), got (%s, %s, %s)", args[0].Type(), args[1].Type(), args[2].Type())
	}
	var descs value.Object
	if len(args) == 4 {
		descs = args[3]
	}
	tools, defs, errv := agentTools(args[2], descs)
	if errv != nil {
		return errv
	}
	m, errv := in.model("agent", spec.Value)
	if errv != nil {
		return errv
	}
	req := llm.Request{Tools: defs, Messages: []llm.Message{userText(goal.Value)}}
	turns := in.top().budget.MaxTurns()
	for turn := 1; ; turn++ {
		if turn > turns {
			return errorOf(&runtime.ExceededError{Kind: "max_turns", Limit: int64(turns)}, runtime.KindBudget)
		}
		resp, errv := in.complete("agent", m, req)
		if errv != nil {
			return errv
		}
		// The whole reply goes back, thinking blocks included, unchanged.
		req.Messages = append(req.Messages, llm.Message{Role: llm.RoleAssistant, Content: resp.Content})
		results, errv := in.runTools(tools, resp)
		if errv != nil {
			return errv
		}
		if len(results) == 0 {
			return &value.String{Value: resp.Text()} // charged in complete
		}
		req.Messages = append(req.Messages, llm.Message{Role: llm.RoleUser, Content: results})
	}
}

// runTools runs the reply's tool_use blocks, in order, and returns one
// tool_result for each; none unless the model stopped to use tools.
func (in *Interpreter) runTools(tools map[string]agentTool, resp llm.Response) ([]llm.Block, *value.Error) {
	if resp.StopReason != llm.StopToolUse {
		return nil, nil
	}
	var results []llm.Block
	for _, use := range resp.Content {
		if use.Type != llm.BlockToolUse {
			continue
		}
		text, isErr, errv := in.runTool(tools, use)
		if errv != nil {
			return nil, errv
		}
		if e := in.charge(blockSize); e != nil {
			return nil, e
		}
		results = append(results, llm.Block{Type: llm.BlockToolResult, ID: use.ID, Text: runtime.RedactDenials(text), IsError: isErr})
	}
	return results, nil
}

// runTool runs one tool call in this task, so with exactly the agent's
// authority, and returns the text for its tool_result and whether it is an
// error. It charges what it builds. A budget or cancellation error is
// returned as errv instead: the agent must stop.
func (in *Interpreter) runTool(tools map[string]agentTool, use llm.Block) (text string, isErr bool, errv *value.Error) {
	fail := func(msg string) (string, bool, *value.Error) {
		return msg, true, in.charge(int64(len(msg)))
	}
	tool, ok := tools[use.Name]
	if !ok {
		return fail(fmt.Sprintf("unknown tool %s", quoteOption(&value.String{Value: use.Name})))
	}
	if err := in.Task.Require(runtime.Capability{Kind: runtime.Tool, Scope: use.Name}); err != nil {
		return fail(err.(*runtime.DeniedError).Redacted())
	}
	strs, err := toolArgs(use.Input, tool.params)
	if err != nil {
		return fail(err.Error())
	}
	vals := make([]value.Object, len(strs))
	for i, s := range strs {
		if e := in.charge(int64(len(s))); e != nil {
			return "", false, e
		}
		vals[i] = &value.String{Value: s}
	}
	result := in.applyFunction(tool.fn, vals)
	if e, ok := result.(*value.Error); ok {
		if k := e.ErrKind(); k == runtime.KindBudget || k == runtime.KindCancelled || in.Task.Err() != nil {
			return "", false, e
		}
		return fail(e.Message)
	}
	if s, ok := result.(*value.String); ok {
		return s.Value, false, nil // already charged
	}
	return fail(fmt.Sprintf("tool %q returned %s; a tool returns a STRING", use.Name, result.Type()))
}
```

In `arv/evaluator/evaluator.go`, replace:

```go
			return in.ask(args)
		}
```

with:

```go
			return in.ask(args)
		case agentBuiltin:
			return in.agent(args)
		}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run '^(TestAgentRunsAnAllowedTool|TestAgentDeniedToolIsAToolResult|TestAgentToolEffectIsChecked|TestAgentMaxTurns|TestSpawnedAgentHoldsOnlyItsGrants)$' ./arv/evaluator/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add arv/evaluator/agent_test.go arv/evaluator/agent.go arv/evaluator/evaluator.go
git commit -m "feat(evaluator): agent

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
