package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestScopeWaitsForAllTasks(t *testing.T) {
	s := NewScope(context.Background())
	var done atomic.Int32
	for i := 0; i < 10; i++ {
		s.Go(func() error {
			time.Sleep(10 * time.Millisecond)
			done.Add(1)
			return nil
		})
	}
	if err := s.Wait(); err != nil {
		t.Fatal(err)
	}
	if done.Load() != 10 {
		t.Fatalf("Wait returned with %d/10 tasks done", done.Load())
	}
}

func TestScopeFailFast(t *testing.T) {
	s := NewScope(context.Background())
	boom := errors.New("boom")
	s.Go(func() error { return boom })
	s.Go(func() error {
		select {
		case <-s.Context().Done():
			return context.Cause(s.Context())
		case <-time.After(5 * time.Second):
			return errors.New("sibling was not cancelled")
		}
	})
	start := time.Now()
	if err := s.Wait(); !errors.Is(err, boom) {
		t.Fatalf("Wait = %v, want boom", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("sibling was not cancelled promptly")
	}
}

func TestScopeRecoversPanics(t *testing.T) {
	s := NewScope(context.Background())
	s.Go(func() error { panic("bad") })
	if err := s.Wait(); err == nil {
		t.Fatal("panic did not fail the scope")
	}
}
