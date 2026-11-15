package runtime

import (
	"bytes"
	"context"
	"reflect"
	"testing"
)

// LineageCaps lists the task's own grants first, then each ancestor's up
// to the root, so a spawned task's fs opens can be confined by the
// outermost grant its lineage holds (review round 3).
func TestLineageCaps(t *testing.T) {
	g, _ := New(FSRead, "/")
	e, _ := New(Env, "HOME")
	n, _ := New(Net, "example.com")
	root := NewRoot(context.Background(), NewCapSet(g, e), &bytes.Buffer{})
	mid := root.Child(root.Ctx, NewCapSet(e))
	leaf := mid.Child(mid.Ctx, NewCapSet(n))
	if got, want := leaf.LineageCaps(), []Capability{n, e, g, e}; !reflect.DeepEqual(got, want) {
		t.Fatalf("leaf.LineageCaps() = %v, want %v", got, want)
	}
	if got, want := root.LineageCaps(), []Capability{g, e}; !reflect.DeepEqual(got, want) {
		t.Fatalf("root.LineageCaps() = %v, want %v", got, want)
	}
}
