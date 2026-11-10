package runtime

import (
	"errors"
	"math/rand"
	"sync"
	"testing"
)

func TestExceededErrorText(t *testing.T) {
	for _, tt := range []struct {
		e    ExceededError
		want string
	}{
		{ExceededError{"steps", 1000}, "budget exceeded: steps (limit 1000)"},
		{ExceededError{"timeout", 100}, "budget exceeded: timeout (limit 100 ms)"},
		{ExceededError{"alloc", 1 << 20}, "budget exceeded: alloc (limit 1048576 bytes)"},
		{ExceededError{"tasks", 3}, "budget exceeded: tasks (limit 3 live tasks)"},
	} {
		if got := tt.e.Error(); got != tt.want {
			t.Errorf("got %q, want %q", got, tt.want)
		}
	}
}

// wantExceeded fails unless err is an *ExceededError of kind and limit.
func wantExceeded(t *testing.T, err error, kind string, limit int64) {
	t.Helper()
	var e *ExceededError
	if !errors.As(err, &e) || e.Kind != kind || e.Limit != limit {
		t.Fatalf("err = %v, want %s exceeded at limit %d", err, kind, limit)
	}
}

// I6: a charge counts at every level, so the smallest remaining limit wins,
// and what one level spends is gone for its siblings.
func TestStepsAcrossThreeLevels(t *testing.T) {
	root := Root(Limits{Steps: 1000})
	mid := root.Child(Limits{}) // no limit of its own
	inner := mid.Task().Child(Limits{Steps: 300})

	if err := inner.Steps(300); err != nil {
		t.Fatalf("exactly the limit: %v", err)
	}
	wantExceeded(t, inner.Steps(1), "steps", 300)

	sibling := mid.Child(Limits{Steps: 5000}) // more than the root has left
	if err := sibling.Steps(600); err != nil {
		t.Fatal(err)
	}
	wantExceeded(t, sibling.Steps(200), "steps", 1000) // root: 300+1+600+200 > 1000
}

func TestExceededStaysExceeded(t *testing.T) {
	root := Root(Limits{Steps: 10, AllocBytes: 100})
	wantExceeded(t, root.Steps(11), "steps", 10)
	wantExceeded(t, root.Steps(1), "steps", 10) // nothing is refunded
	if err := root.Steps(0); err != nil {
		t.Fatalf("Steps(0) = %v; charging nothing never fails", err)
	}
	wantExceeded(t, root.Alloc(101), "alloc", 100)
	wantExceeded(t, root.Task().Alloc(1), "alloc", 100)
	if err := root.Alloc(0); err != nil {
		t.Fatalf("Alloc(0) = %v", err)
	}
}

func TestAllocChargesEveryLevel(t *testing.T) {
	root := Root(Limits{AllocBytes: 100})
	inner := root.Task().Child(Limits{AllocBytes: 1000}).Task()
	if err := inner.Alloc(60); err != nil {
		t.Fatal(err)
	}
	wantExceeded(t, inner.Alloc(60), "alloc", 100) // 120 > the root's 100
}

func TestNoLimitsChargeNothing(t *testing.T) {
	b := Root(Limits{}).Child(Limits{}).Task()
	if b.Steps(1<<40) != nil || b.Alloc(1<<50) != nil || Root(Limits{}).live.limit != DefaultLiveTasks {
		t.Fatal("a budget with no limits refused a charge, or the default live-task cap is wrong")
	}
}

// Concurrent chargers never get past the limit unnoticed: once the total
// passes it, every charge fails.
func TestStepsConcurrent(t *testing.T) {
	root := Root(Limits{Steps: 100_000})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b := root.Task().Child(Limits{Steps: 50_000})
			for b.Steps(256) == nil {
			}
		}()
	}
	wg.Wait()
	if left := root.steps.left.Load(); left >= 0 {
		t.Fatalf("root has %d steps left after every charger failed", left)
	}
}

// I6 as a property: over random trees of nested limits and random charges,
// for steps and for bytes, a charge reaches every level above it; it
// succeeds only if no level has passed its limit, and a failure names the
// level with the least left, the one that would have tripped first.
func TestBudgetsOnlyShrinkProperty(t *testing.T) {
	kinds := []struct {
		kind   string
		lim    func(n int64) Limits
		charge func(b *Budget, n int64) error
	}{
		{"steps", func(n int64) Limits { return Limits{Steps: n} }, (*Budget).Steps},
		{"alloc", func(n int64) Limits { return Limits{AllocBytes: n} }, (*Budget).Alloc},
	}
	rng := rand.New(rand.NewSource(1))
	for _, k := range kinds {
		for round := 0; round < 300; round++ {
			type node struct {
				b      *Budget
				parent int // -1 for the root
				limit  int64
				used   int64 // charged to this level, by it or its descendants
			}
			rootLimit := rng.Int63n(5000) // 0: no limit
			nodes := []*node{{b: Root(k.lim(rootLimit)), parent: -1, limit: rootLimit}}
			for i := 0; i < 1+rng.Intn(8); i++ {
				p := rng.Intn(len(nodes))
				var limit int64
				if rng.Intn(2) == 0 {
					limit = 1 + rng.Int63n(3000)
				}
				b := nodes[p].b.Child(k.lim(limit))
				if rng.Intn(2) == 0 {
					b = nodes[p].b.Task().Child(k.lim(limit))
				}
				nodes = append(nodes, &node{b: b, parent: p, limit: limit})
			}
			for op := 0; op < 300; op++ {
				i := rng.Intn(len(nodes))
				n := 1 + rng.Int63n(300)
				err := k.charge(nodes[i].b, n)
				var want *node // the level with the least left, if any is past its limit
				for j := i; j != -1; j = nodes[j].parent {
					nd := nodes[j]
					if nd.limit == 0 {
						continue
					}
					nd.used += n
					if nd.used > nd.limit && (want == nil || nd.limit-nd.used < want.limit-want.used) {
						want = nd
					}
				}
				var e *ExceededError
				switch {
				case want == nil && err != nil:
					t.Fatalf("%s round %d: no level is past its limit, but the charge failed: %v", k.kind, round, err)
				case want != nil && (!errors.As(err, &e) || e.Kind != k.kind):
					t.Fatalf("%s round %d: a level is at %d/%d, but the charge returned %v",
						k.kind, round, want.used, want.limit, err)
				case want != nil && e.Limit != want.limit:
					// Ties: another level with the same limit and as little left is as good.
					ok := false
					for j := i; j != -1; j = nodes[j].parent {
						nd := nodes[j]
						ok = ok || (nd.limit == e.Limit && nd.limit-nd.used == want.limit-want.used)
					}
					if !ok {
						t.Fatalf("%s round %d: failed at limit %d, want %d (the level with the least left)",
							k.kind, round, e.Limit, want.limit)
					}
				}
			}
		}
	}
}

// StepsSpent reports a level that a charge has already taken past its
// limit, from b up, without charging anything; a level at exactly its
// limit is not spent.
func TestStepsSpent(t *testing.T) {
	root := Root(Limits{Steps: 10})
	inner := root.Child(Limits{Steps: 100}).Task().Child(Limits{})
	if err := inner.StepsSpent(); err != nil {
		t.Fatalf("fresh budget: StepsSpent = %v", err)
	}
	if err := inner.Steps(10); err != nil {
		t.Fatalf("Steps(10) = %v", err)
	}
	if err := inner.StepsSpent(); err != nil {
		t.Fatalf("at exactly the limit: StepsSpent = %v", err)
	}
	wantExceeded(t, root.Steps(1), "steps", 10)
	wantExceeded(t, inner.StepsSpent(), "steps", 10)
	if err := Root(Limits{}).StepsSpent(); err != nil {
		t.Fatalf("no limit: StepsSpent = %v", err)
	}
}

// AllocSpent is StepsSpent for bytes.
func TestAllocSpent(t *testing.T) {
	root := Root(Limits{AllocBytes: 10})
	inner := root.Child(Limits{AllocBytes: 100}).Task().Child(Limits{})
	if err := inner.AllocSpent(); err != nil {
		t.Fatalf("fresh budget: AllocSpent = %v", err)
	}
	if err := inner.Alloc(10); err != nil {
		t.Fatalf("Alloc(10) = %v", err)
	}
	if err := inner.AllocSpent(); err != nil {
		t.Fatalf("at exactly the limit: AllocSpent = %v", err)
	}
	wantExceeded(t, root.Alloc(1), "alloc", 10)
	wantExceeded(t, inner.AllocSpent(), "alloc", 10)
	if err := Root(Limits{}).AllocSpent(); err != nil {
		t.Fatalf("no limit: AllocSpent = %v", err)
	}
}
