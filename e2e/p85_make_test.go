package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// makeIn runs make with args in dir, with env added to the environment
// (less its GIT_* variables and the user's git configuration: see
// noUserGitEnv), and returns its output.
func makeIn(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	cmd := exec.Command("make", args...)
	cmd.Dir = dir
	cmd.Env = noUserGitEnv(append([]string{"MAKEFLAGS="}, env...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// The make targets the README documents run what it says: make build
// writes bin/arv from cmd/arv even when the environment exports BIN (only
// the command line moves it), make install installs cmd/arv, and make test
// and make e2e run the suites under the race detector.
func TestP85MakeRecipes(t *testing.T) {
	t.Parallel()
	env := []string{"BIN=" + filepath.Join(t.TempDir(), "elsewhere")}
	for _, c := range []struct{ target, want string }{
		{"build", " -o bin/arv ./cmd/arv"},
		{"install", "go install "},
		{"install", " ./cmd/arv"},
		{"test", "go test -race ./..."},
		{"e2e", "go test -race -count=1 ./e2e/"},
	} {
		// --dry-run prints the recipe without running it.
		if out := makeIn(t, "..", env, "--dry-run", c.target); !strings.Contains(out, c.want) {
			t.Errorf("make --dry-run %s does not run %q:\n%s", c.target, c.want, out)
		}
	}
}

// make build names the commit even in a git worktree, where the go
// command stamps nothing (it looks for a .git directory, and a worktree's
// .git is a file), and says "(modified)" once the tree has changes.
func TestP85MakeBuildInWorktree(t *testing.T) {
	t.Parallel()
	checkMakeBuildInWorktree(t)
}

func checkMakeBuildInWorktree(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo, head := freshRepo(t, "Makefile")
	wt := filepath.Join(t.TempDir(), "wt")
	git(t, repo, "worktree", "add", "-q", wt, "HEAD")
	if fi, err := os.Lstat(filepath.Join(wt, ".git")); err != nil || fi.IsDir() {
		t.Fatalf("the worktree's .git should be a file: %v", err)
	}
	bin := t.TempDir()
	makeIn(t, wt, nil, "-s", "build", "BIN="+bin)
	if _, commit := version(t, filepath.Join(bin, "arv")); commit != head {
		t.Fatalf("make build in a worktree: commit %q, want %s", commit, head)
	}
	if err := os.WriteFile(filepath.Join(wt, "notes.txt"), []byte("not committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeIn(t, wt, nil, "-s", "build", "BIN="+bin)
	if _, commit := version(t, filepath.Join(bin, "arv")); commit != head+" (modified)" {
		t.Fatalf("make build in a changed worktree: commit %q, want %s (modified)", commit, head)
	}
}
