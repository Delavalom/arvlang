package runtime

import (
	"context"
	"testing"
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
