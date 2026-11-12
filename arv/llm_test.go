package arv

import (
	"errors"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

func TestRunWithModelProviders(t *testing.T) {
	f := llm.NewFake(llm.TextReply("Lisbon", 5, 2))
	host := &llm.Host{Providers: map[string]llm.Provider{"anthropic": f}}
	caps := []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}}
	got, err := Run(`ask("anthropic:claude-haiku-5-5", "Capital of Portugal?")`, Options{LLM: host, Caps: caps})
	if err != nil || got.Inspect() != "Lisbon" {
		t.Fatalf("Run = %v, %v", got, err)
	}
	// The host's limits are the root of the model budgets.
	_, err = Run(`ask("anthropic:claude-haiku-5-5", "x")`, Options{LLM: host, Caps: caps, Limits: runtime.Limits{Tokens: 10}})
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != runtime.KindBudget || ae.Msg != "budget exceeded: tokens (limit 10)" {
		t.Fatalf("err = %v, want a tokens budget error", err)
	}
	// Without a host, a call is an effect error, not a panic.
	_, err = Run(`ask("anthropic:claude-haiku-5-5", "x")`, Options{Caps: caps})
	if !errors.As(err, &ae) || ae.Kind != runtime.KindEffect {
		t.Fatalf("err = %v, want an effect error", err)
	}
}
