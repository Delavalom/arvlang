package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestChildIDsAndAuthority(t *testing.T) {
	net, _ := New(Net, "api.github.com")
	root := NewRoot(context.Background(), NewCapSet(net), &bytes.Buffer{})

	a := root.Child(root.Ctx, NewCapSet())
	b := root.Child(root.Ctx, NewCapSet(net))
	if a.ID != "root/0" || b.ID != "root/1" {
		t.Fatalf("ids = %s, %s", a.ID, b.ID)
	}
	if err := a.Require(net); err == nil {
		t.Error("child with empty caps passed Require")
	}
	if err := b.Require(net); err != nil {
		t.Errorf("child holding net failed Require: %v", err)
	}
}

func TestTickSeesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	task := NewRoot(ctx, NewCapSet(), &bytes.Buffer{})
	stop := errors.New("stop")
	cancel(stop)

	var err error
	for i := 0; i < checkEvery && err == nil; i++ {
		err = task.Tick()
	}
	if !errors.Is(err, stop) {
		t.Fatalf("Tick after cancel = %v, want %v", err, stop)
	}
}

func TestIsAncestorOf(t *testing.T) {
	root := NewRoot(context.Background(), NewCapSet(), io.Discard)
	a, b := root.Child(root.Ctx, NewCapSet()), root.Child(root.Ctx, NewCapSet())
	aa := a.Child(a.Ctx, NewCapSet())
	for _, tt := range []struct {
		anc, d *Task
		want   bool
	}{{root, a, true}, {root, aa, true}, {a, aa, true}, {a, a, false}, {aa, a, false}, {b, aa, false}, {a, b, false}} {
		if got := tt.anc.IsAncestorOf(tt.d); got != tt.want {
			t.Errorf("%s.IsAncestorOf(%s) = %v, want %v", tt.anc.ID, tt.d.ID, got, tt.want)
		}
	}
}
