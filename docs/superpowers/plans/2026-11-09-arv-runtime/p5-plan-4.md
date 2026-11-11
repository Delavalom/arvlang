# Phase 5, task p5.4: the target farm

> **For agentic workers:** this file is one task, p5.4. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p5.1 to p5.3 are committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3 and 4 merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`), and `bench/uptime-go`'s own `go vet` and `go test -race`. Design: `12-phase5-gate.md`. Deviations from it, and why: `p5-plan-1.md`. **Do not measure or write the gate verdict**: a separate step does that.

### Task p5.4: `bench/farm`: plan, handler and stats

The farm is the benchmark's whole outside world: a local HTTP server with N targets at `/t/0` to `/t/N-1`. `Plan` decides each target's latency (uniform in `[MinLatency, MaxLatency]`) and behaviour from the config alone, using a PCG generator seeded with `Seed`: `round(N*Fail)` targets are down (half answer 503, half drop the connection), `round(N*Flaky)` are flaky (they drop their first request after each reset, then answer 200), and the rest answer 200 `ok`. The same config always gives the same farm.

`/stats` reports, since the last `POST /reset`, the requests in flight, the peak in flight, the total, and what reached `/sink` (the escape probe's drop box). A request leaves the in-flight count before any of its response is written, or before its connection is dropped: a checker that has its answer and sends its next request at once is never counted twice. A request whose client gives up leaves as soon as the server sees its context end. The tests order events by polling `/stats`, never by sleeping.

It is a library (package `farm`); p5.6 adds the binary. See deviation 2 in `p5-plan-1.md`.

**Files:**
- Create: `bench/farm/farm.go`
- Create (test): `bench/farm/farm_test.go`

- [ ] **Step 1: Write the failing test**

Create `bench/farm/farm_test.go`:

```go
package farm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

var gate = Config{N: 200, MinLatency: 50 * time.Millisecond, MaxLatency: 400 * time.Millisecond, Fail: 0.1, Flaky: 0.05, Seed: 1}

func TestPlanIsDeterministic(t *testing.T) {
	a, err := Plan(gate)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Plan(gate)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("the same config planned two different farms")
	}
	other := gate
	other.Seed = 2
	c, _ := Plan(other)
	if reflect.DeepEqual(a, c) {
		t.Fatal("seeds 1 and 2 planned the same farm")
	}
}

// The gate's farm: 200 targets, 10% down (half 503, half dropped), 5%
// flaky, latencies uniform in 50-400 ms.
func TestPlanCountsAndLatencies(t *testing.T) {
	ts, _ := Plan(gate)
	count := map[Behaviour]int{}
	var sum time.Duration
	for _, tg := range ts {
		count[tg.Behaviour]++
		if tg.Latency < gate.MinLatency || tg.Latency > gate.MaxLatency {
			t.Errorf("latency %s outside %s-%s", tg.Latency, gate.MinLatency, gate.MaxLatency)
		}
		sum += tg.Latency
	}
	want := map[Behaviour]int{OK: 170, Unavailable: 10, Drop: 10, Flaky: 10}
	if !reflect.DeepEqual(count, want) {
		t.Errorf("behaviours %v, want %v", count, want)
	}
	// Uniform on 50-400 ms has mean 225 ms; 200 samples put the mean
	// within about 7 ms of it per standard deviation.
	if mean := sum / time.Duration(len(ts)); mean < 195*time.Millisecond || mean > 255*time.Millisecond {
		t.Errorf("mean latency %s, want about 225ms", mean)
	}
}

func TestPlanRejectsBadConfigs(t *testing.T) {
	for _, c := range []Config{
		{N: 0},
		{N: 10, MinLatency: 2, MaxLatency: 1},
		{N: 10, MinLatency: -1},
		{N: 10, Fail: -0.1},
		{N: 10, Fail: 0.6, Flaky: 0.5},
	} {
		if _, err := Plan(c); err == nil {
			t.Errorf("Plan(%+v) succeeded", c)
		}
	}
}

// start serves a farm of 40 fast targets: 20 down, 10 flaky.
func start(t *testing.T) (*Farm, *httptest.Server) {
	t.Helper()
	f, err := New(Config{N: 40, MaxLatency: time.Millisecond, Fail: 0.5, Flaky: 0.25, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

// find is the index of the first target that behaves as b.
func find(t *testing.T, f *Farm, b Behaviour) int {
	t.Helper()
	for i, tg := range f.Targets() {
		if tg.Behaviour == b {
			return i
		}
	}
	t.Fatalf("no target behaves as %d", b)
	return -1
}

// get fetches path on a new connection, so net/http never retries it.
func get(srv *httptest.Server, path string) (int, string, error) {
	c := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	resp, err := c.Get(srv.URL + path)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), err
}

func stats(t *testing.T, srv *httptest.Server) Stats {
	t.Helper()
	_, body, err := get(srv, "/stats")
	if err != nil {
		t.Fatal(err)
	}
	var s Stats
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		t.Fatalf("stats %q: %v", body, err)
	}
	return s
}

func TestTargetBehaviours(t *testing.T) {
	f, srv := start(t)
	path := func(b Behaviour) string { return "/t/" + strconv.Itoa(find(t, f, b)) }
	if code, body, err := get(srv, path(OK)); err != nil || code != 200 || body != "ok\n" {
		t.Errorf("OK target: %d %q %v", code, body, err)
	}
	if code, _, err := get(srv, path(Unavailable)); err != nil || code != 503 {
		t.Errorf("unavailable target: %d %v", code, err)
	}
	for i := 0; i < 3; i++ {
		if _, _, err := get(srv, path(Drop)); err == nil {
			t.Errorf("drop target answered on try %d", i)
		}
	}
	if _, _, err := get(srv, path(Flaky)); err == nil {
		t.Error("flaky target answered its first request")
	}
	if code, _, err := get(srv, path(Flaky)); err != nil || code != 200 {
		t.Errorf("flaky target, second request: %d %v", code, err)
	}
	f.Reset()
	if _, _, err := get(srv, path(Flaky)); err == nil {
		t.Error("flaky target answered its first request after a reset")
	}
	for _, p := range []string{"/t/40", "/t/-1", "/t/x", "/nope"} {
		if code, _, _ := get(srv, p); code != 404 {
			t.Errorf("%s: %d, want 404", p, code)
		}
	}
}

// A client that has its answer never finds its request still counted:
// the farm leaves the in-flight count before it writes the response.
func TestInFlightLeavesBeforeTheResponse(t *testing.T) {
	f, srv := start(t)
	for _, b := range []Behaviour{OK, Unavailable, Drop} {
		get(srv, "/t/"+strconv.Itoa(find(t, f, b)))
		if s := stats(t, srv); s.InFlight != 0 {
			t.Errorf("behaviour %d: %d in flight after the client's answer", b, s.InFlight)
		}
	}
	if s := stats(t, srv); s.Total != 3 || s.Peak != 1 {
		t.Errorf("stats %+v, want total 3, peak 1", s)
	}
}

// Requests that overlap are counted at once in Peak, and a client that
// gives up leaves the count. Ordering comes from polling /stats, not from
// sleeping.
func TestPeakCountsOverlappingRequests(t *testing.T) {
	f, err := New(Config{N: 8, MinLatency: time.Hour, MaxLatency: time.Hour, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(f)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	for i := 0; i < 5; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/t/"+strconv.Itoa(i), nil)
			if resp, err := http.DefaultClient.Do(req); err == nil {
				resp.Body.Close()
			}
		}()
	}
	waitFor(t, func() bool { return f.Stats().InFlight == 5 })
	if s := f.Stats(); s.Peak != 5 || s.Total != 5 {
		t.Errorf("stats %+v, want peak 5, total 5", s)
	}
	cancel()
	for i := 0; i < 5; i++ {
		<-done
	}
	waitFor(t, func() bool { return f.Stats().InFlight == 0 })
	f.Reset()
	if s := f.Stats(); s != (Stats{}) {
		t.Errorf("after reset: %+v", s)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(60 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 60s")
		}
	}
}

// /sink counts what reaches it, apart from the targets.
func TestSink(t *testing.T) {
	_, srv := start(t)
	resp, err := http.Post(srv.URL+"/sink", "text/plain", strings.NewReader("secret"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if s := stats(t, srv); s.Sink != 1 || s.SinkBytes != 6 || s.Total != 0 {
		t.Errorf("stats %+v, want sink 1, sink_bytes 6, total 0", s)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./bench/farm/`
Expected: FAIL, build errors such as `undefined: Config`, `undefined: Plan` and `undefined: Farm`.

- [ ] **Step 3: Implement**

Create `bench/farm/farm.go`:

```go
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
	Drop                         // closes the connection without a response, always
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
		hj, ok := w.(http.Hijacker)
		if !ok {
			leave()
			http.Error(w, "cannot drop: no hijacker", http.StatusInternalServerError)
			return
		}
		conn, _, err := hj.Hijack()
		leave()
		if err == nil {
			conn.Close()
		}
	default:
		leave()
		io.WriteString(w, "ok\n")
	}
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race ./bench/farm/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add bench/farm/farm.go bench/farm/farm_test.go
git commit -m "feat(bench): target farm for the Phase 5 gate

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
