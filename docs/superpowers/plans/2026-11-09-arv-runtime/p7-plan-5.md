# Phase 7, task p7.5: `arv/llm`: prices and `Host`

> **For agentic workers:** this file is one task, p7.5. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p7.4 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous. No test may reach the network or spend money: never set `ARV_LIVE_ANTHROPIC`, and never put a real API key anywhere.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3, 4 and 5 merged; Phase 6 is not) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `14-phase7-agents.md`. Deviations from it, and why: `p7-plan-1.md`.

### Task p7.5: `arv/llm`: prices and `Host`

`Price` is micro-dollars per million tokens, with an optional long-context tier. `Cost` rounds up and saturates instead of overflowing, so a huge count can never wrap to a negative cost. `DefaultPrices` copies the `claude-api` skill's table; the comment records where and when. `Host` is what an embedder hands a run: providers by name, with their keys inside them, and price overrides. It travels on the run's context (`WithHost`, `HostFrom`); nothing in it becomes an Arv value.

**Files:**
- Create: `arv/llm/pricing.go`
- Create: `arv/llm/host.go`
- Create (test): `arv/llm/pricing_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/llm/pricing_test.go`:

```go
package llm

import (
	"context"
	"math"
	"testing"
)

func TestCost(t *testing.T) {
	opus := DefaultPrices["anthropic:claude-opus-5-5"] // $4 / $20
	if got := opus.Cost(1_000_000, 0); got != 4_000_000 {
		t.Errorf("1M input tokens = %d µ$, want 4000000", got)
	}
	if got := opus.Cost(0, 1000); got != 20_000 {
		t.Errorf("1000 output tokens = %d µ$, want 20000 (2 cents)", got)
	}
	haiku := DefaultPrices["anthropic:claude-haiku-5-5"] // $0.10 / $0.50, then $0.50 / $2.50
	if got := haiku.Cost(1, 0); got != 1 {
		t.Errorf("one Haiku token = %d µ$, want 1 (rounded up)", got)
	}
	if got := haiku.Cost(100_000, 0); got != 10_000 {
		t.Errorf("100K Haiku input = %d µ$, want 10000", got)
	}
	if got := haiku.Cost(200_000, 0); got != 100_000 {
		t.Errorf("200K Haiku input = %d µ$, want 100000 (long-prompt price)", got)
	}
	if got := opus.Cost(-5, -5); got != 0 {
		t.Errorf("negative usage = %d, want 0", got)
	}
	if got := (Price{Input: usd(1000) * 1_000_000, Output: 1}).Cost(1<<62, 1<<62); got < math.MaxInt64/1_000_000 {
		t.Errorf("huge usage = %d, want a saturated cost", got)
	}
}

func TestEveryDefaultPriceIsSet(t *testing.T) {
	for spec, p := range DefaultPrices {
		if _, _, err := ParseModel(spec); err != nil {
			t.Errorf("%s: %v", spec, err)
		}
		if p.Input <= 0 || p.Output <= p.Input {
			t.Errorf("%s: %+v, want output dearer than input, both set", spec, p)
		}
	}
}

func TestHost(t *testing.T) {
	fake := NewFake()
	h := &Host{Providers: map[string]Provider{"anthropic": fake}, Prices: map[string]Price{"anthropic:x": {Input: 1, Output: 2}}}
	p, model, err := h.Lookup("anthropic:claude-haiku-5-5")
	if err != nil || p != fake || model != "claude-haiku-5-5" {
		t.Fatalf("Lookup = %v, %q, %v", p, model, err)
	}
	if _, _, err := h.Lookup("other:m"); err == nil {
		t.Error("Lookup of an unconfigured provider succeeded")
	}
	if pr, ok := h.Price("anthropic:x"); !ok || pr.Output != 2 {
		t.Errorf("Price override = %+v, %v", pr, ok)
	}
	if pr, ok := h.Price("anthropic:claude-opus-5-5"); !ok || pr.Input != usd(4) {
		t.Errorf("default Price = %+v, %v", pr, ok)
	}
	if _, ok := h.Price("anthropic:unknown"); ok {
		t.Error("an unknown model has a price")
	}
	ctx := WithHost(context.Background(), h)
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	if HostFrom(child) != h || HostFrom(context.Background()) != nil {
		t.Fatal("HostFrom does not find the host through a derived context")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run '^(TestCost|TestEveryDefaultPriceIsSet|TestHost)$' ./arv/llm/`

Expected: FAIL: the package does not compile. The output includes:

```text
# github.com/delavalom/arvlang/arv/llm [github.com/delavalom/arvlang/arv/llm.test]
arv/llm/pricing_test.go:10:10: undefined: DefaultPrices
arv/llm/pricing_test.go:17:11: undefined: DefaultPrices
arv/llm/pricing_test.go:30:13: undefined: Price
```

- [ ] **Step 3: Implement**

Create `arv/llm/pricing.go`:

```go
package llm

import "math"

// Price is what a model charges, in micro-dollars (millionths of a US
// dollar) per million tokens: $4 per million is 4_000_000. Above
// LongAbove input tokens (when it is not 0) a request pays the Long
// prices instead, for input and output alike.
type Price struct {
	Input, Output         int64
	LongAbove             int
	LongInput, LongOutput int64
}

// Cost is what a request using in input and out output tokens costs, in
// micro-dollars, rounded up. Negative counts and prices cost nothing, and
// a cost too large for an int64 is math.MaxInt64 / 1_000_000 or more,
// never a wrapped-around negative.
func (p Price) Cost(in, out int) int64 {
	pin, pout := p.Input, p.Output
	if p.LongAbove > 0 && in > p.LongAbove {
		pin, pout = p.LongInput, p.LongOutput
	}
	sum := addSat(mulSat(int64(in), pin), mulSat(int64(out), pout))
	return sum/1_000_000 + min(sum%1_000_000, 1)
}

// mulSat is a*b for non-negative a and b (a negative one counts as 0),
// saturating at math.MaxInt64.
func mulSat(a, b int64) int64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a > math.MaxInt64/b {
		return math.MaxInt64
	}
	return a * b
}

// addSat is a+b for non-negative a and b, saturating at math.MaxInt64.
func addSat(a, b int64) int64 {
	if a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}

// usd is dollars per million tokens as micro-dollars per million tokens.
func usd(dollars float64) int64 { return int64(dollars*1_000_000 + 0.5) }

// DefaultPrices are Anthropic's first-party API prices, by "provider:model".
//
// Source: the claude-api skill's model table (cached 2026-11-03, which
// mirrors https://platform.claude.com/docs/en/about-claude/pricing),
// copied on 2026-11-11. Update this table and that date together, from
// the pricing page, never from memory. Batch, cache and fast-mode prices
// do not apply: Arv sends plain Messages requests.
var DefaultPrices = map[string]Price{
	"anthropic:claude-fable-5-2":  {Input: usd(8), Output: usd(40)},
	"anthropic:claude-mythos-5-2": {Input: usd(8), Output: usd(40)},
	"anthropic:claude-fable-5-1":  {Input: usd(10), Output: usd(50)},
	"anthropic:claude-mythos-5-1": {Input: usd(10), Output: usd(50)},
	"anthropic:claude-fable-5":    {Input: usd(10), Output: usd(50)},
	"anthropic:claude-opus-5-5":   {Input: usd(4), Output: usd(20)},
	"anthropic:claude-opus-5":     {Input: usd(5), Output: usd(25)},
	"anthropic:claude-opus-4-8":   {Input: usd(5), Output: usd(25)},
	"anthropic:claude-opus-4-7":   {Input: usd(5), Output: usd(25)},
	"anthropic:claude-opus-4-6":   {Input: usd(5), Output: usd(25)},
	"anthropic:claude-sonnet-5-5": {Input: usd(2), Output: usd(10)},
	"anthropic:claude-sonnet-5":   {Input: usd(2), Output: usd(10)},
	"anthropic:claude-sonnet-4-6": {Input: usd(3), Output: usd(15)},
	// Haiku 5.5: $0.10 / $0.50 for prompts up to 100K tokens, $0.50 / $2.50 beyond.
	"anthropic:claude-haiku-5-5": {Input: usd(0.10), Output: usd(0.50), LongAbove: 100_000, LongInput: usd(0.50), LongOutput: usd(2.50)},
	"anthropic:claude-haiku-4-5": {Input: usd(1), Output: usd(5)},
}
```

Create `arv/llm/host.go`:

```go
package llm

import (
	"context"
	"fmt"
)

// Host is what the embedding program gives a run for model calls: its
// providers, by name ("anthropic"), and prices that override
// DefaultPrices, by "provider:model". The providers hold the credentials;
// nothing in a Host is ever an Arv value.
type Host struct {
	Providers map[string]Provider
	Prices    map[string]Price
}

// Lookup returns the provider and the provider's model ID for spec
// ("provider:model", see ParseModel).
func (h *Host) Lookup(spec string) (Provider, string, error) {
	provider, model, err := ParseModel(spec)
	if err != nil {
		return nil, "", err
	}
	p := h.Providers[provider]
	if p == nil {
		return nil, "", fmt.Errorf("no provider %q is configured", provider)
	}
	return p, model, nil
}

// Price is spec's price: h.Prices first, then DefaultPrices.
func (h *Host) Price(spec string) (Price, bool) {
	if p, ok := h.Prices[spec]; ok {
		return p, true
	}
	p, ok := DefaultPrices[spec]
	return p, ok
}

type hostKey struct{}

// WithHost returns ctx carrying h. arv.Run puts Options.LLM on the root
// task's context, and every task's context derives from it, so the
// evaluator finds the host from any task (HostFrom).
func WithHost(ctx context.Context, h *Host) context.Context {
	return context.WithValue(ctx, hostKey{}, h)
}

// HostFrom is the host ctx carries, or nil.
func HostFrom(ctx context.Context) *Host {
	h, _ := ctx.Value(hostKey{}).(*Host)
	return h
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run '^(TestCost|TestEveryDefaultPriceIsSet|TestHost)$' ./arv/llm/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add arv/llm/pricing_test.go arv/llm/pricing.go arv/llm/host.go
git commit -m "feat(llm): prices and the host configuration

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
