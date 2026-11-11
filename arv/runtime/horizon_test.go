package runtime

import (
	"context"
	"io"
	"math"
	"testing"
)

// A task's horizon for an environment is the spawn time of the first task
// on its path from the root spawned after the environment was created.
func TestHorizon(t *testing.T) {
	root := NewRoot(context.Background(), NewCapSet(), io.Discard)
	before := Now()
	if h := root.Horizon(before); h != math.MaxUint64 || root.HorizonLen() != 0 {
		t.Fatalf("root horizon = %d (len %d), want none", h, root.HorizonLen())
	}
	child := root.Child(context.Background(), NewCapSet())
	between := Now()
	grand := child.Child(context.Background(), NewCapSet())
	after := Now()
	if child.HorizonLen() != 1 || grand.HorizonLen() != 2 {
		t.Fatalf("horizon lengths %d, %d, want 1, 2", child.HorizonLen(), grand.HorizonLen())
	}
	// The child was spawned at between: what existed before is frozen then.
	if h := child.Horizon(before); h != between {
		t.Errorf("child.Horizon(before) = %d, want %d", h, between)
	}
	// The grandchild sees the root's old environment as the child did...
	if h := grand.Horizon(before); h != between {
		t.Errorf("grand.Horizon(before) = %d, want %d", h, between)
	}
	// ...the child's own environments as they were at its spawn...
	if h := grand.Horizon(between); h != after {
		t.Errorf("grand.Horizon(between) = %d, want %d", h, after)
	}
	// ...and its own without limit.
	if h := grand.Horizon(after); h != math.MaxUint64 {
		t.Errorf("grand.Horizon(after) = %d, want none", h)
	}
	if Now() != after {
		t.Errorf("the clock moved without a spawn")
	}
}
