package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// I1: building a capability value needs no authority, so it must not touch
// the filesystem. It holds the path as written, made absolute.
func TestCapabilityConstructorsDoNotTouchFS(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink("/etc", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "loop"), filepath.Join(dir, "loop")); err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{
		fmt.Sprintf(`fs_read(%q)`, filepath.Join(dir, "link")),
		fmt.Sprintf(`fs_write(%q)`, filepath.Join(dir, "loop", "x")),
		`fs_read("/etc/localtime")`,
	} {
		got, _ := runWith(t, src)
		if got.Inspect() != src {
			t.Errorf("%s = %s, want it printed as written", src, got.Inspect())
		}
	}
	chdir(t, dir)
	cwd, _ := os.Getwd()
	got, _ := runWith(t, `fs_read("link/../x")`)
	if want := fmt.Sprintf("fs_read(%q)", cwd+"/link/../x"); got.Inspect() != want {
		t.Errorf("relative path = %s, want %s (absolute, not cleaned)", got.Inspect(), want)
	}
}

// Canonicalisation happens at spawn: a request through a symlink into the
// parent's grant is allowed; one that resolves outside it is denied, and
// the denial shows the path as the script wrote it.
func TestSpawnCanonicalisesRequests(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	mustMkdir(t, target)
	os.WriteFile(filepath.Join(target, "f"), []byte("through the link"), 0o644)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	src := fmt.Sprintf(`await spawn read_file(%q) with [fs_read(%q)]`, filepath.Join(target, "f"), link)

	got, _ := runWith(t, src, grant(t, runtime.FSRead, target))
	if got.Inspect() != "through the link" {
		t.Fatalf("grant covers the link's target: got %s", got.Inspect())
	}

	other := filepath.Join(dir, "other")
	mustMkdir(t, other)
	got, _ = runWith(t, src, grant(t, runtime.FSRead, other))
	wantDenied(t, got, fmt.Sprintf("fs_read(%q)", link))
	if strings.Contains(got.(*value.Error).Message, "target") {
		t.Errorf("denial reveals where the link points: %s", got.Inspect())
	}

	got, _ = runWith(t, fmt.Sprintf(`spawn read_file("x") with [fs_read(%q)]`, filepath.Join(dir, "loop", "x")),
		grant(t, runtime.FSRead, other))
	wantDenied(t, got, "loop")
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
