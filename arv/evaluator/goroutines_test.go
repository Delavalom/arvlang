package evaluator

import (
	goruntime "runtime"
	"testing"
	"time"
)

// settled fails the test unless the goroutine count comes back to at most
// before within 10 s: nothing a run started may outlive it.
func settled(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		n := goruntime.NumGoroutine()
		if n <= before {
			return
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("%d goroutines still running, %d before the run:\n%s",
				n, before, buf[:goruntime.Stack(buf, true)])
		}
		time.Sleep(10 * time.Millisecond)
	}
}
