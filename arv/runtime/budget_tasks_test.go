package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

// acquireAsync calls b.Acquire on a new goroutine and reports the result.
func acquireAsync(ctx context.Context, b *Budget) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- b.Acquire(ctx) }()
	return ch
}

// blocked fails the test unless ch stays empty for a moment.
func blocked(t *testing.T, ch <-chan error) {
	t.Helper()
	select {
	case err := <-ch:
		t.Fatalf("Acquire returned %v, want it to block", err)
	case <-time.After(50 * time.Millisecond):
	}
}

// done fails the test unless ch delivers within 10 s, and returns the error.
func done(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("Acquire still blocked after 10 s")
		return nil
	}
}

func TestAcquireBlocksUntilRelease(t *testing.T) {
	ctx := context.Background()
	b := Root(Limits{}).Child(Limits{Tasks: 2})
	for i := 0; i < 2; i++ {
		if err := b.Acquire(ctx); err != nil {
			t.Fatal(err)
		}
	}
	third := acquireAsync(ctx, b)
	blocked(t, third)
	b.Release()
	if err := done(t, third); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireGivesUpOnCancel(t *testing.T) {
	b := Root(Limits{}).Child(Limits{Tasks: 1})
	if err := b.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("scope failed")
	ctx, cancel := context.WithCancelCause(context.Background())
	second := acquireAsync(ctx, b)
	blocked(t, second)
	cancel(cause)
	if err := done(t, second); err != cause {
		t.Fatalf("Acquire = %v, want the context's cause", err)
	}
	// the cancelled Acquire held nothing: one Release frees the only slot
	b.Release()
	if err := b.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// I6 within a task: a nested scope's spawns also take the enclosing
// scope's slots, so tasks: 10 inside tasks: 2 still runs two at a time.
func TestNestedScopeSlotsShrink(t *testing.T) {
	ctx := context.Background()
	outer := Root(Limits{}).Child(Limits{Tasks: 2})
	inner := outer.Child(Limits{Tasks: 10})
	for i := 0; i < 2; i++ {
		if err := inner.Acquire(ctx); err != nil {
			t.Fatal(err)
		}
	}
	third := acquireAsync(ctx, inner)
	blocked(t, third)
	inner.Release()
	if err := done(t, third); err != nil {
		t.Fatal(err)
	}
}

// "tasks" counts direct children: a task's own spawns never wait for the
// slots of the scope it was spawned into (else tasks: 2 with two children
// that each spawn a grandchild would deadlock).
func TestTaskBoundaryStopsSlots(t *testing.T) {
	ctx := context.Background()
	scope := Root(Limits{}).Child(Limits{Tasks: 2})
	for i := 0; i < 2; i++ {
		if err := scope.Acquire(ctx); err != nil {
			t.Fatal(err)
		}
	}
	child := scope.Task() // body of one of the two children
	if err := done(t, acquireAsync(ctx, child)); err != nil {
		t.Fatalf("grandchild spawn = %v", err)
	}
	if err := done(t, acquireAsync(ctx, child.Child(Limits{}))); err != nil {
		t.Fatalf("grandchild spawn from a nested scope = %v", err)
	}
}

// The live-task cap counts every task in the run, at any depth, and fails
// instead of blocking, so spawn recursion ends in an error.
func TestLiveTaskCap(t *testing.T) {
	ctx := context.Background()
	root := Root(Limits{LiveTasks: 2})
	a := root.Task()
	if err := root.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	wantExceeded(t, a.Task().Acquire(ctx), "tasks", 2)
	a.Release()
	if err := a.Task().Acquire(ctx); err != nil {
		t.Fatalf("after a release: %v", err)
	}
}

// A failed live-task check gives back the slots it took on the way.
func TestLiveTaskCapReleasesSlots(t *testing.T) {
	ctx := context.Background()
	root := Root(Limits{LiveTasks: 1})
	scope := root.Child(Limits{Tasks: 5})
	if err := root.Task().Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ { // would fill the 5 slots if failures kept them
		wantExceeded(t, scope.Acquire(ctx), "tasks", 1)
	}
	if n := len(scope.slots); n != 0 {
		t.Fatalf("%d slots still held after failed acquires", n)
	}
}

// A cancelled Acquire gives back the slots it already took at lower
// levels. Here inner has room but outer is full, so the second Acquire
// takes inner's only slot and then blocks on outer. Once it is cancelled,
// inner's slot must be free again: a scope that outlives a failed spawn
// would otherwise run with one slot fewer for good.
func TestAcquireCancelFreesLowerSlots(t *testing.T) {
	outer := Root(Limits{}).Child(Limits{Tasks: 1})
	inner := outer.Child(Limits{Tasks: 1})
	sibling := outer.Child(Limits{})
	if err := sibling.Acquire(context.Background()); err != nil { // outer is full
		t.Fatal(err)
	}
	cause := errors.New("scope failed")
	ctx, cancel := context.WithCancelCause(context.Background())
	second := acquireAsync(ctx, inner)
	blocked(t, second) // holds inner's slot, waits for outer's
	cancel(cause)
	if err := done(t, second); err != cause {
		t.Fatalf("Acquire = %v, want the context's cause", err)
	}
	if n := len(inner.slots); n != 0 {
		t.Fatalf("inner holds %d slots after the cancelled Acquire, want 0", n)
	}
	sibling.Release() // outer is free again, so inner can take both
	if err := done(t, acquireAsync(context.Background(), inner)); err != nil {
		t.Fatal(err)
	}
}
