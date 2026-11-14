package e2e

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gatedListener holds every Accept until open is closed, so a test can
// leave a connection, request bytes and all, queued in the kernel's
// backlog after its client has gone: what a loaded server does to a
// child process's cancelled request.
type gatedListener struct {
	net.Listener
	open chan struct{}
}

func (l *gatedListener) Accept() (net.Conn, error) {
	<-l.open
	return l.Listener.Accept()
}

// queuedRequest writes a whole request for path to s and closes the
// connection before the server has accepted it.
func queuedRequest(t *testing.T, s *httptest.Server, path string) {
	t.Helper()
	c, err := net.Dial("tcp", s.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(c, "GET "+path+" HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	c.Close()
}

// The final round's fix for the TestP6PropRecordReplay flake: a request
// the record wrote before it exited can be counted after the test read the
// counter. drain must not return before such a request has been handled,
// and must not wait on another test's requests.
func TestDrainCountsRequestsAlreadyWritten(t *testing.T) {
	t.Parallel()
	var mine, theirs atomic.Int32
	release := make(chan struct{}) // lets the other test's handler return
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("id") {
		case "mine":
			time.Sleep(100 * time.Millisecond) // a loaded server, slow to count
			mine.Add(1)
		case "theirs":
			theirs.Add(1)
			<-release // a gate another test still holds
		}
	}))
	gate := &gatedListener{Listener: s.Listener, open: make(chan struct{})}
	s.Listener = gate
	ct := track(s)
	s.Start()
	t.Cleanup(s.Close)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock) // before s.Close: Close waits for handlers

	queuedRequest(t, s, "/ok?id=theirs")
	queuedRequest(t, s, "/ok?id=mine")
	queuedRequest(t, s, "/ok?id=mine")
	if n := mine.Load(); n != 0 {
		t.Fatalf("the server counted %d requests before accepting any", n)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := ct.settle(s.URL, "mine", time.Minute); err != nil {
			t.Error(err)
		}
	}()
	close(gate.open)
	select {
	case <-done:
	case <-time.After(2 * time.Minute):
		t.Fatal("drain did not return: it waited on the other test's held request")
	}
	if n := mine.Load(); n != 2 {
		t.Fatalf("after drain the server had counted %d of the 2 queued requests", n)
	}
	unblock()
}

// With id "", drain waits for every connection older than the barrier: a
// per-test server (the Phase 7 provider stand-in) has no ids.
func TestDrainAllWaitsForEveryOlderConnection(t *testing.T) {
	t.Parallel()
	var n atomic.Int32
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/messages" {
			time.Sleep(100 * time.Millisecond)
			n.Add(1)
		}
	}))
	gate := &gatedListener{Listener: s.Listener, open: make(chan struct{})}
	s.Listener = gate
	ct := track(s)
	s.Start()
	t.Cleanup(s.Close)

	for range 3 {
		queuedRequest(t, s, "/v1/messages")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := ct.settle(s.URL, "", time.Minute); err != nil {
			t.Error(err)
		}
	}()
	close(gate.open)
	<-done
	if got := n.Load(); got != 3 {
		t.Fatalf("after drain the server had counted %d of the 3 queued requests", got)
	}
}
