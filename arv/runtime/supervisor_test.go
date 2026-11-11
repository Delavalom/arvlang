package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// kindErr is an error of a given kind, as the evaluator's are.
type kindErr struct{ kind, msg string }

func (e *kindErr) Error() string     { return e.msg }
func (e *kindErr) ErrorKind() string { return e.kind }

// failing returns an attempt func that fails with kind for its first
// fails attempts, then succeeds, counting attempts and checking that they
// are numbered 1, 2, 3, ...
func failing(t *testing.T, kind string, fails int, count *atomic.Int32) func(int) error {
	return func(n int) error {
		if got := count.Add(1); int(got) != n {
			t.Errorf("attempt numbered %d, want %d", n, got)
		}
		if n <= fails {
			return &kindErr{kind, fmt.Sprintf("attempt %d failed", n)}
		}
		return nil
	}
}

func restartScope(retries int, backoff time.Duration, kinds ...string) *Scope {
	s := NewScope(context.Background())
	p := Policy{Strategy: Restart, Retries: retries, Backoff: backoff, RetryOn: map[string]bool{}}
	for _, k := range kinds {
		p.RetryOn[k] = true
	}
	s.SetPolicy(p)
	return s
}

func TestFailFastIsTheDefault(t *testing.T) {
	s := NewScope(context.Background())
	var count atomic.Int32
	var done atomic.Int32
	s.Supervise("root/0", failing(t, KindEffect, 5, &count), func() { done.Add(1) })
	if err := s.Wait(); err == nil || err.Error() != "attempt 1 failed" {
		t.Fatalf("Wait = %v, want attempt 1's error", err)
	}
	if count.Load() != 1 || done.Load() != 1 || s.Context().Err() == nil {
		t.Fatalf("attempts %d, done %d, cancelled %v; want 1, 1, true", count.Load(), done.Load(), s.Context().Err() != nil)
	}
}

// Under Collect a failure cancels nothing and is not the scope's.
func TestCollectDoesNotFailTheScope(t *testing.T) {
	s := NewScope(context.Background())
	s.SetPolicy(Policy{Strategy: Collect})
	var count atomic.Int32
	cancelled := make(chan bool, 1)
	s.Supervise("root/0", failing(t, KindEffect, 1, &count), nil)
	s.Go(func() error {
		time.Sleep(50 * time.Millisecond)
		cancelled <- s.Context().Err() != nil
		return nil
	})
	if err := s.Wait(); err != nil {
		t.Fatalf("Wait = %v, want nil", err)
	}
	if <-cancelled {
		t.Fatal("a collected failure cancelled its sibling")
	}
}

func TestRestartRecovers(t *testing.T) {
	s := restartScope(3, time.Millisecond, KindEffect)
	var count atomic.Int32
	s.Supervise("root/0", failing(t, KindEffect, 2, &count), nil)
	if err := s.Wait(); err != nil || count.Load() != 3 {
		t.Fatalf("Wait = %v after %d attempts, want nil after 3", err, count.Load())
	}
}

// After Retries reruns the last error fails the scope.
func TestRestartGivesUp(t *testing.T) {
	s := restartScope(2, time.Millisecond, KindEffect)
	var count atomic.Int32
	s.Supervise("root/0", failing(t, KindEffect, 100, &count), nil)
	if err := s.Wait(); err == nil || err.Error() != "attempt 3 failed" || count.Load() != 3 {
		t.Fatalf("Wait = %v after %d attempts, want attempt 3's error after 3", err, count.Load())
	}
}

// Only the kinds in RetryOn restart; a capability or budget error would
// fail the same way again, and an error without a kind is a runtime one.
func TestRestartOnlyRetryableKinds(t *testing.T) {
	for _, err := range []error{
		&kindErr{KindUser, "user"}, &DeniedError{Want: Capability{Kind: Env, Scope: "X"}},
		&ExceededError{Kind: "steps", Limit: 1}, errors.New("no kind"),
	} {
		s := restartScope(3, time.Millisecond, KindEffect)
		var count atomic.Int32
		s.Supervise("root/0", func(int) error { count.Add(1); return err }, nil)
		if got := s.Wait(); got != err || count.Load() != 1 {
			t.Errorf("%v: Wait = %v after %d attempts, want it after 1", err, got, count.Load())
		}
	}
	s := restartScope(3, time.Millisecond, KindEffect, KindUser)
	var count atomic.Int32
	s.Supervise("root/0", failing(t, KindUser, 1, &count), nil)
	if err := s.Wait(); err != nil || count.Load() != 2 {
		t.Fatalf("retry_on user: Wait = %v after %d attempts", err, count.Load())
	}
}

// Backoff doubles, plus at most a tenth, capped at MaxBackoff; the same
// task and attempt always wait the same.
func TestDelayBounds(t *testing.T) {
	p := Policy{Strategy: Restart, Backoff: 100 * time.Millisecond}
	for n, base := range map[int]time.Duration{1: 100, 2: 200, 3: 400, 4: 800} {
		base *= time.Millisecond
		d := p.Delay("root/0", n)
		if d < base || d > base+base/10 {
			t.Errorf("Delay(attempt %d) = %s, want in [%s, %s]", n, d, base, base+base/10)
		}
		if d != p.Delay("root/0", n) {
			t.Errorf("Delay(attempt %d) is not deterministic", n)
		}
	}
	for _, n := range []int{10, 64, 1000} {
		if d := p.Delay("root/0", n); d != MaxBackoff {
			t.Errorf("Delay(attempt %d) = %s, want the cap %s", n, d, MaxBackoff)
		}
	}
	if d := (Policy{Backoff: time.Hour}).Delay("x", 1); d != MaxBackoff {
		t.Errorf("an hour's backoff waits %s, want the cap", d)
	}
	if d := (Policy{}).Delay("x", 5); d != 0 {
		t.Errorf("no backoff waits %s, want 0", d)
	}
}

// The waits add up: two failures with 100 ms backoff take at least 300 ms.
func TestRestartWaits(t *testing.T) {
	s := restartScope(3, 100*time.Millisecond, KindEffect)
	var count atomic.Int32
	start := time.Now()
	s.Supervise("root/0", failing(t, KindEffect, 2, &count), nil)
	if err := s.Wait(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Fatalf("took %s, want at least 300ms", elapsed)
	}
}

// Cancelling the scope ends a backoff at once, and nothing runs again.
func TestCancelDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := NewScope(ctx)
	s.SetPolicy(Policy{Strategy: Restart, Retries: 3, Backoff: time.Hour, RetryOn: map[string]bool{KindEffect: true}})
	var count atomic.Int32
	failed := make(chan struct{})
	s.Supervise("root/0", func(n int) error {
		count.Add(1)
		close(failed)
		return &kindErr{KindEffect, "boom"}
	}, nil)
	<-failed
	start := time.Now()
	cancel()
	s.Wait()
	if elapsed := time.Since(start); elapsed > 10*time.Second || count.Load() != 1 {
		t.Fatalf("Wait took %s after %d attempts, want prompt after 1", elapsed, count.Load())
	}
}

// A scope's deadline also ends a backoff, and the scope reports it.
func TestDeadlineDuringBackoff(t *testing.T) {
	cause := &ExceededError{Kind: "timeout", Limit: 50}
	s := NewTimedScope(context.Background(), 50*time.Millisecond, cause)
	s.SetPolicy(Policy{Strategy: Restart, Retries: 3, Backoff: time.Hour, RetryOn: map[string]bool{KindEffect: true}})
	s.Supervise("root/0", func(int) error { return &kindErr{KindEffect, "boom"} }, nil)
	start := time.Now()
	if err := s.Wait(); err != cause {
		t.Fatalf("Wait = %v, want the timeout", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("Wait took %s", elapsed)
	}
}

// A panic fails the scope under any policy, and done still runs, after
// the scope is cancelled.
func TestSupervisePanic(t *testing.T) {
	for _, st := range []Strategy{FailFast, Collect, Restart} {
		s := NewScope(context.Background())
		s.SetPolicy(Policy{Strategy: st, Retries: 3, RetryOn: map[string]bool{KindEffect: true, KindRuntime: true}})
		var cancelledAtDone atomic.Bool
		s.Supervise("root/0", func(int) error { panic("boom") }, func() {
			cancelledAtDone.Store(s.Context().Err() != nil)
		})
		if err := s.Wait(); err == nil || err.Error() != "panic in task: boom" {
			t.Errorf("strategy %d: Wait = %v, want the panic", st, err)
		}
		if !cancelledAtDone.Load() {
			t.Errorf("strategy %d: done ran before the scope was cancelled", st)
		}
	}
}
