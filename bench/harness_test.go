package bench

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// cpu.sh refuses to start when the load is above MAX_LOAD, and writes
// nothing (03-benchmarks.md §B0: the load rule).
func TestCPUScriptRefusesUnderLoad(t *testing.T) {
	out := t.TempDir()
	cmd := exec.Command("bash", "cpu.sh")
	cmd.Env = append(os.Environ(), "MAX_LOAD=-1", "BENCH_RESULTS="+out)
	b, err := cmd.CombinedOutput()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 3 {
		t.Fatalf("cpu.sh with MAX_LOAD=-1: err %v, want exit 3\n%s", err, b)
	}
	if !strings.Contains(string(b), "above MAX_LOAD") {
		t.Fatalf("output does not name the load rule:\n%s", b)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Fatalf("a refused run wrote %d entries", len(entries))
	}
}

// cpu.sh, end to end on one program and two runs: env.txt names the
// machine, the commit and both loads, both engines' raw output is kept,
// and benchstat's comparison is written.
func TestCPUScriptWritesResults(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two test binaries and runs benchmarks")
	}
	out := t.TempDir()
	cmd := exec.Command("bash", "cpu.sh")
	cmd.Env = append(os.Environ(), "COUNT=2", "BENCH_FILTER=calls$", "MAX_LOAD=100000", "BENCH_RESULTS="+out)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cpu.sh: %v\n%s", err, b)
	}
	dirs, _ := filepath.Glob(filepath.Join(out, "*-cpu"))
	if len(dirs) != 1 {
		t.Fatalf("result dirs %v", dirs)
	}
	env, err := os.ReadFile(filepath.Join(dirs[0], "env.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"date_utc=", "os=", "cpu=", "go=", "commit=", "load_before=", "load_after=", "valid=yes", "count=2"} {
		if !strings.Contains(string(env), key) {
			t.Errorf("env.txt lacks %q:\n%s", key, env)
		}
	}
	for _, name := range []string{"compiled.txt", "treewalk.txt"} {
		raw, err := os.ReadFile(filepath.Join(dirs[0], name))
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(raw), "BenchmarkSpeed/calls"); n != 2 {
			t.Errorf("%s has %d BenchmarkSpeed/calls lines, want 2:\n%s", name, n, raw)
		}
	}
	if st, err := os.Stat(filepath.Join(dirs[0], "benchstat.txt")); err != nil || st.Size() == 0 {
		t.Errorf("benchstat.txt missing or empty: %v", err)
	}
}
