package runtime

import (
	"math/rand"
	"testing"
)

// I6 for the model budgets, as TestEffectiveLimitIsTheSmallestProperty
// checks steps and bytes: on random chains of nested limits, reserving and
// settling one unit at a time at the innermost level succeeds exactly as
// often as the smallest limit allows, and the next reservation fails
// naming that limit. max_turns at the leaf is the smallest in the chain
// (DefaultMaxTurns standing in for a root that sets none).
func TestModelLimitsOnlyShrinkProperty(t *testing.T) {
	spend := func(tokens, usd int64) func(*Budget) error {
		return func(b *Budget) error {
			r, err := b.Reserve(tokens, usd)
			if err != nil {
				return err
			}
			return r.Settle(tokens, usd)
		}
	}
	kinds := []struct {
		kind  string
		lim   func(int64) Limits
		spend func(*Budget) error
	}{
		{"tokens", func(n int64) Limits { return Limits{Tokens: n} }, spend(1, 0)},
		{"usd_cents", func(n int64) Limits { return Limits{USDCents: n} }, spend(0, microsPerCent)},
	}
	rng := rand.New(rand.NewSource(7))
	for _, k := range kinds {
		for round := 0; round < 300; round++ {
			chain := randomChain(rng, k.lim)
			min := int64(-1)
			for _, l := range chain {
				if l.limit > 0 && (min < 0 || l.limit < min) {
					min = l.limit
				}
			}
			if min < 0 {
				continue
			}
			leaf := chain[len(chain)-1].b
			for i := int64(0); i < min; i++ {
				if err := k.spend(leaf); err != nil {
					t.Fatalf("%s round %d: unit %d of %d failed: %v", k.kind, round, i+1, min, err)
				}
			}
			wantExceeded(t, k.spend(leaf), k.kind, min)
		}
	}
	for round := 0; round < 300; round++ {
		chain := randomChain(rng, func(n int64) Limits { return Limits{MaxTurns: int(n)} })
		want := int64(DefaultMaxTurns)
		if chain[0].limit > 0 {
			want = chain[0].limit
		}
		for _, l := range chain[1:] {
			if l.limit > 0 && l.limit < want {
				want = l.limit
			}
		}
		if got := chain[len(chain)-1].b.MaxTurns(); int64(got) != want {
			t.Fatalf("max_turns round %d: leaf allows %d, want %d", round, got, want)
		}
	}
}
