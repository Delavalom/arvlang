package evaluator

import (
	"math"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

func TestModelScopeOptions(t *testing.T) {
	got, _ := runWith(t, `scope (tokens: 50000, usd_cents: 25, max_turns: 3) { 1 + 1 }`)
	if got.Inspect() != "2" {
		t.Fatalf("got %s, want 2", got.Inspect())
	}
	for _, tt := range []struct{ input, msg string }{
		{`scope (tokens: 0) { 1 }`, "scope option tokens: want a positive INTEGER, got 0"},
		{`scope (usd_cents: "1") { 1 }`, "scope option usd_cents: want a positive INTEGER, got STRING"},
		{`scope (usd_cents: 922337203685478) { 1 }`, "scope option usd_cents: 922337203685478 is too large (max 922337203685477)"},
		{`scope (max_turns: 10001) { 1 }`, "scope option max_turns: 10001 is too large (max 10000)"},
	} {
		got, _ := runWith(t, tt.input)
		wantErrorAt(t, got, tt.msg, "1:8")
	}
	if runtime.MaxUSDCents != math.MaxInt64/10_000 {
		t.Fatal("MaxUSDCents changed: update the error above")
	}
}
