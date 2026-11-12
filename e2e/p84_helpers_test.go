package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Phase 8.4 through the real binary: the same program on both engines.
// The default build runs programs as compiled code; a build with the
// arv_treewalk tag runs them on the tree-walker, the reference. These
// tests build both (reusing arvBin for the engine it already is) and
// compare what each prints, its exit code and its errors.

// The two engines, by the name the tests use.
const (
	walkEngine     = "walk"
	compiledEngine = "compiled"
)

var engines = []string{walkEngine, compiledEngine}

var (
	engineOnce sync.Once
	engineBin  map[string]string // engine -> binary
	engineErr  error
	engineDir  string // removed by removeEngineBins
)

// engineBinary is the arv binary that runs programs on engine e, built on
// first use with the suite's flags (-race under ARV_E2E_RACE).
func engineBinary(t *testing.T, e string) string {
	t.Helper()
	engineOnce.Do(buildEngineBins)
	if engineErr != nil {
		t.Fatal(engineErr)
	}
	return engineBin[e]
}

func buildEngineBins() {
	engineBin = map[string]string{}
	switch os.Getenv("ARV_E2E_TAGS") {
	case "":
		engineBin[compiledEngine] = arvBin
	case "arv_treewalk":
		engineBin[walkEngine] = arvBin
	}
	dir, err := os.MkdirTemp("", "arv-e2e-p84-")
	if err != nil {
		engineErr = err
		return
	}
	engineDir = dir
	var race []string
	if os.Getenv("ARV_E2E_RACE") != "" {
		race = []string{"-race"}
	}
	for _, e := range engines {
		if engineBin[e] != "" {
			continue
		}
		bin := filepath.Join(dir, "arv-"+e)
		args := append([]string{"build", "-o", bin}, race...)
		if e == walkEngine {
			args = append(args, "-tags", "arv_treewalk")
		}
		build := exec.Command("go", append(args, cmdArv)...)
		if out, err := build.CombinedOutput(); err != nil {
			engineErr = fmt.Errorf("building the %s binary (%s): %v\n%s", e, strings.Join(build.Args, " "), err, out)
			return
		}
		engineBin[e] = bin
	}
}

func removeEngineBins() {
	if engineDir != "" {
		os.RemoveAll(engineDir)
	}
}

// onEngine is s run by engine e's binary.
func onEngine(t *testing.T, e string, s spec) result {
	t.Helper()
	s.bin = engineBinary(t, e)
	return run(t, s)
}

// sameRun fails unless the two runs printed the same, to stdout and to
// stderr, and exited with the same code. what names the comparison.
func sameRun(t *testing.T, what string, walk, comp result) {
	t.Helper()
	if walk.code != comp.code || walk.stdout != comp.stdout || walk.stderr != comp.stderr {
		t.Fatalf("%s: the engines differ\n=== tree-walker ===\n%s\n=== compiled ===\n%s", what, walk, comp)
	}
}

// bothEngines runs s on each engine and fails unless they agree; it
// returns the (common) result.
func bothEngines(t *testing.T, s spec) result {
	t.Helper()
	w, c := onEngine(t, walkEngine, s), onEngine(t, compiledEngine, s)
	sameRun(t, strings.Join(s.args, " "), w, c)
	return c
}

// writeProg writes src, {{SERVER}} and {{ID}} expanded, to a fresh file
// and returns its path. Both engines then run the same file, so the
// paths in their errors are the same.
func writeProg(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prog.arv")
	mustWrite(t, path, expand(t, src, nil))
	return path
}
