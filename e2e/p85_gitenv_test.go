package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// repoState is what a stray git command run against a repository would
// change: HEAD, its refs, its index and its configuration.
func repoState(t *testing.T, dir string) (head, refs string, index, config []byte) {
	t.Helper()
	head = git(t, dir, "rev-parse", "HEAD")
	refs = git(t, dir, "for-each-ref")
	index, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	config, err = os.ReadFile(filepath.Join(dir, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	return head, refs, index, config
}

// The git fixtures (freshRepo, the go build in a fresh repository, make in
// its worktree) ignore the GIT_* variables of the environment. git exports
// GIT_DIR and GIT_INDEX_FILE to its hooks, so a hook that runs make test
// would otherwise reinitialise, stage into and commit onto the repository
// it runs in. Here they name a decoy, which must come out unchanged, and
// the fixtures must still work. Not parallel: t.Setenv changes the
// process's environment, and the parallel tests are paused meanwhile.
func TestP85GitFixturesIgnoreGitEnv(t *testing.T) {
	decoy := t.TempDir()
	git(t, decoy, "init", "-q")
	if err := os.WriteFile(filepath.Join(decoy, "a.txt"), []byte("decoy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, decoy, "add", "a.txt")
	git(t, decoy, "commit", "-q", "-m", "decoy")
	head, refs, index, config := repoState(t, decoy)

	gitDir := filepath.Join(decoy, ".git")
	t.Setenv("GIT_DIR", gitDir)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(gitDir, "index"))
	t.Run("VersionFromGit", checkVersionFromGit)
	t.Run("MakeBuildInWorktree", checkMakeBuildInWorktree)

	h, r, i, c := repoState(t, decoy)
	if h != head || r != refs || !bytes.Equal(i, index) || !bytes.Equal(c, config) {
		t.Fatalf("the decoy repository changed: HEAD %s -> %s\nrefs %q -> %q\nindex changed %v, config changed %v",
			head, h, refs, r, !bytes.Equal(i, index), !bytes.Equal(c, config))
	}
	if files := git(t, decoy, "ls-files"); files != "a.txt" {
		t.Fatalf("the decoy's index lists %q, want a.txt", files)
	}
}
