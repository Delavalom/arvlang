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
