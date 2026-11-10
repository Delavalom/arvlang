package runtime

import "testing"

// I6: a charge that passes several limits at once names the one that would
// have tripped first, the level with the least left, never a nested limit
// larger than what its enclosing levels allow. Found by the e2e I6 cases:
// scope (steps: 100) { scope (steps: 150) { ... } } reported "limit 150".
func TestExceededNamesTheLimitThatTrippedFirst(t *testing.T) {
	type charge func(b *Budget, n int64) error
	type spent func(b *Budget) error
	kinds := []struct {
		kind   string
		lim    func(n int64) Limits
		charge charge
		spent  spent
	}{
		{"steps", func(n int64) Limits { return Limits{Steps: n} }, (*Budget).Steps, (*Budget).StepsSpent},
		{"alloc", func(n int64) Limits { return Limits{AllocBytes: n} }, (*Budget).Alloc, (*Budget).AllocSpent},
	}
	for _, k := range kinds {
		t.Run(k.kind, func(t *testing.T) {
			// The enclosing limit is smaller: it is the one exceeded.
			root := Root(k.lim(100))
			inner := root.Child(k.lim(150))
			wantExceeded(t, k.charge(inner, 256), k.kind, 100)
			wantExceeded(t, k.spent(inner), k.kind, 100)

			// The same across a task boundary.
			root = Root(k.lim(100))
			inner = root.Task().Child(k.lim(150))
			wantExceeded(t, k.charge(inner, 256), k.kind, 100)

			// The enclosing level has less left, though its limit is larger.
			root = Root(k.lim(1000))
			if err := k.charge(root, 950); err != nil {
				t.Fatal(err)
			}
			inner = root.Child(k.lim(150))
			wantExceeded(t, k.charge(inner, 256), k.kind, 1000)
			wantExceeded(t, k.spent(inner), k.kind, 1000)

			// The nested level has less left: it is the one exceeded.
			root = Root(k.lim(1000))
			inner = root.Child(k.lim(150))
			wantExceeded(t, k.charge(inner, 256), k.kind, 150)
			wantExceeded(t, k.spent(inner), k.kind, 150)
		})
	}
}
