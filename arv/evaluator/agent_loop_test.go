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
