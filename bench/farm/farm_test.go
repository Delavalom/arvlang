package farm

import (
	"context"
	"encoding/json"
	"io"
	"math"
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

// The widest latency span there is plans without a panic.
func TestPlanTakesTheWidestSpan(t *testing.T) {
	ts, err := Plan(Config{N: 3, MaxLatency: math.MaxInt64, Seed: 1})
	if err != nil || len(ts) != 3 {
		t.Fatalf("Plan: %v, %d targets", err, len(ts))
	}
}

func TestPlanRejectsBadConfigs(t *testing.T) {
	for _, c := range []Config{
		{N: 0},
		{N: 10, MinLatency: 2, MaxLatency: 1},
		{N: 10, MinLatency: -1},
		{N: 10, Fail: -0.1},
		{N: 10, Fail: 0.6, Flaky: 0.5},
		{N: 10, Fail: math.NaN()},
		{N: 10, Flaky: math.NaN()},
		{N: 10, Fail: math.Inf(1), Flaky: math.Inf(1)},
		{N: 10, Fail: 1e300, Flaky: 1e300},
		{N: 10, Fail: 1.5, Flaky: -1},
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
