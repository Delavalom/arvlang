package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

// A reply over maxResponse is refused without being read to the end, and
// its connection is still closed: no goroutine of the transport or of the
// connection outlives the call (mutation M49: io.ReadAll reads a smaller
// reply to EOF, which releases the connection even if the body is never
// closed). The server writes until the client hangs up, at most 64 MiB.
func TestAnthropicOversizedReplyLeavesNoGoroutine(t *testing.T) {
	chunk := []byte(strings.Repeat("x", 1<<16))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		for n := 0; n < 64<<20; n += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	defer srv.CloseClientConnections() // a leaked connection would block Close
	a := NewAnthropic(testKey, srv.URL)

	before := goruntime.NumGoroutine()
	_, err := a.Complete(context.Background(), Request{Model: "m", MaxTokens: 1})
	var e *Error
	if !errors.As(err, &e) || e.Kind != ErrDecode || !strings.Contains(e.Message, "MiB") {
		t.Fatalf("Complete = %v, want a decode error for the reply's size", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for goruntime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			t.Fatalf("%d goroutines before the call, %d after:\n%s",
				before, goruntime.NumGoroutine(), buf[:goruntime.Stack(buf, true)])
		}
		time.Sleep(20 * time.Millisecond)
	}
}
