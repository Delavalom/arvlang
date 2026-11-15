package arv

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// The host sees what caused an error that ended the run.
func TestRunErrorKind(t *testing.T) {
	for _, tt := range []struct{ src, kind string }{
		{`1 / 0`, runtime.KindRuntime},
		{`scope (timeout_ms: 10) { sleep(5000) }`, runtime.KindBudget},
		{`let f = fn() { 1 }; spawn f() with [env("ARV_KIND_X")]`, runtime.KindCapability},
	} {
		_, err := Run(tt.src, Options{})
		var ae *Error
		if !errors.As(err, &ae) || ae.Kind != tt.kind {
			t.Errorf("%s: got %#v, want an *Error of kind %q", tt.src, err, tt.kind)
		}
	}
}

// lineWatch closes seen once n lines have been written to it.
type lineWatch struct {
	mu   sync.Mutex
	n    int
	seen chan struct{}
}

func (w *lineWatch) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, b := range p {
		if b == '\n' {
			if w.n--; w.n == 0 {
				close(w.seen)
			}
		}
	}
	return len(p), nil
}

// A host cancel while restarted tasks wait out their backoff reports kind
// cancelled, as the same cancel does under fail_fast or collect, not the
// last attempt's user error.
func TestHostCancelDuringRestartBackoffIsCancelled(t *testing.T) {
	before := settle(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &lineWatch{n: 2, seen: make(chan struct{})}
	go func() {
		<-out.seen
		time.Sleep(200 * time.Millisecond) // let both attempts fail and start waiting
		cancel()
	}()
	_, err := Run(`let f = fn() { puts("failing"); error("x") };
		scope (supervise: "restart", retries: 1000, backoff_ms: 30000, retry_on: ["user"]) { spawn f(); spawn f() }`,
		Options{Ctx: ctx, Out: out})
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != runtime.KindCancelled {
		t.Errorf("got %#v, want an *Error of kind cancelled", err)
	}
	if after := settle(before); after > before {
		t.Fatalf("%d goroutines before the run, %d after", before, after)
	}
}
