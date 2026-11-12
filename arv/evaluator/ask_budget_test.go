package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// A call the tokens budget cannot cover is refused before the request.
func TestAskTokensBudget(t *testing.T) {
	f := llm.NewFake(llm.TextReply("a", 10, 5), llm.TextReply("b", 10, 5))
	src := `scope (tokens: 100) { ask("anthropic:claude-sonnet-5-5", "x") }`
	got, _ := runLLM(t, src, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"))
	wantKindHas(t, got, runtime.KindBudget, "budget exceeded: tokens (limit 100)")
	if len(f.Calls()) != 0 {
		t.Fatal("a refused call reached the provider")
	}
	// The reservation is settled to the real use: two calls fit in a
	// budget that holds only one reservation (about 4.7K tokens) at a time.
	src = `scope (tokens: 6000) { ask("anthropic:claude-sonnet-5-5", "x") + ask("anthropic:claude-sonnet-5-5", "y") }`
	got, _ = runLLM(t, src, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"))
	if got.Inspect() != "ab" {
		t.Fatalf("got %s, want ab", got.Inspect())
	}
}

// Acceptance: usd_cents: 1 with a price at which a call costs most of a
// cent. The second call is refused before it reaches the provider.
func TestAskUSDBudget(t *testing.T) {
	prices := map[string]llm.Price{sonnet: {Input: 0, Output: 2_000_000}} // 2 µ$ an output token
	f := llm.NewFake(llm.TextReply("a", 3, 4000), llm.TextReply("b", 3, 4000))
	src := `scope (usd_cents: 1) { ask("anthropic:claude-sonnet-5-5", "x"); ask("anthropic:claude-sonnet-5-5", "y") }`
	got, _ := runLLM(t, src, fakeHost(f, prices), runtime.Limits{}, grant(t, runtime.LLM, "*"))
	wantKindHas(t, got, runtime.KindBudget, "budget exceeded: usd_cents (limit 1)")
	if n := len(f.Calls()); n != 1 {
		t.Fatalf("the provider got %d calls, want 1", n)
	}
	// A model with no price cannot be counted against usd_cents.
	got, _ = runLLM(t, `scope (usd_cents: 5) { ask("anthropic:unpriced", "x") }`, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"))
	wantKindHas(t, got, runtime.KindBudget, "no price is known for anthropic:unpriced")
}

// Use beyond the reservation (a provider counting more input than the
// estimate) spends the budget: the call fails with a budget error.
func TestAskOverageIsABudgetError(t *testing.T) {
	f := llm.NewFake(llm.TextReply("a", 60_000, 5))
	got, _ := runLLM(t, `scope (tokens: 50000) { ask("anthropic:m", "x") }`, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"))
	wantKindHas(t, got, runtime.KindBudget, "budget exceeded: tokens (limit 50000)")
}
