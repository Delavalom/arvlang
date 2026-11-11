package main

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/bench/farm"
)

// examples/uptime-ergonomic.arv is the gate scenario in the Phase 5E
// syntax. Against the same farm it writes the same report, prints the
// same line and makes the same requests as examples/uptime.arv; only its
// error message is new.
func TestUptimeErgonomicMatchesUptime(t *testing.T) {
	plain, err := os.ReadFile("examples/uptime.arv")
	if err != nil {
		t.Fatal(err)
	}
	ergonomic, err := os.ReadFile("examples/uptime-ergonomic.arv")
	if err != nil {
		t.Fatal(err)
	}
	atFive := fastGate
	atFive.Fail = 0.05
	for _, tt := range []struct {
		name string
		c    farm.Config
		msg  string // the ergonomic version's error; "" for none
	}{
		{"gate", fastGate, "20 of 200 targets are down, more than 5%"},
		{"at 5%", atFive, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			want := runUptimeSrc(t, plain, tt.c)
			got := runUptimeSrc(t, ergonomic, tt.c)
			if err := farm.Compare(got.got, got.want); err != nil {
				t.Fatalf("report against the farm: %v", err)
			}
			if a, b := withoutHost(got.got), withoutHost(want.got); !reflect.DeepEqual(a, b) {
				t.Fatalf("report %v, uptime.arv's %v", a, b)
			}
			if got.out != want.out {
				t.Errorf("printed %q, uptime.arv printed %q", got.out, want.out)
			}
			if got.stats.Total != want.stats.Total {
				t.Errorf("farm saw %d requests, %d for uptime.arv", got.stats.Total, want.stats.Total)
			}
			var ae *arv.Error
			switch {
			case tt.msg == "" && got.err != nil:
				t.Errorf("run error %v, want none", got.err)
			case tt.msg != "" && (!errors.As(got.err, &ae) || ae.Kind != "user" || ae.Msg != tt.msg):
				t.Errorf("run error %v, want the user error %q", got.err, tt.msg)
			}
		})
	}
}

// withoutHost is rs with each URL cut to its path and no latency: each
// run has its own farm, on its own port.
func withoutHost(rs []farm.Result) []farm.Result {
	out := make([]farm.Result, len(rs))
	for i, r := range rs {
		r.URL = r.URL[strings.Index(r.URL, "/t/"):]
		r.LatencyMS = 0
		out[i] = r
	}
	return out
}

// The new syntax makes the script shorter, not longer.
func TestUptimeErgonomicIsShort(t *testing.T) {
	count := func(path string) int {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, line := range strings.Split(string(src), "\n") {
			if l := strings.TrimSpace(line); l != "" && !strings.HasPrefix(l, "//") {
				n++
			}
		}
		return n
	}
	plain, ergonomic := count("examples/uptime.arv"), count("examples/uptime-ergonomic.arv")
	if ergonomic > plain {
		t.Errorf("%d code lines, uptime.arv has %d", ergonomic, plain)
	}
	t.Logf("%d code lines (uptime.arv: %d)", ergonomic, plain)
}
