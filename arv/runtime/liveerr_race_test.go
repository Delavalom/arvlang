package runtime

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Review round 5 (Critical): liveErr looked at the scopes' deadlines
// (expireDue) and then at the context's own deadline. A deadline that
// passed between the two looks, or whose timer was still cancelling the
// scopes below it, came back as context.DeadlineExceeded, a cause no
// timed scope names: the check was logged with no By, and its replay
// waited for a cancellation that only that deadline's firing could
// deliver, while the firing waited for the records after it. An honest
// log stalled. Checks made right at a scope's deadline must see its
// timeout.
func TestLiveErrAtTheDeadlineIsTheTimeout(t *testing.T) {
	rounds := 300
	if testing.Short() {
		rounds = 60
	}
	var other atomic.Int64
	var example atomic.Value
	for range rounds {
		cause := &ExceededError{Kind: "timeout", Limit: 1}
		s := NewTimedScope(context.Background(), time.Millisecond, cause)
		mid := NewScope(s.Context())
		inner := NewScope(mid.Context())
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					if err := liveErr(inner.Context()); err != nil {
						if err != cause {
							other.Add(1)
							example.Store(err)
						}
						return
					}
				}
			}()
		}
		wg.Wait()
		inner.Wait()
		mid.Wait()
		s.Wait()
	}
	if n := other.Load(); n > 0 {
		t.Fatalf("%d checks at a deadline saw %v, not the scope's timeout", n, example.Load())
	}
}
