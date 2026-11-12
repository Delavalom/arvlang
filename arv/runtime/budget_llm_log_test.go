package runtime

import (
	"errors"
	"testing"
)

// A replayed reservation is taken even past the limit, and a level ends
// where the recorded run's did once the settlements are made too.
func TestReserveLogged(t *testing.T) {
	root := Root(Limits{})
	if root.LimitsModels() {
		t.Fatal("a budget with no model limit limits models")
	}
	b := root.Child(Limits{Tokens: 100, USDCents: 1})
	if !b.LimitsModels() || !b.Child(Limits{}).LimitsModels() {
		t.Fatal("a level under a tokens limit does not limit models")
	}
	r1 := b.ReserveLogged(80, 5000)
	r2 := b.ReserveLogged(80, 5000) // past both limits: a replay's order
	if _, err := b.Reserve(1, 0); !isExceeded(err, "tokens") {
		t.Fatalf("Reserve while past the limit: %v", err)
	}
	if !r1.Covers(80, 5000) || r1.Covers(81, 0) || r1.Covers(0, 5001) || !r1.Covers(-1, -1) {
		t.Fatal("Covers")
	}
	r1.Settle(30, 2000)
	r2.Settle(30, 2000)
	// 100 - 60 tokens and 10000 - 4000 micro-dollars are left.
	if _, err := b.Reserve(40, 6000); err != nil {
		t.Fatalf("what is left after settling: %v", err)
	}
	if _, err := b.Reserve(1, 0); !isExceeded(err, "tokens") {
		t.Fatalf("one token past the rest: %v", err)
	}
}

func isExceeded(err error, kind string) bool {
	var ex *ExceededError
	return errors.As(err, &ex) && ex.Kind == kind
}

// Overdrawn names a level below zero, in its own unit, and nothing at or
// above zero.
func TestOverdrawn(t *testing.T) {
	root := Root(Limits{USDCents: 1})
	b := root.Child(Limits{Tokens: 100})
	if err := b.Overdrawn(); err != nil {
		t.Fatalf("a fresh budget: %v", err)
	}
	r := b.ReserveLogged(100, 10_000) // exactly the limits
	if err := b.Overdrawn(); err != nil {
		t.Fatalf("at zero: %v", err)
	}
	b.ReserveLogged(0, 1)
	var ex *ExceededError
	if err := b.Overdrawn(); !errors.As(err, &ex) || ex.Kind != "usd_cents" || ex.Limit != 1 {
		t.Fatalf("usd_cents below zero: %v", err)
	}
	r.Settle(101, 0)
	if err := b.Overdrawn(); !isExceeded(err, "tokens") {
		t.Fatalf("tokens below zero: %v", err)
	}
}
