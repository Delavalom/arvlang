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
