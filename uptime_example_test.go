package main

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/bench/farm"
)

// uptimeRun is one run of examples/uptime.arv against an in-process farm.
type uptimeRun struct {
	out   string        // what the script printed
	err   error         // what arv.Run returned
	stats farm.Stats    // the farm's counters afterwards
	want  []farm.Result // the farm's expected report
	got   []farm.Result // out/report.json
}

// runUptime runs the example in a temp dir holding targets.txt, with
// exactly the grants its header names, against a farm planned from c.
func runUptime(t *testing.T, c farm.Config) uptimeRun {
	t.Helper()
	src, err := os.ReadFile("examples/uptime.arv")
	if err != nil {
		t.Fatal(err)
	}
	return runUptimeSrc(t, src, c)
}

// runUptimeSrc is runUptime for the script src. It changes the working
// directory until the test ends, so read every script before the first
// call.
func runUptimeSrc(t *testing.T, src []byte, c farm.Config) uptimeRun {
	t.Helper()
	f, err := farm.New(c)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(f)
	defer srv.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "targets.txt"), []byte(f.TargetList(srv.URL)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	var caps []runtime.Capability
	for _, g := range []string{"fs_read=targets.txt", "fs_write=out", "net=" + strings.TrimPrefix(srv.URL, "http://")} {
		grant, err := runtime.Parse(g, dir)
		if err != nil {
			t.Fatal(err)
		}
		caps = append(caps, grant)
	}
	var out bytes.Buffer
	r := uptimeRun{want: f.Expected(srv.URL)}
	_, r.err = arv.Run(string(src), arv.Options{Caps: caps, Out: &out, Limits: runtime.Limits{Timeout: 5 * time.Minute}})
	r.out, r.stats = out.String(), f.Stats()
	data, err := os.ReadFile(filepath.Join(dir, "out", "report.json"))
	if err != nil {
		t.Fatalf("no report (run error %v): %v", r.err, err)
	}
	if r.got, err = farm.ParseReport(data); err != nil {
		t.Fatal(err)
	}
	return r
}

// fastGate is the gate's farm with latencies short enough for a test.
var fastGate = farm.Config{N: 200, MinLatency: 5 * time.Millisecond, MaxLatency: 40 * time.Millisecond, Fail: 0.1, Flaky: 0.05, Seed: 1}

// The gate scenario: 20 of 200 down is more than 5%, so the run fails
// with the script's own error, after writing the full report. Flaky
// targets are up after one restart; dropped ones are retried twice.
func TestUptimeExampleReportsTheFarm(t *testing.T) {
	r := runUptime(t, fastGate)
	if err := farm.Compare(r.got, r.want); err != nil {
		t.Fatal(err)
	}
	var ae *arv.Error
	if !errors.As(r.err, &ae) || ae.Kind != "user" || ae.Msg != "more than 5% of the targets are down" {
		t.Fatalf("run error %v, want the user error", r.err)
	}
	if r.out != `{"down":20,"total":200}`+"\n" {
		t.Errorf("output %q", r.out)
	}
	// 200 first attempts, a second for each of the 10 flaky targets, and
	// two more for each of the 10 that drop every connection.
	if r.stats.Total != 230 {
		t.Errorf("farm saw %d requests, want 230", r.stats.Total)
	}
	if r.stats.Peak < 1 || r.stats.Peak > 20 {
		t.Errorf("peak in flight %d, want 1 to 20", r.stats.Peak)
	}
	t.Logf("peak in flight: %d", r.stats.Peak)
}

// At exactly 5% down the run succeeds.
func TestUptimeExampleAtFivePercentSucceeds(t *testing.T) {
	c := fastGate
	c.Fail = 0.05
	r := runUptime(t, c)
	if r.err != nil {
		t.Fatalf("run error %v", r.err)
	}
	if err := farm.Compare(r.got, r.want); err != nil {
		t.Fatal(err)
	}
	if r.out != `{"down":10,"total":200}`+"\n" {
		t.Errorf("output %q", r.out)
	}
}

// No goroutine of the run survives it.
func TestUptimeExampleLeaksNothing(t *testing.T) {
	c := fastGate
	c.N = 40
	before := goruntime.NumGoroutine()
	runUptime(t, c)
	for deadline := time.Now().Add(30 * time.Second); goruntime.NumGoroutine() > before; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			t.Fatalf("%d goroutines, %d before:\n%s", goruntime.NumGoroutine(), before, buf[:goruntime.Stack(buf, true)])
		}
	}
}

// The gate's concision target: at most 40 lines that are not blank or
// comments.
func TestUptimeExampleIsShort(t *testing.T) {
	src, err := os.ReadFile("examples/uptime.arv")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(string(src), "\n") {
		if l := strings.TrimSpace(line); l != "" && !strings.HasPrefix(l, "//") {
			n++
		}
	}
	if n > 40 {
		t.Errorf("%d code lines, want at most 40", n)
	}
	t.Logf("%d code lines", n)
}
