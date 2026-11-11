// Package farm is the Phase 5 gate's target farm: a local HTTP server
// whose targets answer after a set latency, and some of which are down or
// flaky, all decided by a seed. /stats reports the peak number of
// requests in flight. The benchmark never touches the public internet.
package farm

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config sets the farm's targets.
type Config struct {
	N          int           // number of targets, /t/0 to /t/N-1
	MinLatency time.Duration // each target's latency is fixed, uniform in [MinLatency, MaxLatency]
	MaxLatency time.Duration
	Fail       float64 // fraction of targets that are always down
	Flaky      float64 // fraction that drop the first request after each reset, then answer 200
	Seed       uint64
}

// Behaviour is how a target answers.
type Behaviour int

const (
	OK          Behaviour = iota // 200 "ok"
	Unavailable                  // 503, always
	Drop                         // cuts its response short and closes the connection, always
	Flaky                        // drops its first request after a reset, then OK
)

// Target is one target's latency and behaviour.
type Target struct {
	Latency   time.Duration
	Behaviour Behaviour
}

// Plan decides every target from c alone. round(N*Fail) targets are down
// (half answer 503, half drop the connection) and round(N*Flaky) are
// flaky, chosen by a permutation seeded with c.Seed, so the same Config
// gives the same farm.
func Plan(c Config) ([]Target, error) {
	if c.N < 1 || c.N > 1_000_000 {
		return nil, fmt.Errorf("farm: n must be 1 to 1000000, got %d", c.N)
	}
	if c.MinLatency < 0 || c.MaxLatency < c.MinLatency {
		return nil, fmt.Errorf("farm: want 0 <= min latency <= max latency, got %s-%s", c.MinLatency, c.MaxLatency)
	}
	down := int(math.Round(float64(c.N) * c.Fail))
	flaky := int(math.Round(float64(c.N) * c.Flaky))
	if c.Fail < 0 || c.Flaky < 0 || down+flaky > c.N {
		return nil, fmt.Errorf("farm: want fail >= 0, flaky >= 0 and fail + flaky <= 1, got %g and %g", c.Fail, c.Flaky)
	}
	rng := rand.New(rand.NewPCG(c.Seed, c.Seed^0x9e3779b97f4a7c15))
	ts := make([]Target, c.N)
	span := int64(c.MaxLatency - c.MinLatency)
	for i := range ts {
		ts[i].Latency = c.MinLatency + time.Duration(rng.Int64N(span+1))
	}
	for k, i := range rng.Perm(c.N) {
		switch {
		case k < down/2:
			ts[i].Behaviour = Unavailable
		case k < down:
			ts[i].Behaviour = Drop
		case k < down+flaky:
			ts[i].Behaviour = Flaky
		}
	}
	return ts, nil
}

// Stats is what /stats reports since the last reset.
type Stats struct {
	InFlight  int `json:"in_flight"`  // target requests being served now
	Peak      int `json:"peak"`       // the most target requests in flight at once
	Total     int `json:"total"`      // target requests received
	Sink      int `json:"sink"`       // requests to /sink, the escape probe's drop box
	SinkBytes int `json:"sink_bytes"` // body bytes they carried
}

// Farm serves the targets. It is an http.Handler.
type Farm struct {
	targets []Target
	mu      sync.Mutex
	stats   Stats
	hits    []int // requests per target since the last reset
}

// New plans the farm from c.
func New(c Config) (*Farm, error) {
	ts, err := Plan(c)
	if err != nil {
		return nil, err
	}
	return &Farm{targets: ts, hits: make([]int, len(ts))}, nil
}

// Targets is the farm's target list as Plan made it.
func (f *Farm) Targets() []Target { return append([]Target(nil), f.targets...) }

// Stats returns the counters since the last reset.
func (f *Farm) Stats() Stats {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stats
}

// Reset zeroes the counters and makes flaky targets drop their next
// request again, so every benchmark run sees the same farm. Requests in
// flight stay counted.
func (f *Farm) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stats = Stats{InFlight: f.stats.InFlight, Peak: f.stats.InFlight}
	clear(f.hits)
}

// ServeHTTP serves /t/{i}, /stats, /reset (POST) and /sink.
func (f *Farm) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/t/"):
		f.target(w, r)
	case r.URL.Path == "/stats":
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(f.Stats())
	case r.URL.Path == "/reset" && r.Method == http.MethodPost:
		f.Reset()
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/sink":
		n, _ := io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
		f.mu.Lock()
		f.stats.Sink++
		f.stats.SinkBytes += int(n)
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

// target serves /t/{i}. The request leaves the in-flight count before
// any of the response is written, so a client that has its answer and
// sends its next request is never counted twice.
func (f *Farm) target(w http.ResponseWriter, r *http.Request) {
	i, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/t/"))
	if err != nil || i < 0 || i >= len(f.targets) {
		http.NotFound(w, r)
		return
	}
	t := f.targets[i]
	f.mu.Lock()
	f.stats.Total++
	f.stats.InFlight++
	f.stats.Peak = max(f.stats.Peak, f.stats.InFlight)
	f.hits[i]++
	first := f.hits[i] == 1
	f.mu.Unlock()
	left := false
	leave := func() {
		if !left {
			left = true
			f.mu.Lock()
			f.stats.InFlight--
			f.mu.Unlock()
		}
	}
	defer leave()
	timer := time.NewTimer(t.Latency)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-r.Context().Done():
		return
	}
	switch {
	case t.Behaviour == Unavailable:
		leave()
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	case t.Behaviour == Drop || (t.Behaviour == Flaky && first):
		leave()
		drop(w)
	default:
		leave()
		io.WriteString(w, "ok\n")
	}
}

// dropBody is what a dropped response sends of the dropBodyLen bytes its
// header promises.
const (
	dropBody    = "dropped"
	dropBodyLen = 64
)

// drop sends a 200 head that promises dropBodyLen bytes, part of the body,
// then closes the connection: the client sees its body cut short, an
// error. Closing before any response would be simpler, but net/http's
// client re-sends a GET by itself when a reused keep-alive connection
// closes before the response starts. Go's checker would then pay two
// latencies, and the farm count two requests, for each dropped attempt,
// while Arv, which opens a new connection per http_get, pays one. Once
// a response has started the client never re-sends, so both checkers see
// one failed attempt per request. The status is 200 so that a checker
// which ignores a body error reports the target up, and is caught.
func drop(w http.ResponseWriter) {
	w.Header().Set("Content-Length", strconv.Itoa(dropBodyLen))
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, dropBody)
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
	if hj, ok := w.(http.Hijacker); ok {
		if conn, _, err := hj.Hijack(); err == nil {
			conn.Close()
			return
		}
	}
	// No hijacker: return short of the promised length, and net/http
	// closes the connection after the reply.
}
