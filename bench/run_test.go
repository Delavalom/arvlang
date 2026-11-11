// Package bench holds the Phase 5 gate benchmark: run.sh, the farm, the
// Go baseline (its own module, in uptime-go) and the escape probe.
package bench

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// run.sh, end to end, on a small fast farm: one run of each version,
// each with a correct report, at most 20 requests in flight, and both
// exiting 1 (4 of 40 targets down is more than 5%).
func TestRunScript(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binaries")
	}
	out := t.TempDir()
	cmd := exec.Command("bash", "run.sh")
	cmd.Env = append(os.Environ(), "RUNS=1", "FARM_FLAGS=-n 40 -latency 1ms-5ms", "BENCH_OUT="+out)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run.sh: %v\n%s", err, b)
	}
	csv, err := os.ReadFile(filepath.Join(out, "results.csv"))
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(string(csv)), "\n")
	if len(rows) != 3 || rows[0] != "version,run,wall_s,user_s,sys_s,cpu_s,max_rss_kb,peak_in_flight,requests,exit_code,report_ok" {
		t.Fatalf("results.csv:\n%s", csv)
	}
	seen := map[string]bool{}
	for _, row := range rows[1:] {
		f := strings.Split(row, ",")
		if len(f) != 11 {
			t.Fatalf("row %q", row)
		}
		seen[f[0]] = true
		wall, _ := strconv.ParseFloat(f[2], 64)
		rss, _ := strconv.Atoi(f[6])
		peak, _ := strconv.Atoi(f[7])
		if wall <= 0 || rss <= 0 || peak < 1 || peak > 20 || f[9] != "1" || f[10] != "yes" {
			t.Errorf("row %q: want wall and rss > 0, peak 1 to 20, exit 1, report yes", row)
		}
	}
	if !seen["arv"] || !seen["go"] {
		t.Errorf("versions %v, want arv and go", seen)
	}
	for _, want := range []string{"code lines: arv ", "arv: median wall ", "go: median wall ", "wrong reports 0", "versions agree: 2 reports and summaries, all the same as arv run 1"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("summary lacks %q:\n%s", want, b)
		}
	}
}
