package e2e

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

// A request a child process wrote just before it exited (a task cancelled
// by fail-fast with its http_get on the wire) can sit in the server's
// socket buffer, or in the listen backlog, after the process is gone. A
// loaded server runs its handler tens of milliseconds later, so a counter
// read right after cmd.Wait can be short by one, and a later comparison
// ("the replay made no request") sees the record's own request arrive.
//
// connTracker closes that gap. It numbers connections in accept order and
// remembers the ?id of each connection's request. drain dials a barrier
// connection, which the backlog (FIFO) hands to the server after every
// older one, then waits until each older connection is closed or is known
// to carry another test's id. Every client the suite runs (arv, the Go
// host, the Anthropic adapter) disables keep-alives, so a connection
// carries one request and is closed once its handler has returned.

const drainPath = "/__drain"

type connTracker struct {
	mu   sync.Mutex
	cond *sync.Cond
	next uint64
	open map[net.Conn]*trackedConn
}

type trackedConn struct {
	seq  uint64
	seen bool   // its request line has been read
	id   string // the request's ?id
}

type trackedKey struct{}

// track numbers s's connections and wraps its handler; s must not be
// started yet. httptest.Server.Start chains its own ConnState after ours.
func track(s *httptest.Server) *connTracker {
	ct := &connTracker{open: map[net.Conn]*trackedConn{}}
	ct.cond = sync.NewCond(&ct.mu)
	s.Config.ConnContext = func(ctx context.Context, c net.Conn) context.Context {
		ct.mu.Lock()
		defer ct.mu.Unlock()
		ct.next++
		tc := &trackedConn{seq: ct.next}
		ct.open[c] = tc
		return context.WithValue(ctx, trackedKey{}, tc)
	}
	s.Config.ConnState = func(c net.Conn, st http.ConnState) {
		if st == http.StateClosed || st == http.StateHijacked {
			ct.mu.Lock()
			delete(ct.open, c)
			ct.cond.Broadcast()
			ct.mu.Unlock()
		}
	}
	inner := s.Config.Handler
	s.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tc, _ := r.Context().Value(trackedKey{}).(*trackedConn)
		if tc != nil && r.URL.Path == drainPath {
			io.WriteString(w, strconv.FormatUint(tc.seq, 10))
			return
		}
		if tc != nil {
			ct.mu.Lock()
			tc.seen, tc.id = true, r.URL.Query().Get("id")
			ct.cond.Broadcast()
			ct.mu.Unlock()
		}
		inner.ServeHTTP(w, r)
	})
	return ct
}

// settle returns once every request for id ("" means every request) that
// was written to the server before the call has been handled.
func (ct *connTracker) settle(base, id string, limit time.Duration) error {
	tr := &http.Transport{DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	resp, err := (&http.Client{Transport: tr, Timeout: limit}).Get(base + drainPath)
	if err != nil {
		return fmt.Errorf("drain barrier: %w", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("drain barrier: %w", err)
	}
	barrier, err := strconv.ParseUint(string(body), 10, 64)
	if err != nil {
		return fmt.Errorf("drain barrier answered %q", body)
	}

	expired := false
	timer := time.AfterFunc(limit, func() {
		ct.mu.Lock()
		expired = true
		ct.cond.Broadcast()
		ct.mu.Unlock()
	})
	defer timer.Stop()
	ct.mu.Lock()
	defer ct.mu.Unlock()
	for {
		pending := 0
		for _, tc := range ct.open {
			if tc.seq < barrier && (!tc.seen || id == "" || tc.id == id) {
				pending++
			}
		}
		if pending == 0 {
			return nil
		}
		if expired {
			return fmt.Errorf("%d connections older than the drain barrier still open after %s", pending, limit)
		}
		ct.cond.Wait()
	}
}

// drainLimit bounds a drain. The connections it waits on belong to a
// process that has exited, so the server only has to read them.
const drainLimit = time.Minute

var srvConns *connTracker // the shared server's tracker (newServer)

// drainServer waits until every request this test's child processes wrote
// to the shared server has been handled; call it after a process exits and
// before reading hits or gate arrivals that a later run must not change.
func drainServer(t *testing.T) {
	t.Helper()
	if err := srvConns.settle(srv.URL, testID(t), drainLimit); err != nil {
		t.Fatal(err)
	}
}
