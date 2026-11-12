package evaluator

import (
	"context"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func TestAskNeedsTheModelCapability(t *testing.T) {
	f := llm.NewFake(llm.TextReply("hi", 1, 1))
	got, _ := runLLM(t, `ask("anthropic:claude-sonnet-5-5", "hello")`, fakeHost(f, nil), runtime.Limits{},
		grant(t, runtime.LLM, "anthropic:claude-haiku-5-5"))
	wantDenied(t, got, `capability denied: llm("anthropic:claude-sonnet-5-5")`)
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("the provider got %d calls, want 0", n)
	}
}

func TestAskReturnsTheReply(t *testing.T) {
	f := llm.NewFake(llm.TextReply("Lisbon", 12, 3))
	got, _ := runLLM(t, `ask("anthropic:claude-sonnet-5-5", "Capital of Portugal?")`, fakeHost(f, nil), runtime.Limits{},
		grant(t, runtime.LLM, "anthropic:*"))
	if s, ok := got.(*value.String); !ok || s.Value != "Lisbon" {
		t.Fatalf("got %s, want Lisbon", got.Inspect())
	}
	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	c := calls[0]
	if c.Model != "claude-sonnet-5-5" || c.MaxTokens != maxTokens || len(c.Tools) != 0 ||
		len(c.Messages) != 1 || c.Messages[0].Role != llm.RoleUser || userTextOf(c) != "Capital of Portugal?" {
		t.Fatalf("request = %+v", c)
	}
}

func TestAskErrors(t *testing.T) {
	all := grant(t, runtime.LLM, "*")
	host := fakeHost(llm.NewFake(llm.Reply{Err: &llm.Error{Provider: "anthropic", Kind: llm.ErrRateLimit, Status: 429, Message: "slow down"}}), nil)
	got, _ := runLLM(t, `ask("anthropic:claude-sonnet-5-5", "x")`, host, runtime.Limits{}, all)
	wantKindHas(t, got, runtime.KindEffect, "ask: anthropic: rate_limit (HTTP 429): slow down")

	got, _ = runLLM(t, `ask("anthropic:*", "x")`, host, runtime.Limits{}, all)
	wantKindHas(t, got, runtime.KindRuntime, "name one model")
	got, _ = runLLM(t, `ask("anthropic:m", 1)`, host, runtime.Limits{}, all)
	wantKindHas(t, got, runtime.KindRuntime, "ask: want (STRING, STRING), got (STRING, INTEGER)")
	got, _ = runLLM(t, `ask("anthropic:m", "x")`, nil, runtime.Limits{}, all)
	wantKindHas(t, got, runtime.KindEffect, "the host configured no model providers")
	got, _ = runLLM(t, `ask("other:m", "x")`, host, runtime.Limits{}, all)
	wantKindHas(t, got, runtime.KindEffect, `no provider "other" is configured`)

	for _, c := range []struct {
		stop, msg string
	}{
		{llm.StopRefusal, "refused the request"}, {llm.StopMaxTokens, "reached max_tokens"}, {"pause_turn", `unexpected stop reason "pause_turn"`},
	} {
		r := llm.TextReply("partial", 1, 1)
		r.Response.StopReason = c.stop
		got, _ := runLLM(t, `ask("anthropic:m", "x")`, fakeHost(llm.NewFake(r), nil), runtime.Limits{}, all)
		wantKindHas(t, got, runtime.KindEffect, c.msg)
	}
}

// The reply's bytes are charged to alloc_mb.
func TestAskChargesTheReply(t *testing.T) {
	f := llm.NewFake(llm.TextReply(strings.Repeat("x", 2<<20), 1, 1))
	got, _ := runLLM(t, `scope (alloc_mb: 1) { ask("anthropic:m", "x") }`, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"))
	wantKindHas(t, got, runtime.KindBudget, "budget exceeded: alloc")
}

// A prompt carrying a capability denial goes out redacted: the model
// never sees the grants the task holds.
func TestAskRedactsDenialsInThePrompt(t *testing.T) {
	f := llm.NewFake(llm.TextReply("ok", 1, 1))
	src := `let msg = match getenv("SECRET_NAME") { ok(v) => v, err(e) => e["message"] };
ask("anthropic:m", "it said: " + msg)`
	got, _ := runLLM(t, src, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"), grant(t, runtime.Env, "PRIVATE_GRANT"))
	if got.Inspect() != "ok" {
		t.Fatalf("got %s", got.Inspect())
	}
	sent := userTextOf(f.Calls()[0])
	if sent != `it said: capability denied: env("SECRET_NAME")` || strings.Contains(sent, "PRIVATE_GRANT") {
		t.Fatalf("sent %q", sent)
	}
}

// A call blocked in the provider ends when its scope times out, and
// nothing it started outlives the run.
func TestAskIsCancelledWithItsScope(t *testing.T) {
	before := goruntime.NumGoroutine()
	f := llm.NewFake()
	f.Then = func(ctx context.Context, req llm.Request) (llm.Response, error) {
		<-ctx.Done()
		return llm.Response{}, context.Cause(ctx)
	}
	got, _ := runLLM(t, `scope (timeout_ms: 50) { ask("anthropic:m", "x") }`, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"))
	wantKindHas(t, got, runtime.KindBudget, "budget exceeded: timeout (limit 50 ms)")
	settled(t, before)
}
