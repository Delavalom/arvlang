package arv

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

func TestRunReturnsValueAndOutput(t *testing.T) {
	var out bytes.Buffer
	got, err := Run(`puts("start"); let add = fn(a, b) { a + b }; add(2, 3)`, Options{Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if got.Inspect() != "5" || out.String() != "start\n" {
		t.Fatalf("got %s, out %q", got.Inspect(), out.String())
	}
}

func TestRunSurfacesErrors(t *testing.T) {
	if _, err := Run(`let = 1`, Options{}); err == nil || !strings.HasPrefix(err.Error(), "parse error") {
		t.Fatalf("want parse error, got %v", err)
	}
	if _, err := Run(`getenv("HOME")`, Options{}); err == nil || !strings.Contains(err.Error(), "capability denied") {
		t.Fatalf("want capability denied, got %v", err)
	}
	home, _ := runtime.New(runtime.Env, "HOME")
	if _, err := Run(`getenv("HOME")`, Options{Caps: []runtime.Capability{home}}); err != nil {
		t.Fatalf("granted env: %v", err)
	}
}

// Options.Caps bypasses runtime.New, so Run must validate every grant itself.
// An fs grant with an empty scope used to cover the whole filesystem.
func TestRunRejectsInvalidCaps(t *testing.T) {
	for _, c := range []runtime.Capability{
		{Kind: runtime.FSRead},
		{Kind: runtime.FSWrite, Scope: ""},
		{Kind: runtime.FSRead, Scope: "relative/dir"},
		{Kind: runtime.Net, Scope: "127.0.0.1:8080"},
		{Kind: "disk", Scope: "/"},
	} {
		got, err := Run(`read_file("/etc/hosts")`, Options{Caps: []runtime.Capability{c}})
		if err == nil || got != nil {
			t.Errorf("Run with %s = %v, %v; want an invalid-capability error", c, got, err)
		}
	}
}

// Run canonicalises grants the way runtime.New does, so a grant spelled
// through a symlink (macOS t.TempDir() is under /var -> /private/var) still
// covers requests, which are always canonical.
func TestRunCanonicalisesCaps(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("via link"), 0o644); err != nil {
		t.Fatal(err)
	}
	caps := []runtime.Capability{{Kind: runtime.FSRead, Scope: link}}
	got, err := Run(fmt.Sprintf(`read_file(%q)`, filepath.Join(dir, "f.txt")), Options{Caps: caps})
	if err != nil || got.Inspect() != "via link" {
		t.Fatalf("got %v, %v; want the file read through a raw symlinked grant", got, err)
	}
}
