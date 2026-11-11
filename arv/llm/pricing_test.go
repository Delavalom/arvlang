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
