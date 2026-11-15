package evaluator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// stallingListener accepts TCP connections on loopback and never writes,
// so a TLS handshake to it stalls after the ClientHello. Each accepted
// connection is sent on the returned channel; all are closed at cleanup.
func stallingListener(t *testing.T) (string, <-chan net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan net.Conn, 16)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- c
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		<-stopped
		for {
			select {
			case c := <-accepted:
				c.Close()
			default:
				return
			}
		}
	})
	return ln.Addr().String(), accepted
}

const dialFrame = "net/http.(*Transport).dialConnFor("

// Leak rule: an http_get cancelled while its connection is still being
// dialed (here, stalled in the TLS handshake) must not leave the dial
// running after arv.Run. net/http dials on a context detached from the
// request's (context.WithoutCancel), so cancelling the request does not
// stop the dial; only the deferred client.CloseIdleConnections does, by
// cancelling dials no request waits for. Without it the dial, and its
// connection to the server, outlive the run until the transport's 10s
// TLS handshake timeout (mutation check, Phase 8.3 round 4: removing the
// deferred CloseIdleConnections passed every other test).
//
// The order is fixed by the server, not by sleeps: the run is cancelled
// once the server has accepted the connection. The handshake timer
// started after start, so without the fix the client keeps the connection
// open until at least start+10s. The test needs the cancel by start+2s
// (else it retries: a loaded machine can only make it retry, not fail)
// and then gives the client 8s to close the connection.
func TestHTTPGetCancelledMidDialLeavesNoDial(t *testing.T) {
	addr, accepted := stallingListener(t)
	p := parser.New(lexer.New(fmt.Sprintf(`http_get("https://%s/")`, addr)))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	caps := runtime.NewCapSet(grant(t, runtime.Net, "127.0.0.1"))
	for attempt := 1; attempt <= 5; attempt++ {
		ctx, cancel := context.WithCancel(context.Background())
		var out bytes.Buffer
		root := runtime.NewRoot(ctx, caps, &out)
		start := time.Now()
		done := make(chan value.Object, 1)
		go func() { done <- New(root).Run(program, value.NewEnvironment()) }()

		var conn net.Conn
		select {
		case conn = <-accepted:
		case got := <-done:
			cancel()
			t.Fatalf("http_get returned before the server saw its connection: %s", got.Inspect())
		case <-time.After(2 * time.Minute):
			cancel()
			t.Fatal("the server saw no connection in 2 minutes")
		}
		cancelled := time.Now()
		cancel()
		select {
		case got := <-done:
			wantErrorHas(t, got, "http_get: ")
		case <-time.After(2 * time.Minute):
			conn.Close()
			t.Fatal("the run did not return after its context was cancelled")
		}
		if cancelled.Sub(start) > 2*time.Second {
			t.Logf("attempt %d: cancelled %s after start; too late to tell, retrying", attempt, cancelled.Sub(start))
			conn.Close()
			waitNoDial(t)
			continue
		}

		closed := readUntilClosed(conn, cancelled.Add(8*time.Second))
		conn.Close()
		if !closed {
			t.Fatalf("8s after the run was cancelled, its dial still held the connection open")
		}
		waitNoDial(t)
		return
	}
	t.Skip("the machine was too slow to cancel the run within 2s in 5 attempts")
}

// readUntilClosed reads c until the peer closes it (true) or until
// deadline (false).
func readUntilClosed(c net.Conn, deadline time.Time) bool {
	c.SetReadDeadline(deadline)
	buf := make([]byte, 4096)
	for {
		if _, err := c.Read(buf); err != nil {
			return !errors.Is(err, os.ErrDeadlineExceeded)
		}
	}
}

// waitNoDial fails unless every transport dial is gone within a generous
// bound (the machine may be loaded).
func waitNoDial(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		buf := make([]byte, 1<<20)
		stacks := string(buf[:goruntime.Stack(buf, true)])
		if !strings.Contains(stacks, dialFrame) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("a dial outlived the run:\n%s", stacks)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
