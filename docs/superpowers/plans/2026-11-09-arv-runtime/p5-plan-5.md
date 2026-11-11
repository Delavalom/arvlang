# Phase 5, task p5.5: farm reports

> **For agentic workers:** this file is one task, p5.5. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p5.1 to p5.4 are committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3 and 4 merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`), and `bench/uptime-go`'s own `go vet` and `go test -race`. Design: `12-phase5-gate.md`. Deviations from it, and why: `p5-plan-1.md`. **Do not measure or write the gate verdict**: a separate step does that.

### Task p5.5: expected report, strict parse, compare

Both uptime checkers write `report.json` as a JSON array, in target order, of `{"url", "status", "up", "latency_ms"}` (deviation 5 in `p5-plan-1.md`). This task adds the farm's side of correctness:
- `Expected(base)` is the report a correct checker writes, with every latency 0: OK and flaky targets are 200 and up, 503 targets are down with status 503, and dropped targets are down with status 0.
- `TargetList(base)` is `targets.txt`, one URL per line.
- `TooManyDown` is the exit rule: more than 5% down.
- `ParseReport` is strict: exactly the four fields, each present, `latency_ms` not negative, nothing after the array.
- `Compare` reports the first difference, ignoring latency.

**Files:**
- Create: `bench/farm/report.go`
- Create (test): `bench/farm/report_test.go`

- [ ] **Step 1: Write the failing test**

Create `bench/farm/report_test.go` (it uses `gate` from `farm_test.go`):

```go
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
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./bench/farm/`
Expected: FAIL, build errors such as `f.Expected undefined`, `undefined: Result` and `undefined: TooManyDown`.

- [ ] **Step 3: Implement**

Create `bench/farm/report.go`:

```go
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
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race ./bench/farm/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add bench/farm/report.go bench/farm/report_test.go
git commit -m "feat(bench): farm reports: expected, parse, compare

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
