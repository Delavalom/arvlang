package e2e

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// sourceFiles lists, relative to root and slash-separated, the files a
// build of cmd/arv reads: go.mod and the non-test .go files under arv and
// cmd/arv, outside testdata. It walks the tree rather than asking git, so
// it works in a source copy that is not a git checkout.
func sourceFiles(t *testing.T, root string) []string {
	t.Helper()
	files := []string{"go.mod"}
	for _, dir := range []string{"arv", "cmd/arv"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if name := d.Name(); name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				rel, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				files = append(files, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("listing the sources of cmd/arv: %v", err)
		}
	}
	return files
}

// freshRepo is a new git repository holding a copy of the sources of
// cmd/arv and of the extra files named (paths relative to the checkout),
// committed once. It returns the repository and its HEAD.
func freshRepo(t *testing.T, extra ...string) (repo, head string) {
	t.Helper()
	files := append(sourceFiles(t, ".."), extra...)
	repo = t.TempDir()
	for _, f := range files {
		src, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(f)))
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(repo, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if len(files) < 10 {
		t.Fatalf("copied %d files: %v", len(files), files)
	}
	git(t, repo, "init", "-q")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "arv e2e")
	return repo, git(t, repo, "rev-parse", "HEAD")
}

// sourceFiles needs no git repository: the e2e suite (make test, make e2e)
// runs in a source copy without .git, such as a git archive export.
func TestP85SourceFilesWithoutGit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, f := range []string{"go.mod", "go.sum", "README.md", "main.go", "arv/a.go", "arv/a_test.go",
		"arv/x/b.go", "arv/testdata/c.go", "arv/notes.txt", "cmd/arv/main.go", "cmd/other/d.go", "e2e/e.go"} {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := sourceFiles(t, root)
	slices.Sort(got)
	want := []string{"arv/a.go", "arv/x/b.go", "cmd/arv/main.go", "go.mod"}
	if !slices.Equal(got, want) {
		t.Fatalf("sourceFiles: %q, want %q", got, want)
	}
}
