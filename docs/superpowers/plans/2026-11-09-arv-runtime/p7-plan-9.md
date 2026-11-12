# Phase 7, task p7.9: `ask` under budgets; `arv.Options.LLM`

> **For agentic workers:** this file is one task, p7.9. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p7.8 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous. No test may reach the network or spend money: never set `ARV_LIVE_ANTHROPIC`, and never put a real API key anywhere.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3, 4 and 5 merged; Phase 6 is not) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `14-phase7-agents.md`. Deviations from it, and why: `p7-plan-1.md`.

### Task p7.9: `ask` under budgets; `arv.Options.LLM`

Each call now reserves its most from the scope's budgets before the request: the input estimate (`estimateInput`: bytes plus framing, since a token is at least a byte) plus `maxTokens` of output, and their cost at the model's price. If that cannot be covered, the call is refused and the provider never sees it. After the reply, the reservation settles to the reported usage, so a budget that holds one reservation at a time still runs several small calls. Under a `usd_cents` limit a model with no price is refused. A failed call settles to 0.

An embedder passes providers with `arv.Options.LLM`; `Run` puts the host on the root context.

**Files:**
- Modify: `arv/evaluator/llmcall.go`
- Modify: `arv/arv.go`
- Create (test): `arv/evaluator/ask_budget_test.go`
- Create (test): `arv/llm_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/ask_budget_test.go`:

```go
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
```

Create `arv/llm_test.go`:

```go
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
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run '^(TestAskTokensBudget|TestAskUSDBudget|TestAskOverageIsABudgetError|TestRunWithModelProviders)$' ./arv/evaluator/ ./arv/`

Expected: FAIL: package `arv` does not compile (no `Options.LLM`), and the three evaluator tests get replies their budgets should have refused. The output includes:

```text
    ask_budget_test.go:15: got STRING a, want a budget error
    ask_budget_test.go:35: got STRING b, want a budget error
    ask_budget_test.go:49: got STRING a, want a budget error
# github.com/delavalom/arvlang/arv [github.com/delavalom/arvlang/arv.test]
```

- [ ] **Step 3: Implement**

In `arv/evaluator/llmcall.go`, replace:

```go
// maxTokens is the most output tokens one model call may write.
const maxTokens = 4096
```

with:

```go
// maxTokens is the most output tokens one model call may write. It is
// reserved from the tokens and usd_cents budgets before every call.
const maxTokens = 4096

// inputOverhead is added to every call's input estimate, for what the
// provider adds around the text (roles, and its tool-use prompt when the
// call offers tools).
const (
	inputOverhead     = 512
	toolInputOverhead = 1024
)
```

In `arv/evaluator/llmcall.go`, replace:

```go
// complete makes one call to m: it calls the provider in the task's
// context and charges the reply's bytes to alloc_mb. A refusal, a reply
// cut off at max_tokens, or a stop reason it does not know is an effect
// error.
func (in *Interpreter) complete(fn string, m model, req llm.Request) (llm.Response, *value.Error) {
	req.Model, req.MaxTokens = m.id, maxTokens
	resp, err := m.provider.Complete(in.Task.Ctx, req)
	if err != nil {
		if terr := in.Task.Err(); terr != nil {
```

with:

```go
// complete makes one call to m: it reserves the most the call can use
// from the scope's tokens and usd_cents budgets (refusing it, with no
// request sent, if they cannot cover that), calls the provider in the
// task's context, settles the reservation to the real usage, and charges
// the reply's bytes to alloc_mb. A refusal, a reply cut off at
// max_tokens, or a stop reason it does not know is an effect error.
func (in *Interpreter) complete(fn string, m model, req llm.Request) (llm.Response, *value.Error) {
	req.Model, req.MaxTokens = m.id, maxTokens
	b := in.top().budget
	if b.LimitsUSD() && !m.priced {
		return llm.Response{}, newErrorKind(runtime.KindBudget, "%s: no price is known for %s, so its usd_cents cannot be counted", fn, m.spec)
	}
	est := estimateInput(req)
	r, err := b.Reserve(int64(est+req.MaxTokens), m.price.Cost(est, req.MaxTokens))
	if err != nil {
		return llm.Response{}, errorOf(err, runtime.KindBudget)
	}
	resp, err := m.provider.Complete(in.Task.Ctx, req)
	if err != nil {
		r.Settle(0, 0) // see Known limits: a failed call is assumed to cost nothing
		if terr := in.Task.Err(); terr != nil {
```

In `arv/evaluator/llmcall.go`, after:

```go
		return llm.Response{}, newErrorKind(runtime.ErrorKind(err, runtime.KindEffect), "%s: %s", fn, err)
	}
```

add:

```go
	used := int64(resp.InputTokens) + int64(resp.OutputTokens)
	if err := r.Settle(used, m.price.Cost(resp.InputTokens, resp.OutputTokens)); err != nil {
		return llm.Response{}, errorOf(err, runtime.KindBudget)
	}
```

In `arv/evaluator/llmcall.go`, before:

```go
// replySize is what a reply costs to keep: its blocks and their bytes.
```

add:

```go
// estimateInput bounds the input tokens req can use by its bytes: a token
// is at least one byte of text. The provider's own framing is
// inputOverhead, and toolInputOverhead more when tools are offered.
func estimateInput(req llm.Request) int {
	n := inputOverhead + len(req.System)
	if len(req.Tools) > 0 {
		n += toolInputOverhead
	}
	for _, t := range req.Tools {
		n += 64 + len(t.Name) + len(t.Description)
		for _, p := range t.Params {
			n += 32 + len(p)
		}
	}
	for _, msg := range req.Messages {
		for _, c := range msg.Content {
			n += 16 + len(c.Text) + len(c.ID) + len(c.Name) + len(c.Input) + len(c.Raw)
		}
	}
	return n
}

```

In `arv/arv.go`, replace:

```go
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
```

with:

```go
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/parser"
```

In `arv/arv.go`, replace:

```go
	Limits runtime.Limits       // the run's budget; zero values mean no limit, except LiveTasks (runtime.DefaultLiveTasks); a negative one is an error
}
```

with:

```go
	Limits runtime.Limits       // the run's budget; zero values mean no limit, except LiveTasks (runtime.DefaultLiveTasks); a negative one is an error
	LLM    *llm.Host            // model providers for ask and agent; nil: none. Its keys never become Arv values.
}
```

In `arv/arv.go`, after:

```go
		opts.Ctx = context.Background()
	}
```

add:

```go
	if opts.LLM != nil {
		opts.Ctx = llm.WithHost(opts.Ctx, opts.LLM)
	}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run '^(TestAskTokensBudget|TestAskUSDBudget|TestAskOverageIsABudgetError|TestRunWithModelProviders)$' ./arv/evaluator/ ./arv/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add arv/evaluator/ask_budget_test.go arv/llm_test.go arv/evaluator/llmcall.go arv/arv.go
git commit -m "feat: model calls reserve and settle tokens and usd_cents

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
