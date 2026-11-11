package runtime

import (
	"context"
	goruntime "runtime"
	"testing"
	"time"
)

// Once Wait returns, no supervisor goroutine or backoff timer survives,
// under any strategy, including a restart cancelled mid-backoff.
func TestSuperviseLeavesNoGoroutines(t *testing.T) {
	before := goruntime.NumGoroutine()
	for _, st := range []Strategy{FailFast, Collect, Restart} {
		for _, cancelEarly := range []bool{false, true} {
			ctx, cancel := context.WithCancel(context.Background())
			s := NewScope(ctx)
			s.SetPolicy(Policy{Strategy: st, Retries: 3, Backoff: time.Hour, RetryOn: map[string]bool{KindEffect: true}})
			failed := make(chan struct{}, 4)
			for i := range 4 {
				id := "root/" + string(rune('0'+i))
				s.Supervise(id, func(int) error {
					failed <- struct{}{}
					return &kindErr{KindEffect, "boom"}
				}, func() {})
			}
			if cancelEarly || st == Restart {
				for range 4 {
					<-failed
				}
				cancel()
			}
			s.Wait()
			cancel()
		}
	}
	deadline := time.Now().Add(30 * time.Second)
	for goruntime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("%d goroutines, want at most %d:\n%s",
				goruntime.NumGoroutine(), before, buf[:goruntime.Stack(buf, true)])
		}
		time.Sleep(10 * time.Millisecond)
	}
}
