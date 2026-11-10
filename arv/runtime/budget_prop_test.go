package runtime

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"
)

// I6, one property per kind of limit: for random chains of nested limits,
// what a level allows is never more than what the levels around it allow.
// A chain is the root, then levels that are each a nested scope in the
// same task or the first scope in a new task.

type chainLevel struct {
	b     *Budget
	limit int64 // 0: none at this level
	task  bool  // the level was opened in a new task
}

func randomChain(rng *rand.Rand, lim func(int64) Limits) []chainLevel {
	limit := func() int64 {
		if rng.Intn(3) == 0 {
			return 0
		}
		return 1 + rng.Int63n(400)
	}
	l := limit()
	chain := []chainLevel{{b: Root(lim(l)), limit: l, task: true}}
	for i := rng.Intn(6); i >= 0; i-- {
		l := limit()
		parent := chain[len(chain)-1].b
		if rng.Intn(2) == 0 {
			chain = append(chain, chainLevel{b: parent.Child(lim(l)), limit: l})
		} else {
			chain = append(chain, chainLevel{b: parent.Task().Child(lim(l)), limit: l, task: true})
		}
	}
	return chain
}

// Steps and bytes: charged one at a time at the innermost level of a
// fresh chain, exactly the smallest limit in the chain succeeds, and the
// next charge fails naming it. A nested limit larger than an enclosing
// one adds nothing.
func TestEffectiveLimitIsTheSmallestProperty(t *testing.T) {
	kinds := []struct {
		kind   string
		lim    func(int64) Limits
		charge func(*Budget, int64) error
	}{
		{"steps", func(n int64) Limits { return Limits{Steps: n} }, (*Budget).Steps},
		{"alloc", func(n int64) Limits { return Limits{AllocBytes: n} }, (*Budget).Alloc},
	}
	rng := rand.New(rand.NewSource(3))
	for _, k := range kinds {
		for round := 0; round < 500; round++ {
			chain := randomChain(rng, k.lim)
			min := int64(-1)
			for _, l := range chain {
				if l.limit > 0 && (min < 0 || l.limit < min) {
					min = l.limit
				}
			}
			if min < 0 {
				continue // unlimited: nothing to check
			}
			leaf := chain[len(chain)-1].b
			for i := int64(0); i < min; i++ {
				if err := k.charge(leaf, 1); err != nil {
					t.Fatalf("%s round %d: charge %d of %d failed: %v", k.kind, round, i+1, min, err)
				}
			}
			wantExceeded(t, k.charge(leaf, 1), k.kind, min)
		}
	}
}

// Task slots: a scope's tasks limit counts its direct children, so the
// spawns a level can have running at once are the smallest tasks limit
// from that level up to the first level of its task (I6), never more,
// and limits in enclosing tasks do not apply.
func TestSlotsOnlyShrinkProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	lim := func(n int64) Limits { return Limits{Tasks: int(n), LiveTasks: 100000} }
	for round := 0; round < 300; round++ {
		chain := randomChain(rng, func(n int64) Limits { return lim(n % 6) })
		want := -1
		for i := len(chain) - 1; i >= 0; i-- {
			if l := int(chain[i].limit % 6); l > 0 && (want < 0 || l < want) {
				want = l
			}
			if chain[i].task {
				break
			}
		}
		if want < 0 {
			continue
		}
		leaf := chain[len(chain)-1].b
		for i := 0; i < want; i++ {
			if err := acquireWithin(leaf, 10*time.Second); err != nil {
				t.Fatalf("round %d: acquire %d of %d: %v", round, i+1, want, err)
			}
		}
		if err := acquireWithin(leaf, 20*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("round %d: acquire %d with %d slots = %v, want it to block", round, want+1, want, err)
		}
		leaf.Release()
		if err := acquireWithin(leaf, 10*time.Second); err != nil {
			t.Fatalf("round %d: acquire after a release: %v", round, err)
		}
		for i := 0; i < want; i++ {
			leaf.Release()
		}
		if n := leaf.live.n.Load(); n != 0 {
			t.Fatalf("round %d: %d live tasks counted after every release", round, n)
		}
	}
}

// acquireWithin is Acquire with a context that ends after d.
func acquireWithin(b *Budget, d time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return b.Acquire(ctx)
}

// Timeouts: a nested timed scope's deadline is the earlier of its own and
// its enclosing scope's, so it never ends later than an enclosing one.
func TestDeadlinesOnlyShrinkProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	cause := errors.New("timeout")
	for round := 0; round < 300; round++ {
		ctx := context.Background()
		var outer time.Time // the effective deadline so far; zero: none
		var scopes []*Scope
		for depth := rng.Intn(6); depth >= 0; depth-- {
			var s *Scope
			var lo, hi time.Time // bounds on the scope's own deadline; zero: none
			if rng.Intn(3) == 0 {
				s = NewScope(ctx)
			} else {
				d := time.Duration(1+rng.Intn(3600)) * time.Second
				lo = time.Now().Add(d)
				s = NewTimedScope(ctx, d, cause)
				hi = time.Now().Add(d)
			}
			scopes = append(scopes, s)
			got, ok := s.Context().Deadline()
			switch {
			case outer.IsZero() && lo.IsZero():
				if ok {
					t.Fatalf("round %d: deadline %v with no timeout anywhere", round, got)
				}
			case !ok:
				t.Fatalf("round %d: no deadline, want one", round)
			case !outer.IsZero() && got.After(outer):
				t.Fatalf("round %d: nested deadline %v is after the enclosing %v", round, got, outer)
			case !hi.IsZero() && got.After(hi):
				t.Fatalf("round %d: deadline %v is after its own, at most %v", round, got, hi)
			case !lo.IsZero() && got.Before(lo) && (outer.IsZero() || got.Before(outer)):
				t.Fatalf("round %d: deadline %v is before its own (%v) and the enclosing (%v)",
					round, got, lo, outer)
			}
			if ok {
				outer = got
			}
			ctx = s.Context()
		}
		for i := len(scopes) - 1; i >= 0; i-- {
			scopes[i].Wait()
		}
	}
}
