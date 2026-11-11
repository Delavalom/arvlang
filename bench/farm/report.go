package farm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// Result is one entry of report.json, as both uptime checkers write it:
// the URL, its HTTP status (0 when no response came), whether it is up
// (a 2xx status) and the latency of the last attempt.
type Result struct {
	URL       string `json:"url"`
	Status    int    `json:"status"`
	Up        bool   `json:"up"`
	LatencyMS int64  `json:"latency_ms"`
}

// Expected is the report a correct checker writes for this farm, served
// at base (such as "http://127.0.0.1:8080"), with every latency 0.
func (f *Farm) Expected(base string) []Result {
	rs := make([]Result, len(f.targets))
	for i, t := range f.targets {
		rs[i] = Result{URL: base + "/t/" + strconv.Itoa(i)}
		switch t.Behaviour {
		case OK, Flaky:
			rs[i].Status, rs[i].Up = 200, true
		case Unavailable:
			rs[i].Status = 503
		}
	}
	return rs
}

// TargetList is the farm's targets.txt: one URL per line.
func (f *Farm) TargetList(base string) string {
	var b bytes.Buffer
	for i := range f.targets {
		fmt.Fprintf(&b, "%s/t/%d\n", base, i)
	}
	return b.String()
}

// TooManyDown says whether more than 5% of rs are down: the checkers'
// exit rule.
func TooManyDown(rs []Result) bool {
	down := 0
	for _, r := range rs {
		if !r.Up {
			down++
		}
	}
	return down*20 > len(rs)
}

// ParseReport reads a report.json strictly: a JSON array of objects with
// exactly the four fields of Result, each present.
func ParseReport(data []byte) ([]Result, error) {
	var raw []struct {
		URL       *string `json:"url"`
		Status    *int    `json:"status"`
		Up        *bool   `json:"up"`
		LatencyMS *int64  `json:"latency_ms"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&raw); err != nil {
		return nil, fmt.Errorf("report: %w", err)
	}
	if d.More() {
		return nil, fmt.Errorf("report: text after the array")
	}
	rs := make([]Result, len(raw))
	for i, r := range raw {
		if r.URL == nil || r.Status == nil || r.Up == nil || r.LatencyMS == nil {
			return nil, fmt.Errorf("report: entry %d lacks a field: want url, status, up and latency_ms", i)
		}
		if *r.LatencyMS < 0 {
			return nil, fmt.Errorf("report: entry %d: negative latency_ms %d", i, *r.LatencyMS)
		}
		rs[i] = Result{*r.URL, *r.Status, *r.Up, *r.LatencyMS}
	}
	return rs, nil
}

// Compare reports the first difference between two reports, ignoring
// latency, or nil if they are the same.
func Compare(a, b []Result) error {
	if len(a) != len(b) {
		return fmt.Errorf("reports differ: %d entries against %d", len(a), len(b))
	}
	for i := range a {
		x, y := a[i], b[i]
		x.LatencyMS, y.LatencyMS = 0, 0
		if x != y {
			return fmt.Errorf("reports differ at entry %d: %+v against %+v", i, x, y)
		}
	}
	return nil
}
