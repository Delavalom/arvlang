package runtime

import (
	"context"
	"testing"
)

// A child's horizon is its parent's plus one entry, allocated exactly: the
// evaluator charges 8 bytes per entry at spawn, and append onto a full
// slice would allocate about twice that.
func TestChildHorizonIsExact(t *testing.T) {
	tk := NewRoot(context.Background(), NewCapSet(), nil)
	for d := 1; d <= 300; d++ {
		tk = tk.Child(context.Background(), NewCapSet())
		if len(tk.horizon) != d || cap(tk.horizon) != d {
			t.Fatalf("depth %d: horizon len %d cap %d, want both %d", d, len(tk.horizon), cap(tk.horizon), d)
		}
	}
}
