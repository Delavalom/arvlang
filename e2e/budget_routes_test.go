package e2e

import (
	"io"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"
)

// /inflight?k=K&ms=N holds each request for N ms (or until the client goes
// away) and records, per test id and key K, how many were in flight at
// once. The tasks-limit tests read the peak to prove backpressure from the
// server's side, where client start-up and scheduling cannot hide it.

// flight is what /inflight saw for one key.
type flight struct {
	now, peak, total int
	first, last      time.Time // first arrival, last departure
}

var (
	flightMu sync.Mutex
	flights  = map[string]*flight{} // id+"/"+k -> stats
)

func addBudgetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/inflight", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		q := r.URL.Query()
		ms, err := strconv.Atoi(q.Get("ms"))
		if err != nil {
			http.Error(w, "bad ms", http.StatusBadRequest)
			return
		}
		key := q.Get("id") + "/" + q.Get("k")
		flightMu.Lock()
		f := flights[key]
		if f == nil {
			f = &flight{first: time.Now()}
			flights[key] = f
		}
		f.now++
		f.total++
		f.peak = max(f.peak, f.now)
		flightMu.Unlock()
		select {
		case <-r.Context().Done():
		case <-time.After(time.Duration(ms) * time.Millisecond):
			io.WriteString(w, "landed")
		}
		flightMu.Lock()
		f.now--
		f.last = time.Now()
		flightMu.Unlock()
	})
}

// flightStats returns what /inflight saw for this test's key k.
func flightStats(t *testing.T, k string) flight {
	t.Helper()
	flightMu.Lock()
	defer flightMu.Unlock()
	if f := flights[testID(t)+"/"+k]; f != nil {
		return *f
	}
	return flight{}
}
