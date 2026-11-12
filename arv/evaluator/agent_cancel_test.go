package evaluator

import (
	"context"
	goruntime "runtime"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// A scope timeout while a tool runs stops the agent: the model gets no
// further call, and nothing the run started outlives it.
func TestAgentStopsWhenATimeoutHitsATool(t *testing.T) {
	before := goruntime.NumGoroutine()
	f := llm.NewFake(llm.ToolUseReply(1, 1, llm.ToolUse("a", "wait", `{}`)), llm.TextReply("too late", 1, 1))
	src := `scope (timeout_ms: 100) { agent("anthropic:m", "go", {"wait": fn() { sleep(60000); "done" }}) }`
	got, _ := runLLM(t, src, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"), grant(t, runtime.Tool, "*"))
	wantKindHas(t, got, runtime.KindBudget, "budget exceeded: timeout (limit 100 ms)")
	if n := len(f.Calls()); n != 1 {
		t.Fatalf("the provider got %d calls, want 1", n)
	}
	settled(t, before)
}

// A timeout during a later turn's model call ends the agent the same way.
func TestAgentStopsWhenATimeoutHitsATurn(t *testing.T) {
	before := goruntime.NumGoroutine()
	f := llm.NewFake(llm.ToolUseReply(1, 1, llm.ToolUse("a", "t", `{}`)))
	f.Then = func(ctx context.Context, req llm.Request) (llm.Response, error) {
		<-ctx.Done()
		return llm.Response{}, context.Cause(ctx)
	}
	src := `scope (timeout_ms: 100) { agent("anthropic:m", "go", {"t": fn() { "ok" }}) }`
	got, out := runLLM(t, src, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"), grant(t, runtime.Tool, "*"))
	wantKindHas(t, got, runtime.KindBudget, "budget exceeded: timeout (limit 100 ms)")
	if n := len(f.Calls()); n != 2 || out != "" {
		t.Fatalf("the provider got %d calls, want 2; output %q", n, out)
	}
	settled(t, before)
}
