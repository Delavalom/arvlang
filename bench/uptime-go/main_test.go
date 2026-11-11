package main

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/bench/farm"
)

// fastGate is the gate's farm (200 targets, 10% down, 5% flaky) with
// latencies short enough for a test.
var fastGate = farm.Config{N: 200, MinLatency: 5 * time.Millisecond, MaxLatency: 40 * time.Millisecond, Fail: 0.1, Flaky: 0.05, Seed: 1}

// setup serves a farm and writes its targets.txt into a new dir with an
// empty out/.
func setup(t *testing.T, c farm.Config) (*farm.Farm, *httptest.Server, string) {
	t.Helper()
	f, err := farm.New(c)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "targets.txt"), []byte(f.TargetList(srv.URL)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	return f, srv, dir
}

func readReport(t *testing.T, path string) []farm.Result {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := farm.ParseReport(data)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func TestReportsTheFarm(t *testing.T) {
	f, srv, dir := setup(t, fastGate)
	var out bytes.Buffer
	err := run(context.Background(), filepath.Join(dir, "targets.txt"), filepath.Join(dir, "out", "report.json"), &out)
	if err == nil || err.Error() != "more than 5% of the targets are down" {
		t.Fatalf("run: %v", err)
	}
	if out.String() != `{"down":20,"total":200}`+"\n" {
		t.Errorf("output %q", out.String())
	}
	if err := farm.Compare(readReport(t, filepath.Join(dir, "out", "report.json")), f.Expected(srv.URL)); err != nil {
		t.Fatal(err)
	}
	if s := f.Stats(); s.Peak < 1 || s.Peak > 20 {
		t.Errorf("peak in flight %d, want 1 to 20", s.Peak)
	}
}

func TestAtFivePercentSucceeds(t *testing.T) {
	c := fastGate
	c.Fail = 0.05
	_, _, dir := setup(t, c)
	if err := run(context.Background(), filepath.Join(dir, "targets.txt"), filepath.Join(dir, "out", "report.json"), &bytes.Buffer{}); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// The gate's correctness criterion: against the same farm, the Arv
// example and this program write the same report, apart from latency,
// and print the same summary.
func TestSameReportAsTheArvExample(t *testing.T) {
	f, srv, dir := setup(t, fastGate)
	var goOut bytes.Buffer
	run(context.Background(), filepath.Join(dir, "targets.txt"), filepath.Join(dir, "out", "report.json"), &goOut)
	goReport := readReport(t, filepath.Join(dir, "out", "report.json"))

	src, err := os.ReadFile("../../examples/uptime.arv")
	if err != nil {
		t.Fatal(err)
	}
	f.Reset() // flaky targets drop their next request again
	os.Remove(filepath.Join(dir, "out", "report.json"))
	t.Chdir(dir)
	var caps []runtime.Capability
	for _, g := range []string{"fs_read=targets.txt", "fs_write=out", "net=" + strings.TrimPrefix(srv.URL, "http://")} {
		grant, err := runtime.Parse(g, dir)
		if err != nil {
			t.Fatal(err)
		}
		caps = append(caps, grant)
	}
	var arvOut bytes.Buffer
	if _, err := arv.Run(string(src), arv.Options{Caps: caps, Out: &arvOut}); err == nil {
		t.Error("the Arv example succeeded with 20 of 200 down")
	}
	if err := farm.Compare(readReport(t, filepath.Join(dir, "out", "report.json")), goReport); err != nil {
		t.Fatal(err)
	}
	if arvOut.String() != goOut.String() {
		t.Errorf("Arv printed %q, Go %q", arvOut.String(), goOut.String())
	}
}
