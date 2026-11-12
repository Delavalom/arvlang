# Phase 7, task p7.12: non-string tool results; conversation tests

> **For agentic workers:** this file is one task, p7.12. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p7.11 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous. No test may reach the network or spend money: never set `ARV_LIVE_ANTHROPIC`, and never put a real API key anywhere.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3, 4 and 5 merged; Phase 6 is not) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `14-phase7-agents.md`. Deviations from it, and why: `p7-plan-1.md`.

### Task p7.12: non-string tool results; conversation tests

A tool that returns something other than a STRING now gives the model JSON, built by `json(v)`'s encoder, which charges `alloc_mb` as it goes. A value JSON cannot hold (a function) goes back as its printed form, charged exactly as `puts` charges it. A `budget` or `cancelled` error from either stops the agent.

The other three tests pin behaviour p7.11 built: the conversation's exact shape (a thinking block goes back byte for byte), a `budget` error inside a tool stopping the agent, and argument errors. Only `TestAgentToolFailuresAreResults` fails before the change.

**Files:**
- Modify: `arv/evaluator/agent.go`
- Create (test): `arv/evaluator/agent_loop_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/agent_loop_test.go`:

```go
package evaluator

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// Every turn resends the same tools and the whole conversation, the
// model's replies unchanged (a thinking block among them), so the history
// only grows by appending.
func TestAgentConversationShape(t *testing.T) {
	thinking := llm.Block{Type: "thinking", Raw: json.RawMessage(`{"type":"thinking","thinking":"","signature":"sig"}`)}
	first := llm.ToolUseReply(20, 10, llm.ToolUse("t1", "weather", `{"city": "Lisbon"}`))
	first.Response.Content = append([]llm.Block{thinking, {Type: llm.BlockText, Text: "Checking."}}, first.Response.Content...)
	f := llm.NewFake(first, llm.TextReply("Done.", 40, 5))
	src := `agent("anthropic:claude-sonnet-5-5", "Bike?", {"weather": fn(city) { "sunny" }}, {"weather": "Weather now"})`
	got, _ := runLLM(t, src, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, sonnet), grant(t, runtime.Tool, "weather"))
	if got.Inspect() != "Done." {
		t.Fatalf("got %s", got.Inspect())
	}
	calls := f.Calls()
	wantTools := []llm.ToolDef{{Name: "weather", Description: "Weather now", Params: []string{"city"}}}
	for i, c := range calls {
		if !reflect.DeepEqual(c.Tools, wantTools) || c.Model != "claude-sonnet-5-5" {
			t.Fatalf("call %d: tools %+v, model %q", i, c.Tools, c.Model)
		}
	}
	m := calls[1].Messages
	if len(m) != 3 || m[0].Role != llm.RoleUser || m[1].Role != llm.RoleAssistant || m[2].Role != llm.RoleUser {
		t.Fatalf("messages = %+v", m)
	}
	if !reflect.DeepEqual(m[1].Content, first.Response.Content) {
		t.Fatalf("the reply was not sent back unchanged: %+v", m[1].Content)
	}
	if !reflect.DeepEqual(calls[0].Messages, m[:1]) {
		t.Fatal("the history was edited, not appended to")
	}
}

// A tool's failures go back to the model as error results, and the agent
// goes on; a non-string result goes back as JSON, or printed if JSON
// cannot hold it.
func TestAgentToolFailuresAreResults(t *testing.T) {
	f := llm.NewFake(
		llm.ToolUseReply(1, 1,
			llm.ToolUse("a", "nope", `{}`),
			llm.ToolUse("b", "fail", `{"x": 1}`),
			llm.ToolUse("c", "fail", `{"x": "boom"}`),
			llm.ToolUse("d", "list", `{}`),
			llm.ToolUse("e", "leak", `{}`),
			llm.ToolUse("f", "func", `{}`),
		),
		llm.TextReply("ok", 1, 1),
	)
	src := `let tools = {
  "fail": fn(x) { error("tool failed: " + x) },
  "list": fn() { [1, "two", {"k": true}] },
  "leak": fn() { match getenv("SECRET_NAME") { ok(v) => v, err(e) => e["message"] } },
  "func": fn() { fn() { 1 } }
};
agent("anthropic:m", "go", tools)`
	got, _ := runLLM(t, src, fakeHost(f, nil), runtime.Limits{},
		grant(t, runtime.LLM, "*"), grant(t, runtime.Tool, "*"), grant(t, runtime.Env, "PRIVATE_GRANT"))
	if got.Inspect() != "ok" {
		t.Fatalf("got %s", got.Inspect())
	}
	want := []llm.Block{
		{Type: llm.BlockToolResult, ID: "a", Text: `unknown tool "nope"`, IsError: true},
		{Type: llm.BlockToolResult, ID: "b", Text: `argument "x" must be a string`, IsError: true},
		{Type: llm.BlockToolResult, ID: "c", Text: "tool failed: boom", IsError: true},
		{Type: llm.BlockToolResult, ID: "d", Text: `[1,"two",{"k":true}]`},
		{Type: llm.BlockToolResult, ID: "e", Text: `capability denied: env("SECRET_NAME")`},
		{Type: llm.BlockToolResult, ID: "f", Text: "fn() {\n1\n}"},
	}
	if res := toolResults(t, f.Calls()[1]); !reflect.DeepEqual(res, want) {
		t.Fatalf("tool results:\n%+v\nwant\n%+v", res, want)
	}
}

// A budget error inside a tool stops the agent: the scope is spent, and
// the model must not be called again.
func TestAgentStopsOnABudgetErrorInATool(t *testing.T) {
	f := llm.NewFake(llm.ToolUseReply(1, 1, llm.ToolUse("a", "spin", `{}`)), llm.TextReply("never", 1, 1))
	src := `let spin = fn(n) { if (n == 0) { 0 } else { spin(n - 1) } };
scope (steps: 2000) { agent("anthropic:m", "go", {"spin": fn() { spin(100000) }}) }`
	got, _ := runLLM(t, src, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"), grant(t, runtime.Tool, "*"))
	wantKindHas(t, got, runtime.KindBudget, "budget exceeded: steps")
	if n := len(f.Calls()); n != 1 {
		t.Fatalf("calls = %d, want 1", n)
	}
}

func TestAgentArgumentErrors(t *testing.T) {
	host := fakeHost(llm.NewFake(), nil)
	all := []runtime.Capability{grant(t, runtime.LLM, "*"), grant(t, runtime.Tool, "*")}
	for src, msg := range map[string]string{
		`agent("anthropic:m", "g")`:                       "agent: want 3 or 4 arguments",
		`agent("anthropic:m", 1, {})`:                     "agent: want (STRING, STRING, HASH), got (STRING, INTEGER, HASH)",
		`agent("anthropic:m", "g", [])`:                   "agent: tools must be a HASH",
		`agent("anthropic:*", "g", {})`:                   "name one model",
		`agent("anthropic:m", "g", {"t": fn() { 1 }}, 5)`: "agent: descriptions must be a HASH",
	} {
		got, _ := runLLM(t, src, host, runtime.Limits{}, all...)
		wantKindHas(t, got, runtime.KindRuntime, msg)
	}
	got, _ := runLLM(t, `agent("anthropic:m", "g", {})`, host, runtime.Limits{}, grant(t, runtime.Tool, "*"))
	wantDenied(t, got, `llm("anthropic:m")`)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run '^(TestAgentConversationShape|TestAgentToolFailuresAreResults|TestAgentStopsOnABudgetErrorInATool|TestAgentArgumentErrors)$' ./arv/evaluator/`

Expected: FAIL in `TestAgentToolFailuresAreResults` only: results `d` and `f` are errors (`tool "list" returned ARRAY; a tool returns a STRING`) instead of JSON and a printed function. The output includes:

```text
    agent_loop_test.go:80: tool results:
        [{Type:tool_result Text:unknown tool "nope" ID:a Name: Input:[] IsError:true Raw:[]} {Type:tool_result Text:argument "x" must be a string ID:b Name: Input:[] IsError:true Raw:[]} {Type:tool_result Text:tool failed: boom ID:c Name: Input:[] IsError:true Raw:[]} {Type:tool_result Text:tool "list" returned ARRAY; a tool returns a STRING ID:d Name: Input:[] IsError:true Raw:[]} {Type:tool_result Text:capability denied: env("SECRET_NAME") ID:e Name: Input:[] IsError:false Raw:[]} {Type:tool_result Text:tool "func" returned FUNCTION; a tool returns a STRING ID:f Name: Input:[] IsError:true Raw:[]}]
```

- [ ] **Step 3: Implement**

In `arv/evaluator/agent.go`, replace:

```go
	}
	return fail(fmt.Sprintf("tool %q returned %s; a tool returns a STRING", use.Name, result.Type()))
}
```

with:

```go
	}
	// Anything else goes back as JSON, which json() charges as it builds
	// it; a value JSON cannot hold (a fn) as its printed form.
	switch enc := jsonEncode(in.Task, in.chargeBytes, result).(type) {
	case *value.String:
		return enc.Value, false, nil
	case *value.Error:
		if k := enc.ErrKind(); k == runtime.KindBudget || k == runtime.KindCancelled {
			return "", false, enc
		}
	}
	return in.inspectCharged(result)
}

// inspectCharged is v's printed form, charged exactly as it is built (as
// puts does), for a tool result JSON cannot hold.
func (in *Interpreter) inspectCharged(v value.Object) (string, bool, *value.Error) {
	var charged int64
	var stop *value.Error
	text, err := value.InspectChecked(v, func(n int64) error {
		if err := in.Task.Err(); err != nil {
			stop = newErrorKind(runtime.KindCancelled, "task %s cancelled: %s", in.Task.ID, err)
		} else if stop = in.charge(n); stop == nil {
			charged += n
			return nil
		}
		return fmt.Errorf("%s", stop.Message)
	})
	if stop != nil {
		return "", false, stop
	}
	if err != nil {
		return "", false, newError("agent: %s", err)
	}
	return text, false, in.charge(int64(len(text)) - charged)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run '^(TestAgentConversationShape|TestAgentToolFailuresAreResults|TestAgentStopsOnABudgetErrorInATool|TestAgentArgumentErrors)$' ./arv/evaluator/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add arv/evaluator/agent_loop_test.go arv/evaluator/agent.go
git commit -m "feat(evaluator): agent tools may return any value

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
