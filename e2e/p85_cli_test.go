package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// checkDistBinary checks that bin, a binary built some other way than the
// suite's (go install, make build), behaves as the suite's: it runs a
// program to the same output, holds no authority it was not granted (I1)
// and keeps the environment's secret out of its error, and refuses
// arguments after version.
func checkDistBinary(t *testing.T, bin string) {
	t.Helper()
	got := run(t, spec{script: "basics.arv", bin: bin})
	want := run(t, spec{script: "basics.arv"})
	if got.code != 0 || got.stdout != want.stdout || got.stderr != "" {
		t.Fatalf("%s differs from the suite's binary\n=== %[1]s ===\n%s\n=== suite ===\n%s", bin, got, want)
	}
	const secret = "sk-dist-binary-secret"
	check(t, spec{
		script: "getenv.arv", vars: map[string]string{"NAME": "ARV_E2E_TOKEN"}, bin: bin,
		env:       []string{"ARV_E2E_TOKEN=" + secret, "ANTHROPIC_API_KEY=" + secret},
		code:      1,
		stderrHas: []string{"capability denied", `env("ARV_E2E_TOKEN")`, "holds: []"},
		stderrNot: []string{secret},
	})
	check(t, spec{args: []string{"version", "extra"}, bin: bin, code: 2, stderrHas: []string{"arv version"}})
}

// arv version reads nothing from the program's world: whatever the
// environment holds (a key, HOME, variables named like its output), it
// prints the same three lines, prints no secret and needs no grant; it
// refuses anything after it, and the usage text lists it.
func TestP85VersionCommand(t *testing.T) {
	t.Parallel()
	const secret = "sk-version-secret"
	plain := run(t, spec{args: []string{"version"}})
	ver, commit := version(t, arvBin) // the three-line form
	if want := "arv " + ver + "\ncommit " + commit + "\n"; !strings.HasPrefix(plain.stdout, want) {
		t.Fatalf("stdout %q does not start %q", plain.stdout, want)
	}
	runAll(t, []spec{
		{
			name: "environment ignored", args: []string{"version"},
			env:    []string{"ANTHROPIC_API_KEY=" + secret, "HOME=/nonexistent", "ARV_VERSION=" + secret, "ARV_COMMIT=" + secret},
			stdout: plain.stdout, stdoutNot: []string{secret},
		},
		{
			name: "grant flags are not version's", args: []string{"version", "--allow", "env=HOME"},
			code: 2, stderrHas: []string{"arv version"},
		},
		{name: "extra argument", args: []string{"version", "extra"}, code: 2, stderrHas: []string{"arv version"}},
		{name: "help flag", args: []string{"version", "--help"}, code: 2, stderrHas: []string{"arv version"}},
		{
			name: "unknown command lists version", args: []string{"versions"},
			code: 2, stderrHas: []string{"\n  arv version "},
		},
		{
			// A directory the binary cannot list is no obstacle: version
			// reads no file.
			name: "unreadable working directory", args: []string{"version"},
			dir: unreadableDir(t), stdout: plain.stdout,
		},
	})
}

// unreadableDir is a fresh directory with no permissions; the test's
// cleanup restores them so it can be removed.
func unreadableDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(dir, 0o711); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	return dir
}

// readmeMakeLine is one line of README's development block, such as
// "make build   # go build -o bin/arv ./cmd/arv (BIN=dir ...)".
var readmeMakeLine = regexp.MustCompile(`^make ([a-z0-9-]+) +# (go [^(,]*)`)

// Every command README says a make target runs is what make runs: its
// words appear, in order, in the target's recipe (which may add flags, as
// make build adds the commit). Moving the command again without updating
// README, or the Makefile without README, fails here.
func TestP85ReadmeMakeTargets(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		m := readmeMakeLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		target, words := m[1], strings.Fields(m[2])
		seen[target] = true
		recipe := strings.Fields(makeIn(t, "..", nil, "--dry-run", target))
		i := 0
		for _, w := range recipe {
			if i < len(words) && w == words[i] {
				i++
			}
		}
		if i != len(words) {
			t.Errorf("README: make %s runs %q, but its recipe is %q", target, m[2], strings.Join(recipe, " "))
		}
	}
	for _, target := range []string{"build", "install", "test"} {
		if !seen[target] {
			t.Errorf("README documents no command for make %s", target)
		}
	}
}
