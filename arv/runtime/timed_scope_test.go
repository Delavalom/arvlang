package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTimedScopeExpires(t *testing.T) {
	cause := &ExceededError{Kind: "timeout", Limit: 50}
	s := NewTimedScope(context.Background(), 50*time.Millisecond, cause)
	s.Go(func() error {
		<-s.Context().Done()
		return context.Cause(s.Context())
	})
	if err := s.Wait(); err != cause {
		t.Fatalf("Wait = %v, want the timeout cause", err)
	}
	if s.Expired() != cause {
		t.Fatalf("Expired = %v, want the timeout cause", s.Expired())
	}
}

func TestTimedScopeFailureFirst(t *testing.T) {
	cause := &ExceededError{Kind: "timeout", Limit: 10_000}
	s := NewTimedScope(context.Background(), 10*time.Second, cause)
	boom := errors.New("boom")
	s.Go(func() error { return boom })
	if err := s.Wait(); err != boom || s.Expired() != nil {
		t.Fatalf("Wait = %v, Expired = %v; want boom and nil", err, s.Expired())
	}
}

// I6: an enclosing deadline that comes first ends the inner scope, but it
// is the enclosing scope's timeout, not the inner one's.
func TestTimedScopeNested(t *testing.T) {
	outerCause := &ExceededError{Kind: "timeout", Limit: 50}
	outer := NewTimedScope(context.Background(), 50*time.Millisecond, outerCause)
	inner := NewTimedScope(outer.Context(), 10*time.Second, &ExceededError{Kind: "timeout", Limit: 10_000})
	start := time.Now()
	<-inner.Context().Done()
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("inner scope ended after %s, want the outer 50 ms deadline", d)
	}
	inner.Wait()
	outer.Wait()
	if inner.Expired() != nil || outer.Expired() != outerCause {
		t.Fatalf("inner.Expired = %v, outer.Expired = %v", inner.Expired(), outer.Expired())
	}
}

func TestUntimedScopeNeverExpires(t *testing.T) {
	s := NewScope(context.Background())
	s.Fail(errors.New("stop"))
	s.Wait()
	if s.Expired() != nil {
		t.Fatal("a scope without a deadline reported Expired")
	}
}

// A scope that closes after its deadline has timed out, even if its work
// never looked at the context and nothing failed.
func TestTimedScopeOverrun(t *testing.T) {
	cause := &ExceededError{Kind: "timeout", Limit: 20}
	s := NewTimedScope(context.Background(), 20*time.Millisecond, cause)
	s.Go(func() error { time.Sleep(200 * time.Millisecond); return nil })
	if err := s.Wait(); err != cause || s.Expired() != cause {
		t.Fatalf("Wait = %v, Expired = %v; want the timeout cause", err, s.Expired())
	}

	// The timer may fire late on a loaded machine; Wait checks the
	// deadline itself, so a context not yet cancelled still counts.
	s = NewTimedScope(context.Background(), time.Hour, cause)
	s.deadline = time.Now().Add(-time.Millisecond)
	if err := s.Wait(); err != cause || s.Expired() != cause {
		t.Fatalf("past deadline, timer not fired: Wait = %v, Expired = %v", err, s.Expired())
	}

	s = NewTimedScope(context.Background(), time.Hour, cause)
	if err := s.Wait(); err != nil || s.Expired() != nil {
		t.Fatalf("in time: Wait = %v, Expired = %v; want nil, nil", err, s.Expired())
	}
}

// A failure after the deadline, before its timer fired, is not what ended
// the scope: the deadline came first.
func TestTimedScopeFailureAfterDeadline(t *testing.T) {
	cause := &ExceededError{Kind: "timeout", Limit: 20}
	s := NewTimedScope(context.Background(), time.Hour, cause)
	s.deadline = time.Now().Add(-time.Millisecond)
	s.Fail(errors.New("too late"))
	if err := s.Wait(); err != cause || s.Expired() != cause {
		t.Fatalf("Wait = %v, Expired = %v, want the timeout cause for both", err, s.Expired())
	}
}

// Once the deadline's timer has cancelled the scope, the first failure
// recorded (often a task reporting that cancellation) is not what Wait
// returns: the scope timed out.
func TestTimedScopeFailureAfterTimer(t *testing.T) {
	cause := &ExceededError{Kind: "timeout", Limit: 1}
	s := NewTimedScope(context.Background(), time.Millisecond, cause)
	<-s.Context().Done()
	s.Fail(errors.New("sleep cancelled: budget exceeded: timeout (limit 1 ms)"))
	if err := s.Wait(); err != cause || s.Expired() != cause {
		t.Fatalf("Wait = %v, Expired = %v, want the timeout cause for both", err, s.Expired())
	}
}
