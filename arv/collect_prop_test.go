package arv

import (
	"math/rand"
	goruntime "runtime"
	"testing"
)

// TestCollectAccountsForEveryChild is the Phase 4 property "supervision
// never drops an error silently": for random trees of tasks that succeed or
// fail (with every kind) under collect, with nested collect scopes that
// await all or only some of their children and fail_fast scopes, the
// collected result holds every awaited child's outcome exactly once, in
// spawn order, with its kind, message, task and position; a collect scope
// with unawaited failures fails with their count and the first one's kind,
// task and position. No goroutine outlives a run.
func TestCollectAccountsForEveryChild(t *testing.T) {
	dir := realDir(t.TempDir())
	n := 200
	if testing.Short() {
		n = 40
	}
	rng := rand.New(rand.NewSource(4))
	for i := 0; i < n; i++ {
		b := make([]byte, 8+rng.Intn(120))
		rng.Read(b)
		before := goruntime.NumGoroutine()
		src, err := runTree(b, dir)
		if err != nil {
			t.Fatalf("tree %d (bytes %x): %v\nprogram:\n%s", i, b, err, src)
		}
		noLeak(t, before, src)
	}
}

// TestCollectTreeShapes pins a few trees the property must cover whatever
// the random source gives: every leaf kind under one collect, an unawaited
// failure two levels down, and a fail_fast scope with its one failure.
func TestCollectTreeShapes(t *testing.T) {
	dir := realDir(t.TempDir())
	for name, b := range map[string][]byte{
		// root: 4 kids (3 -> 1+3), mask, kinds ok, user, cap, effect
		"leaves":         {3, 0, leafOK, leafUser, leafCap, leafEffect},
		"runtime-budget": {1, 0, leafRuntime, leafBudget},
		// root: 1 kid, collectSome with 2 kids, mask 0b01: awaits the ok leaf only, user leaf unawaited
		"unawaited": {0, 0, collectSome, 1, 1, leafOK, leafUser},
		// root: 1 kid, failFast with 3 leaves, the second fails with cap
		"fail_fast": {0, 0, failFast, 2, 1, leafCap - leafUser},
		// collectSome nested in collectAll nested in the root
		"nested": {0, 0, collectAll, 1, 0, collectSome, 1, 0, leafOK, leafEffect},
	} {
		t.Run(name, func(t *testing.T) {
			before := goruntime.NumGoroutine()
			src, err := runTree(b, dir)
			if err != nil {
				t.Fatalf("%v\nprogram:\n%s", err, src)
			}
			noLeak(t, before, src)
		})
	}
}

// FuzzCollectTree is the same property with the tree decoded from fuzzed
// bytes, so coverage guidance can steer the shapes.
func FuzzCollectTree(f *testing.F) {
	f.Add([]byte{3, 0, leafOK, leafUser, leafCap, leafEffect})
	f.Add([]byte{0, 0, collectSome, 1, 1, leafOK, leafUser})
	f.Add([]byte{0, 0, failFast, 2, 1, 1})
	f.Add([]byte{3, 0xff, collectAll, 3, 0, 7, 7, 7, 7, 8, 2, 2, 4})
	dir := realDir(f.TempDir())
	f.Fuzz(func(t *testing.T, b []byte) {
		before := goruntime.NumGoroutine()
		src, err := runTree(b, dir)
		if err != nil {
			t.Fatalf("%v\nprogram:\n%s", err, src)
		}
		noLeak(t, before, src)
	})
}
