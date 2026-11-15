package runtime

import (
	"context"
	"testing"
	"time"
)

// A rerun is a fresh task in the same place: same authority, parent and
// horizon, its own ID and child numbering.
func TestAttempt(t *testing.T) {
	root := NewRoot(context.Background(), NewCapSet(), nil)
	before := Now()
	caps := NewCapSet(Capability{Kind: Env, Scope: "HOME"})
	child := root.Child(context.Background(), caps)
	if child.Attempt(1) != child {
		t.Fatal("the first attempt is not the task itself")
	}
	child.Child(context.Background(), NewCapSet()) // attempt 1 spawned root/0/0
	a := child.Attempt(2)
	if a.ID != "root/0#2" || !a.Caps.Allows(Capability{Kind: Env, Scope: "HOME"}) || !root.IsAncestorOf(a) {
		t.Fatalf("attempt 2 = %s holding %v", a.ID, a.Caps.List())
	}
	if a.Horizon(before) != child.Horizon(before) {
		t.Fatal("attempt 2 sees a later horizon than the task it reruns")
	}
	if g := a.Child(context.Background(), NewCapSet()); g.ID != "root/0#2/0" {
		t.Fatalf("attempt 2's first child is %s", g.ID)
	}
}

// A rerun stays inside its scope (I5, I6): it has t's context, so the
// scope's deadline is its deadline and cancelling the scope stops it. An
// attempt on a context of its own would outlive a timeout or a failed
// sibling.
func TestAttemptSharesTheScopeContext(t *testing.T) {
	root := NewRoot(context.Background(), NewCapSet(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	child := root.Child(ctx, NewCapSet())
	want, _ := ctx.Deadline()
	for _, n := range []int{2, 3, 10} {
		a := child.Attempt(n)
		if d, ok := a.Ctx.Deadline(); !ok || !d.Equal(want) {
			t.Fatalf("attempt %d has deadline %v (set %v), want the scope's %v", n, d, ok, want)
		}
		if a.Err() != nil {
			t.Fatalf("attempt %d stopped before its scope: %v", n, a.Err())
		}
	}
	cancel()
	for _, n := range []int{2, 3, 10} {
		a := child.Attempt(n)
		select {
		case <-a.Ctx.Done():
		default:
			t.Fatalf("attempt %d is not cancelled with its scope", n)
		}
		if a.Err() == nil {
			t.Fatalf("attempt %d may go on after its scope was cancelled", n)
		}
	}
}
