package e2e

import (
	"io"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Server routes for the concurrency tests. They order events on the server
// instead of with sleeps, so the tests stay correct on a loaded machine.
//
//	/arrive?k=K       200, counts one arrival at gate K
//	/wait?k=K&n=N     blocks until gate K has N arrivals: 200 "open", or
//	                  504 after gateTimeout (a broken script never hangs)
//	/slow?ms=N        blocks until the client goes away or N ms pass, and
//	                  records which happened; also counts as an arrival at
//	                  gate "slow"
//
// Like every route they take ?id=<test id>, so parallel tests never share
// a gate.
const gateTimeout = 15 * time.Second

var (
	gateMu   sync.Mutex
	gateN    = map[string]int{}     // id+"/"+gate -> arrivals
	slowEnds = map[string]slowEnd{} // id -> how its /slow request ended
	gateWake = make(chan struct{})  // closed and replaced on every change
)

// slowEnd is how one /slow request ended.
type slowEnd struct {
	cancelled bool          // the client went away before ms passed
	after     time.Duration // time from arrival to the end
}

func addGateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/arrive", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		q := r.URL.Query()
		arrive(q.Get("id"), q.Get("k"))
	})
	mux.HandleFunc("/wait", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		q := r.URL.Query()
		n, err := strconv.Atoi(q.Get("n"))
		if err != nil {
			http.Error(w, "bad n", http.StatusBadRequest)
			return
		}
		key := q.Get("id") + "/" + q.Get("k")
		timeout := time.NewTimer(gateTimeout)
		defer timeout.Stop()
		for {
			gateMu.Lock()
			open, wake := gateN[key] >= n, gateWake
			gateMu.Unlock()
			if open {
				io.WriteString(w, "open")
				return
			}
			select {
			case <-wake:
			case <-r.Context().Done():
				return
			case <-timeout.C:
				http.Error(w, "gate timeout", http.StatusGatewayTimeout)
				return
			}
		}
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		q := r.URL.Query()
		ms, err := strconv.Atoi(q.Get("ms"))
		if err != nil {
			http.Error(w, "bad ms", http.StatusBadRequest)
			return
		}
		id := q.Get("id")
		start := time.Now()
		arrive(id, "slow")
		end := slowEnd{}
		select {
		case <-r.Context().Done():
			end.cancelled = true
		case <-time.After(time.Duration(ms) * time.Millisecond):
			io.WriteString(w, "slow done")
		}
		end.after = time.Since(start)
		gateMu.Lock()
		slowEnds[id] = end
		broadcastLocked()
		gateMu.Unlock()
	})
}

func arrive(id, gate string) {
	gateMu.Lock()
	defer gateMu.Unlock()
	gateN[id+"/"+gate]++
	broadcastLocked()
}

// broadcastLocked wakes every waiter; gateMu must be held.
func broadcastLocked() {
	close(gateWake)
	gateWake = make(chan struct{})
}

// waitSlowEnd returns how this test's /slow request ended. The server sees
// the end asynchronously (when it notices the closed connection), so this
// may wait up to limit after the process has exited.
func waitSlowEnd(t *testing.T, limit time.Duration) (slowEnd, bool) {
	t.Helper()
	id := testID(t)
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	for {
		gateMu.Lock()
		end, ok := slowEnds[id]
		wake := gateWake
		gateMu.Unlock()
		if ok {
			return end, true
		}
		select {
		case <-wake:
		case <-deadline.C:
			return slowEnd{}, false
		}
	}
}
