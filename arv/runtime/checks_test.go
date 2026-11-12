package runtime

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

// A task records where it saw its cancellation (the third check here);
// replayed, it does not see it before that check, and at that check it
// waits until this run cancels it too.
func TestReplayedCancellationIsSeenAtTheSameCheck(t *testing.T) {
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	ctx := l.Start(context.Background())
	sctx, cancel := context.WithCancelCause(ctx)
	task := NewRoot(ctx, NewCapSet(), &bytes.Buffer{})
	task.SetLog(l)
	task = task.Child(sctx, NewCapSet())
	if task.Err() != nil || task.Err() != nil {
		t.Fatal("cancelled too early")
	}
	cancel(errors.New("sibling failed"))
	if err := task.Err(); err == nil || err.Error() != "sibling failed" {
		t.Fatalf("Err = %v", err)
	}
	l.Finish([]byte(`[1]`))

	_, root := replayer(t, log.Bytes(), &bytes.Buffer{})
	rctx, rcancel := context.WithCancelCause(root.Ctx)
	rcancel(errors.New("sibling failed")) // already cancelled: the first two checks still pass
	task = root.Child(rctx, NewCapSet())
	if task.Err() != nil || task.Err() != nil {
		t.Fatal("replay saw the cancellation before the recorded check")
	}
	if err := task.Err(); err == nil || err.Error() != "sibling failed" {
		t.Fatalf("third check = %v", err)
	}

	_, root = replayer(t, log.Bytes(), &bytes.Buffer{})
	rctx, rcancel = context.WithCancelCause(root.Ctx)
	task = root.Child(rctx, NewCapSet())
	task.Err()
	task.Err()
	done := make(chan error, 1)
	go func() { done <- task.Err() }()
	select {
	case err := <-done:
		t.Fatalf("the third check returned %v before this run was cancelled", err)
	case <-time.After(50 * time.Millisecond):
	}
	rcancel(errors.New("now"))
	if err := <-done; err == nil || err.Error() != "now" {
		t.Fatalf("third check = %v", err)
	}
}

// A wait is a check too: replayed, a wait the recorded run saw end still
// ends when its task does, though this run has cancelled the waiter, and
// a wait the recorded run saw cut short is cut short.
func TestReplayedWait(t *testing.T) {
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	ctx, cancel := context.WithCancelCause(root.Ctx)
	task := root.Child(ctx, NewCapSet())
	finished := make(chan struct{})
	close(finished)
	if err := task.WaitFor(finished); err != nil {
		t.Fatal(err)
	}
	cancel(errors.New("sibling failed"))
	if err := task.WaitFor(make(chan struct{})); err == nil || err.Error() != "sibling failed" {
		t.Fatalf("second wait = %v", err)
	}
	l.Finish([]byte(`[null]`))

	l, root = replayer(t, log.Bytes(), &bytes.Buffer{})
	ctx, cancel = context.WithCancelCause(root.Ctx)
	cancel(errors.New("sibling failed")) // before the first wait, this time
	task = root.Child(ctx, NewCapSet())
	if err := task.WaitFor(finished); err != nil {
		t.Fatalf("replayed first wait = %v", err)
	}
	if err := task.WaitFor(make(chan struct{})); err == nil || err.Error() != "sibling failed" {
		t.Fatalf("replayed second wait = %v", err)
	}
	if err := l.Finish([]byte(`[null]`)); err != nil {
		t.Fatal(err)
	}
}
