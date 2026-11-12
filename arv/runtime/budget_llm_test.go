package runtime

import (
	"errors"
	"math/rand"
	"sync"
	"testing"
)

func wantModelExceeded(t *testing.T, err error, kind string, limit int64) {
	t.Helper()
	var e *ExceededError
	if !errors.As(err, &e) || e.Kind != kind || e.Limit != limit {
		t.Fatalf("err = %v, want budget exceeded: %s (limit %d)", err, kind, limit)
	}
}

func TestReserveAndSettleTokens(t *testing.T) {
	root := Root(Limits{Tokens: 100})
	r, err := root.Reserve(80, 0)
	if err != nil {
		t.Fatal(err)
	}
	// 20 left while 80 are reserved: a second call does not fit.
	_, err = root.Reserve(30, 0)
	wantModelExceeded(t, err, "tokens", 100)
	if err := r.Settle(50, 0); err != nil { // 30 come back
		t.Fatal(err)
	}
	if err := r.Settle(0, 0); err != nil { // only the first Settle counts
		t.Fatal(err)
	}
	r2, err := root.Reserve(50, 0)
	if err != nil {
		t.Fatalf("50 of the 50 left: %v", err)
	}
	_, err = root.Reserve(1, 0)
	wantModelExceeded(t, err, "tokens", 100)
	if err := r2.Settle(0, 0); err != nil { // a call that never ran
		t.Fatal(err)
	}
	if _, err := root.Reserve(50, 0); err != nil {
		t.Fatalf("after a refund: %v", err)
	}
}

func TestUSDLimitIsInCents(t *testing.T) {
	root := Root(Limits{USDCents: 1}) // 10_000 µ$
	if !root.LimitsUSD() || Root(Limits{}).LimitsUSD() {
		t.Fatal("LimitsUSD is wrong")
	}
	r, err := root.Reserve(0, 8_000)
	if err != nil {
		t.Fatal(err)
	}
	_, err = root.Reserve(0, 8_000)
	wantModelExceeded(t, err, "usd_cents", 1)
	if err := r.Settle(0, 6_000); err != nil {
		t.Fatal(err)
	}
	_, err = root.Reserve(0, 4_001)
	wantModelExceeded(t, err, "usd_cents", 1)
	if _, err := root.Reserve(0, 4_000); err != nil {
		t.Fatal(err)
	}
}

// Use beyond the reservation is charged, and a level it takes past its
// limit stays spent: every later call is refused, even an empty one.
func TestSettleOverageSpendsTheLevel(t *testing.T) {
	root := Root(Limits{Tokens: 100})
	r, _ := root.Reserve(90, 0)
	wantModelExceeded(t, r.Settle(120, 0), "tokens", 100)
	_, err := root.Reserve(0, 0)
	wantModelExceeded(t, err, "tokens", 100)
}

// A refused reservation sets nothing aside, at any level.
func TestRefusedReservationTakesNothing(t *testing.T) {
	root := Root(Limits{Tokens: 1000, USDCents: 1})
	inner := root.Child(Limits{Tokens: 100})
	_, err := inner.Reserve(50, 20_000) // tokens fit, dollars do not
	wantModelExceeded(t, err, "usd_cents", 1)
	if _, err := inner.Reserve(100, 10_000); err != nil {
		t.Fatalf("everything is still there: %v", err)
	}
}

// I6: a nested level never allows more than the levels around it have
// left, across task boundaries, and max_turns only shrinks.
func TestModelBudgetsNest(t *testing.T) {
	root := Root(Limits{Tokens: 100, MaxTurns: 5})
	if root.MaxTurns() != 5 || Root(Limits{}).MaxTurns() != DefaultMaxTurns {
		t.Fatal("root max_turns is wrong")
	}
	r, _ := root.Reserve(70, 0)
	defer r.Settle(70, 0)
	child := root.Child(Limits{}).Task().Child(Limits{Tokens: 1000, MaxTurns: 50})
	if child.MaxTurns() != 5 {
		t.Fatalf("max_turns = %d, want 5 (a nested 50 cannot widen it)", child.MaxTurns())
	}
	if root.Child(Limits{MaxTurns: 2}).Task().MaxTurns() != 2 {
		t.Fatal("a nested max_turns does not shrink")
	}
	_, err := child.Reserve(31, 0)
	wantModelExceeded(t, err, "tokens", 100)
	if _, err := child.Reserve(30, 0); err != nil {
		t.Fatal(err)
	}
}

func TestValidateModelLimits(t *testing.T) {
	for _, l := range []Limits{{Tokens: -1}, {USDCents: -1}, {MaxTurns: -1}, {USDCents: MaxUSDCents + 1}} {
		if l.Validate() == nil {
			t.Errorf("Validate(%+v) = nil", l)
		}
	}
	if err := (Limits{Tokens: 1, USDCents: MaxUSDCents, MaxTurns: 1}).Validate(); err != nil {
		t.Fatal(err)
	}
}

// I6 under concurrency: tasks reserving and settling at once never spend
// more than a level's limit, and what is left at the end is exactly the
// limit minus what was used.
func TestConcurrentReservationsProperty(t *testing.T) {
	for seed := int64(0); seed < 20; seed++ {
		rng := rand.New(rand.NewSource(seed))
		limit := 500 + rng.Int63n(1000)
		root := Root(Limits{Tokens: limit})
		levels := []*Budget{root.Child(Limits{}).Task(), root.Child(Limits{Tokens: limit / 2}), root}
		var mu sync.Mutex
		var used int64
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			b := levels[g%len(levels)]
			n, u := 1+rng.Int63n(100), rng.Int63n(100)
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 50; i++ {
					r, err := b.Reserve(n, 0)
					if err != nil {
						continue
					}
					got := min(u, n)
					if r.Settle(got, 0) == nil {
						mu.Lock()
						used += got
						mu.Unlock()
					}
				}
			}()
		}
		wg.Wait()
		if left := root.tokens.left.Load(); used > limit || left != limit-used {
			t.Fatalf("seed %d: used %d of %d, %d left", seed, used, limit, left)
		}
	}
}
