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
