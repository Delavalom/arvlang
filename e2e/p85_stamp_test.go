package e2e

import (
	"debug/buildinfo"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// checkoutHead is the commit HEAD names in the checkout the suite runs
// in, or "" without git or outside a git checkout.
func checkoutHead() string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = ".."
	cmd.Env = cleanEnv()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// checkStamp fails t when the go command stamped bin with a commit other
// than the checkout's HEAD. It stamps nothing in a git worktree.
func checkStamp(t *testing.T, bin string) {
	t.Helper()
	info, err := buildinfo.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			if head := checkoutHead(); s.Value != head {
				t.Fatalf("%s is stamped with commit %s, want the checkout's HEAD %q", filepath.Base(bin), s.Value, head)
			}
		}
	}
}

// An engine binary (built as the Phase 8.4 tests build it) is stamped, if
// at all, with the checkout's HEAD.
func checkEngineBuild(t *testing.T) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "arv-"+walkEngine)
	if out, err := engineBuild(bin, walkEngine, nil).CombinedOutput(); err != nil {
		t.Fatalf("building the %s binary: %v\n%s", walkEngine, err, out)
	}
	checkStamp(t, bin)
}

// Without git on PATH the tests that need git skip, as the ones that need
// make do without make: a source export built where only go is installed
// must still pass make test. Not parallel: t.Setenv changes PATH for the
// whole process, and the parallel tests are paused meanwhile.
func TestP85GitTestsSkipWithoutGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, c := range []struct {
		name string
		f    func(*testing.T)
	}{
		{"GitFixturesIgnoreGitEnv", TestP85GitFixturesIgnoreGitEnv},
		{"VersionFromGit", checkVersionFromGit},
		{"MakeBuildInWorktree", checkMakeBuildInWorktree},
	} {
		skipped := false
		t.Run(c.name, func(t *testing.T) {
			defer func() { skipped = t.Skipped() }()
			c.f(t)
		})
		if !skipped {
			t.Errorf("%s did not skip without git on PATH", c.name)
		}
	}
}
