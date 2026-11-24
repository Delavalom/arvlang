package runtime

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync/atomic"
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
	//lint:ignore SA4000 each Err call consumes one numbered check from the log
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
	//lint:ignore SA4000 each Err call consumes one numbered check from the log
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

// The live-task cap and a restart backoff are checks too: replayed, a
// spawn fails at the cap exactly where it did, and a backoff waits for
// nothing unless it was cut short.
func TestReplayedAcquireAndBackoff(t *testing.T) {
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	b := Root(Limits{LiveTasks: 1})
	if err := root.Acquire(b); err != nil {
		t.Fatal(err)
	}
	if err := root.Acquire(b); err == nil {
		t.Fatal("past the cap")
	}
	if !root.Backoff("root#2", func() bool { return true }) {
		t.Fatal("backoff cut short")
	}
	l.Finish([]byte(`[null]`))

	l, root = replayer(t, log.Bytes(), &bytes.Buffer{})
	b = Root(Limits{LiveTasks: 1})
	b.live.n.Add(1) // another task holds the only live slot in this run
	if err := root.Acquire(b); err != nil {
		t.Fatalf("replayed spawn hit this run's cap: %v", err)
	}
	var ex *ExceededError
	if err := root.Acquire(b); !errors.As(err, &ex) || ex.Limit != 1 {
		t.Fatalf("second spawn = %v, want the tasks budget error", err)
	}
	if !root.Backoff("root#2", func() bool { t.Error("a replayed backoff waited"); return true }) {
		t.Fatal("replayed backoff cut short")
	}
	if err := l.Finish([]byte(`[null]`)); err != nil {
		t.Fatal(err)
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

// SuperviseWith hands each restart's backoff to its caller, which may
// skip the wait (as a replay does) or stop the restarts.
func TestSuperviseWithDecidesEachBackoff(t *testing.T) {
	s := restartScope(5, time.Hour, KindEffect) // waiting out a backoff would time the test out
	var count atomic.Int32
	var asked []int
	s.SuperviseWith("root/0", failing(t, KindEffect, 100, &count), func(n int, wait func() bool) bool {
		asked = append(asked, n)
		return n < 2
	}, nil, nil)
	if err := s.Wait(); err == nil || count.Load() != 2 || !slices.Equal(asked, []int{1, 2}) {
		t.Fatalf("Wait = %v after %d attempts, backoffs asked %v; want an error after 2, [1 2]", err, count.Load(), asked)
	}
}

// A scope's deadline at close is a check: replayed, a scope times out
// exactly when the recorded one did, whatever this run's clock says.
func TestReplayedExpiry(t *testing.T) {
	late, early := errors.New("late"), errors.New("early")
	var log bytes.Buffer
	l, _ := NewRecorder(&log, Header{}, &bytes.Buffer{}, nil)
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	s := NewTimedScope(root.Ctx, time.Nanosecond, late)
	<-s.Context().Done()
	s.Wait()
	if err := root.Expired(s); err != late {
		t.Fatalf("first scope: Expired = %v", err)
	}
	s = NewTimedScope(root.Ctx, time.Hour, early)
	s.Wait()
	if err := root.Expired(s); err != nil {
		t.Fatalf("second scope: Expired = %v", err)
	}
	l.Finish([]byte(`[null]`))

	l, root = replayer(t, log.Bytes(), &bytes.Buffer{})
	s = NewTimedScope(root.Ctx, time.Hour, late) // has not expired in this run
	s.Wait()
	if err := root.Expired(s); err != late {
		t.Fatalf("replayed first scope: Expired = %v", err)
	}
	s = NewTimedScope(root.Ctx, time.Nanosecond, early) // has expired in this run
	<-s.Context().Done()
	s.Wait()
	if err := root.Expired(s); err != nil {
		t.Fatalf("replayed second scope: Expired = %v", err)
	}
	if err := l.Finish([]byte(`[null]`)); err != nil {
		t.Fatal(err)
	}
}
