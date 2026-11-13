package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// goEnv is a go env variable as the go that builds the suite's binaries
// sees it.
func goEnv(t *testing.T, name string) string {
	t.Helper()
	out, err := exec.Command("go", "env", name).Output()
	if err != nil {
		t.Fatalf("go env %s: %v", name, err)
	}
	return strings.TrimSpace(string(out))
}

// version runs bin version and returns its lines: the version, the commit
// and the toolchain, which must be the go that built bin.
func version(t *testing.T, bin string) (ver, commit string) {
	t.Helper()
	res := run(t, spec{args: []string{"version"}, bin: bin})
	lines := strings.Split(res.stdout, "\n")
	toolchain := goEnv(t, "GOVERSION") + " " + goEnv(t, "GOOS") + "/" + goEnv(t, "GOARCH")
	if res.code != 0 || res.stderr != "" || len(lines) != 4 || lines[3] != "" ||
		!strings.HasPrefix(lines[0], "arv ") || !strings.HasPrefix(lines[1], "commit ") || lines[2] != toolchain {
		t.Fatalf("want arv VERSION, commit SHA and %q, one per line\n%s", toolchain, res)
	}
	return strings.TrimPrefix(lines[0], "arv "), strings.TrimPrefix(lines[1], "commit ")
}

// git runs git in dir, isolated from the user's and the system's
// configuration and hooks, and returns its trimmed output.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=" + t.TempDir(), "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=arv e2e", "GIT_AUTHOR_EMAIL=e2e@example.invalid",
		"GIT_COMMITTER_NAME=arv e2e", "GIT_COMMITTER_EMAIL=e2e@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// The suite's binary names the commit it was built from, or says it does
// not know. A git worktree is not stamped by the go command, so there it
// is "unknown"; in a plain checkout it is HEAD.
func TestP85VersionOfSuiteBinary(t *testing.T) {
	t.Parallel()
	_, commit := version(t, arvBin)
	if commit == "unknown" {
		return
	}
	commit = strings.TrimSuffix(commit, " (modified)")
	if head := git(t, "..", "rev-parse", "HEAD"); len(commit) < 12 || !strings.HasPrefix(head, commit) {
		t.Fatalf("commit %q, want HEAD %s", commit, head)
	}
}

// Built in a git repository, arv version prints the commit HEAD names and
// the version the go command derives from it, and says "(modified)" once
// the tree has changes. The repository is a copy of the sources arv is
// built from, committed afresh: the checkout the suite runs in may be a
// worktree, which the go command does not stamp, or have uncommitted work.
func TestP85VersionFromGit(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	files := strings.Fields(git(t, "..", "ls-files", "-co", "--exclude-standard", "--", "go.mod", "arv/*.go", "cmd/arv/*.go"))
	repo := t.TempDir()
	copied := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || strings.Contains(f, "/testdata/") {
			continue
		}
		src, err := os.ReadFile(filepath.Join("..", f))
		if os.IsNotExist(err) {
			continue // deleted from the worktree, not yet from the index
		}
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(repo, f)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, src, 0o644); err != nil {
			t.Fatal(err)
		}
		copied++
	}
	if copied < 10 {
		t.Fatalf("copied %d files of %v", copied, files)
	}
	git(t, repo, "init", "-q")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "arv e2e")
	head := git(t, repo, "rev-parse", "HEAD")

	bin := filepath.Join(t.TempDir(), "arv")
	build := func() {
		t.Helper()
		cmd := exec.Command("go", append(append([]string{"build", "-buildvcs=true", "-trimpath", "-o", bin}, buildFlags()...), "./cmd/arv")...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go build in a fresh repository: %v\n%s", err, out)
		}
	}
	build()
	ver, commit := version(t, bin)
	if commit != head || !strings.Contains(ver, head[:12]) || strings.HasSuffix(ver, "+dirty") {
		t.Fatalf("clean tree: version %q, commit %q; want a version naming %s and commit %s", ver, commit, head[:12], head)
	}

	if err := os.WriteFile(filepath.Join(repo, "notes.txt"), []byte("not committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	build()
	ver, commit = version(t, bin)
	if commit != head+" (modified)" || !strings.HasSuffix(ver, "+dirty") {
		t.Fatalf("changed tree: version %q, commit %q; want +dirty and %s (modified)", ver, commit, head)
	}
}
