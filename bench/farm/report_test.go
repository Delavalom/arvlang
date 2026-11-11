package farm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExpectedFollowsThePlan(t *testing.T) {
	f, err := New(gate)
	if err != nil {
		t.Fatal(err)
	}
	rs := f.Expected("http://h:1")
	down := 0
	for i, tg := range f.Targets() {
		r := rs[i]
		want := map[Behaviour]Result{
			OK:          {Status: 200, Up: true},
			Flaky:       {Status: 200, Up: true},
			Unavailable: {Status: 503},
			Drop:        {Status: 0},
		}[tg.Behaviour]
		if r.Status != want.Status || r.Up != want.Up || r.LatencyMS != 0 {
			t.Errorf("target %d (%d): %+v", i, tg.Behaviour, r)
		}
		if !r.Up {
			down++
		}
	}
	if rs[7].URL != "http://h:1/t/7" {
		t.Errorf("url %q", rs[7].URL)
	}
	// 20 of 200 down is more than 5%: the checkers must exit non-zero.
	if down != 20 || !TooManyDown(rs) {
		t.Errorf("%d down, TooManyDown %v", down, TooManyDown(rs))
	}
	lines := strings.Split(f.TargetList("http://h:1"), "\n")
	if len(lines) != 201 || lines[0] != "http://h:1/t/0" || lines[199] != "http://h:1/t/199" || lines[200] != "" {
		t.Errorf("target list: %d lines, %q ... %q", len(lines), lines[0], lines[199])
	}
}

func TestTooManyDownIsMoreThanFivePercent(t *testing.T) {
	rs := make([]Result, 200)
	for i := range rs {
		rs[i].Up = i >= 10
	}
	if TooManyDown(rs) {
		t.Error("10 of 200 down (5%) counted as too many")
	}
	rs[10].Up = false
	if !TooManyDown(rs) {
		t.Error("11 of 200 down not counted as too many")
	}
}

func TestParseReportAndCompare(t *testing.T) {
	want := []Result{{"http://h/t/0", 200, true, 12}, {"http://h/t/1", 0, false, 0}}
	data, _ := json.Marshal(want)
	got, err := ParseReport(data)
	if err != nil || Compare(got, want) != nil || got[0].LatencyMS != 12 {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	// Arv's json() writes keys in order; field order does not matter.
	arv := `[{"latency_ms":3,"status":200,"up":true,"url":"http://h/t/0"},{"latency_ms":9,"status":0,"up":false,"url":"http://h/t/1"}]`
	got, err = ParseReport([]byte(arv))
	if err != nil {
		t.Fatal(err)
	}
	if err := Compare(got, want); err != nil {
		t.Errorf("latency should not count: %v", err)
	}
	got[1].Status = 503
	if err := Compare(got, want); err == nil || !strings.Contains(err.Error(), "entry 1") {
		t.Errorf("Compare: %v, want a difference at entry 1", err)
	}
	if err := Compare(got[:1], want); err == nil {
		t.Error("Compare ignored a missing entry")
	}
}

func TestParseReportIsStrict(t *testing.T) {
	for _, bad := range []string{
		``,
		`{}`,
		`[{"url":"u","status":200,"up":true}]`,
		`[{"url":"u","status":200,"up":true,"latency_ms":1,"error":"x"}]`,
		`[{"url":"u","status":200,"up":true,"latency_ms":-1}]`,
		`[{"url":"u","status":"200","up":true,"latency_ms":1}]`,
		`[] []`,
	} {
		if _, err := ParseReport([]byte(bad)); err == nil {
			t.Errorf("ParseReport(%q) succeeded", bad)
		}
	}
}
