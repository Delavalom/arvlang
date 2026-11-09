package runtime

import (
	"bytes"
	"context"
	"errors"
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
