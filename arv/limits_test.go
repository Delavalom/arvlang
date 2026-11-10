package arv

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// A negative limit is an error, not "no limit": a host that computes what
// is left of a budget may go below zero, and must not get an unlimited run.
func TestNegativeLimitsAreRefused(t *testing.T) {
	src := `let f = fn(n) { if (n == 0) { [] } else { push(f(n - 1), "xxxxxxxxxxxxxxxx") } }; len(f(2000))`
	for _, tt := range []struct {
		limits runtime.Limits
		field  string
	}{
		{runtime.Limits{Timeout: -1}, "Timeout"},
		{runtime.Limits{Tasks: -1}, "Tasks"},
		{runtime.Limits{Steps: -1}, "Steps"},
		{runtime.Limits{AllocBytes: -1}, "AllocBytes"},
		{runtime.Limits{LiveTasks: -1}, "LiveTasks"},
	} {
		got, err := Run(src, Options{Limits: tt.limits})
		if err == nil || !strings.Contains(err.Error(), "invalid limits: "+tt.field+" is negative") {
			t.Errorf("%+v: got %v, %v; want an invalid-limits error naming %s", tt.limits, got, err, tt.field)
		}
	}
	if got, err := Run(src, Options{}); err != nil || got.Inspect() != "2000" {
		t.Errorf("zero limits: got %v, %v; want 2000", got, err)
	}
}
