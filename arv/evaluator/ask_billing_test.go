package evaluator

import (
	"context"
	"math"
	goruntime "runtime"
	"sync/atomic"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// A call cancelled after it may have reached the provider keeps its whole
// reservation: the provider may bill it, and the program chose to cancel.
// Refunding it let a loop of cancelled calls spend without limit under
// tokens and usd_cents (review round 1, Critical).
func TestCancelledCallKeepsItsReservation(t *testing.T) {
	before := goruntime.NumGoroutine()
	var started atomic.Int64
	f := llm.NewFake()
	f.Then = func(ctx context.Context, req llm.Request) (llm.Response, error) {
		started.Add(1) // the provider accepted the call and bills it
		<-ctx.Done()
		return llm.Response{}, context.Cause(ctx)
	}
	src := `let one = fn() { match scope (timeout_ms: 50) { ask("anthropic:claude-sonnet-5-5", "write a long essay") } { ok(v) => 1, err(e) => 0 } };
scope (tokens: 10000, usd_cents: 10) { for i in [0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0] { one() }; "done" }`
	got, _ := runLLM(t, src, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"))
	if got.Inspect() != "done" {
		t.Fatalf("got %s", got.Inspect())
	}
	// One reservation is about 4.7K tokens: 10000 covers two.
	if n := len(f.Calls()); n > 2 {
		t.Fatalf("the provider got %d calls (%d started) under a budget that covers 2", n, started.Load())
	}
	settled(t, before)
}

// A provider error that shows the call was never processed (an HTTP error
// status, or a request refused before sending) gives the reservation back.
func TestUnprocessedCallCostsNothing(t *testing.T) {
	for _, e := range []error{
		&llm.Error{Provider: "anthropic", Kind: llm.ErrRateLimit, Status: 429},
		&llm.Error{Provider: "anthropic", Kind: llm.ErrOverloaded, Status: 529},
		&llm.Error{Provider: "anthropic", Kind: llm.ErrInvalid}, // refused before sending
	} {
		f := llm.NewFake(llm.Reply{Err: e}, llm.TextReply("b", 10, 5))
		src := `scope (tokens: 6000) { match ask("anthropic:m", "x") { ok(v) => v, err(e) => "a" } + ask("anthropic:m", "y") }`
		got, _ := runLLM(t, src, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"))
		if got.Inspect() != "ab" {
			t.Fatalf("%v: got %s, want ab", e, got.Inspect())
		}
	}
}

// Any other failure may have been billed: a network error once the
// request may have gone out, a 200 that could not be read, a 5xx other
// than 529.
func TestFailedCallMayBeBilled(t *testing.T) {
	for _, e := range []error{
		&llm.Error{Provider: "anthropic", Kind: llm.ErrNetwork},
		&llm.Error{Provider: "anthropic", Kind: llm.ErrDecode, Status: 200},
		&llm.Error{Provider: "anthropic", Kind: llm.ErrServer, Status: 504}, // a gateway, after the API started
		llm.ErrNoReply,
	} {
		f := llm.NewFake(llm.Reply{Err: e}, llm.TextReply("b", 10, 5))
		src := `scope (tokens: 6000) { match ask("anthropic:m", "x") { ok(v) => v, err(e) => "a" } + ask("anthropic:m", "y") }`
		got, _ := runLLM(t, src, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"))
		wantKindHas(t, got, runtime.KindBudget, "budget exceeded: tokens (limit 6000)")
		if n := len(f.Calls()); n != 1 {
			t.Fatalf("%v: the provider got %d calls, want 1", e, n)
		}
	}
}

// Usage a provider reports is checked: a negative count, or counts whose
// sum overflows, would otherwise settle the call to nothing. Such a call
// is charged at least its whole reservation.
func TestBadUsageIsChargedInFull(t *testing.T) {
	for _, r := range []llm.Reply{
		llm.TextReply("ok", math.MaxInt64/2+1, math.MaxInt64/2+1),
		llm.TextReply("ok", -1, 5),
		llm.TextReply("ok", 5, -1),
	} {
		f := llm.NewFake(r, llm.TextReply("ok2", 1, 1))
		src := `scope (tokens: 6000) { let a = match ask("anthropic:m", "hi") { ok(v) => v, err(e) => e["message"] }; [a, ask("anthropic:m", "hi")] }`
		got, _ := runLLM(t, src, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"))
		wantKindHas(t, got, runtime.KindBudget, "budget exceeded: tokens (limit 6000)")
		if n := len(f.Calls()); n != 1 {
			t.Fatalf("usage %d+%d: the provider got %d calls, want 1", r.Response.InputTokens, r.Response.OutputTokens, n)
		}
	}
}
