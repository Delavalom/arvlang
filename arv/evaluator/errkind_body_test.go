package evaluator

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// A read that fails with an error that Is context.DeadlineExceeded (as
// net/http's client timeout while reading a body does) failed in the
// world unless the task itself was cancelled: kind effect, so restart
// retries it. Only a cancelled task's failed read is cancelled.
func TestReadErrorKindFollowsTheTask(t *testing.T) {
	live := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dead := runtime.NewRoot(ctx, runtime.NewCapSet(), io.Discard)
	for _, err := range []error{
		context.DeadlineExceeded,
		&url.Error{Op: "Get", URL: "http://x/", Err: context.DeadlineExceeded},
		io.ErrUnexpectedEOF,
	} {
		if e := readError(live, "x", err); e.ErrKind() != runtime.KindEffect {
			t.Errorf("live task, %v: kind %q, want effect", err, e.ErrKind())
		}
		if e := readError(dead, "x", err); e.ErrKind() != runtime.KindCancelled {
			t.Errorf("cancelled task, %v: kind %q, want cancelled", err, e.ErrKind())
		}
	}
	over := &runtime.ExceededError{Kind: "alloc", Limit: 1}
	if e := readError(live, "x", over); e.ErrKind() != runtime.KindBudget {
		t.Errorf("a spent budget: kind %q, want budget", e.ErrKind())
	}
}

// Through the real client: a server that sends the headers and part of
// the body, then stalls past the client's timeout, so net/http's own
// body-timeout error reaches readError. The test must take that path:
// under heavy load the timeout may pass before the headers arrive (the
// Do() path, which says nothing about the body), so it retries with a
// doubled timeout until the error names the body, and fails if it never
// does.
func TestStalledBodyIsAnEffect(t *testing.T) {
	defer func(d time.Duration) { clientTimeout = d }(clientTimeout)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		io.WriteString(w, "partial")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	task := runtime.NewRoot(context.Background(), runtime.NewCapSet(grant(t, runtime.Net, "127.0.0.1")), io.Discard)
	for d := time.Second; d <= 32*time.Second; d *= 2 {
		clientTimeout = d
		got := httpGet(task, func(int64) error { return nil }, &value.String{Value: srv.URL})
		e, ok := got.(*value.Error)
		if !ok {
			t.Fatalf("got %s %s, want an error", got.Type(), got.Inspect())
		}
		if !strings.HasPrefix(e.Message, "http_get: reading body") {
			t.Logf("timeout %v passed before the body (load?): %s", d, e.Message)
			continue
		}
		if e.ErrKind() != runtime.KindEffect {
			t.Fatalf("a body read that timed out: kind %q (%s), want effect", e.ErrKind(), e.Message)
		}
		return
	}
	t.Fatal("the client never got the headers before its timeout, so the body path was not tested")
}

// http_get's own cap on a body's size is a failure in the world.
func TestHTTPBodyTooBigIsAnEffect(t *testing.T) {
	chunk := strings.Repeat("x", 1<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for range maxBody>>20 + 1 {
			if _, err := io.WriteString(w, chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	task := runtime.NewRoot(context.Background(), runtime.NewCapSet(grant(t, runtime.Net, "127.0.0.1")), io.Discard)
	e := wantKind(t, httpGet(task, func(int64) error { return nil }, &value.String{Value: srv.URL}), runtime.KindEffect)
	if !strings.Contains(e.Message, "exceeds") {
		t.Fatalf("got %q, want the size cap", e.Message)
	}
}
