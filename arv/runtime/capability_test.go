package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustCap(t *testing.T, k Kind, scope string) Capability {
	t.Helper()
	c, err := New(k, scope)
	if err != nil {
		t.Fatalf("New(%s, %q): %v", k, scope, err)
	}
	return c
}

func TestCovers(t *testing.T) {
	tests := []struct {
		held, want Capability
		ok         bool
	}{
		{mustCap(t, Net, "api.github.com"), mustCap(t, Net, "api.github.com"), true},
		{mustCap(t, Net, "api.github.com"), mustCap(t, Net, "evil.com"), false},
		{mustCap(t, Net, "*.github.com"), mustCap(t, Net, "api.github.com"), true},
		{mustCap(t, Net, "*.github.com"), mustCap(t, Net, "github.com"), false},
		{mustCap(t, Net, "*"), mustCap(t, Net, "anything.io"), true},
		{mustCap(t, FSRead, "/data"), mustCap(t, FSRead, "/data/a.txt"), true},
		{mustCap(t, FSRead, "/data"), mustCap(t, FSRead, "/database/a.txt"), false},
		{mustCap(t, FSRead, "/data"), mustCap(t, FSRead, "/data/../etc/passwd"), false},
		{mustCap(t, FSRead, "/data"), mustCap(t, FSWrite, "/data/a.txt"), false},
		{mustCap(t, Env, "HOME"), mustCap(t, Env, "HOME"), true},
		{mustCap(t, Env, "HOME"), mustCap(t, Env, "AWS_SECRET_ACCESS_KEY"), false},
	}
	for _, tt := range tests {
		if got := tt.held.Covers(tt.want); got != tt.ok {
			t.Errorf("%s covers %s = %v, want %v", tt.held, tt.want, got, tt.ok)
		}
	}
}

func TestNewRejectsBadInput(t *testing.T) {
	for _, c := range []struct {
		k     Kind
		scope string
	}{{"disk", "/"}, {Net, ""}, {FSRead, "relative/path"}} {
		if _, err := New(c.k, c.scope); err == nil {
			t.Errorf("New(%s, %q) succeeded, want error", c.k, c.scope)
		}
	}
}

func TestAttenuate(t *testing.T) {
	parent := NewCapSet(mustCap(t, Net, "*.github.com"), mustCap(t, FSRead, "/data"))

	child, err := parent.Attenuate([]Capability{mustCap(t, Net, "api.github.com")})
	if err != nil {
		t.Fatalf("attenuate to a covered cap: %v", err)
	}
	if child.Allows(mustCap(t, FSRead, "/data/x")) {
		t.Error("child kept fs_read it did not ask for")
	}

	_, err = parent.Attenuate([]Capability{mustCap(t, Net, "evil.com")})
	var denied *DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("want *DeniedError, got %v", err)
	}
	if !strings.Contains(err.Error(), `net("evil.com")`) {
		t.Errorf("error should name the request, got %q", err)
	}
}

func TestParse(t *testing.T) {
	base := t.TempDir()
	c, err := Parse("fs_read=data", base)
	want, cerr := Canonical(filepath.Join(base, "data"))
	if cerr != nil || err != nil || c.Scope != want {
		t.Fatalf("Parse relative fs path = %v, %v; want scope %s (%v)", c, err, want, cerr)
	}
	if _, err := Parse("net", "/"); err == nil {
		t.Error("Parse without '=' should fail")
	}
}

// canonTree returns a fresh temp dir and its fully resolved form (on macOS
// t.TempDir() is under /var, a symlink to /private/var).
func canonTree(t *testing.T) (root, canon string) {
	t.Helper()
	root = t.TempDir()
	canon, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"data", "outside"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root, canon
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func wantCanonical(t *testing.T, path, want string) {
	t.Helper()
	got, err := Canonical(path)
	if err != nil || got != want {
		t.Fatalf("Canonical(%q) = %q, %v; want %q", path, got, err, want)
	}
}

func TestCanonicalExistingPathMatchesEvalSymlinks(t *testing.T) {
	root, canon := canonTree(t)
	wantCanonical(t, root, canon)
	wantCanonical(t, filepath.Join(root, "data"), filepath.Join(canon, "data"))
	wantCanonical(t, "/", "/")
}

func TestCanonicalMissingPathUnderRealDir(t *testing.T) {
	root, canon := canonTree(t)
	wantCanonical(t, filepath.Join(root, "data", "new", "deeper.txt"), filepath.Join(canon, "data", "new", "deeper.txt"))
}

// The reported bug: a dangling leaf symlink must resolve to its target, not
// to the link's own path inside the grant.
func TestCanonicalDanglingLeafSymlink(t *testing.T) {
	root, canon := canonTree(t)
	symlink(t, filepath.Join(root, "outside", "created.txt"), filepath.Join(root, "data", "dangling"))
	wantCanonical(t, filepath.Join(root, "data", "dangling"), filepath.Join(canon, "outside", "created.txt"))

	// And a capability check on it must fail.
	held := mustCap(t, FSWrite, filepath.Join(root, "data"))
	if want := mustCap(t, FSWrite, filepath.Join(root, "data", "dangling")); held.Covers(want) {
		t.Fatalf("%s covers %s; a dangling symlink escaped its grant", held, want)
	}
}

func TestCanonicalMissingFileUnderSymlinkedDir(t *testing.T) {
	root, canon := canonTree(t)
	symlink(t, filepath.Join(root, "outside"), filepath.Join(root, "data", "link-dir"))
	wantCanonical(t, filepath.Join(root, "data", "link-dir", "new.txt"), filepath.Join(canon, "outside", "new.txt"))

	// The parent symlink is itself dangling: resolve through it all the same.
	symlink(t, filepath.Join(root, "outside", "newdir"), filepath.Join(root, "data", "dlink"))
	wantCanonical(t, filepath.Join(root, "data", "dlink", "x", "y.txt"), filepath.Join(canon, "outside", "newdir", "x", "y.txt"))
}

func TestCanonicalRelativeSymlinkTarget(t *testing.T) {
	root, canon := canonTree(t)
	// Relative targets resolve against the link's directory, not the cwd.
	symlink(t, filepath.Join("..", "outside", "created.txt"), filepath.Join(root, "data", "rel"))
	wantCanonical(t, filepath.Join(root, "data", "rel"), filepath.Join(canon, "outside", "created.txt"))

	// A chain of relative links, ending dangling.
	symlink(t, "hop2", filepath.Join(root, "data", "hop1"))
	symlink(t, "../outside/far.txt", filepath.Join(root, "data", "hop2"))
	wantCanonical(t, filepath.Join(root, "data", "hop1"), filepath.Join(canon, "outside", "far.txt"))
}

// ".." after a symlink goes to the parent of the link's target, as the OS does.
func TestCanonicalDotDotAfterSymlink(t *testing.T) {
	root, canon := canonTree(t)
	if err := os.Mkdir(filepath.Join(root, "outside", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlink(t, filepath.Join(root, "outside", "inner"), filepath.Join(root, "data", "link"))
	wantCanonical(t, filepath.Join(root, "data", "link")+"/../x.txt", filepath.Join(canon, "outside", "x.txt"))
	// Lexical ".." on plain dirs still works.
	wantCanonical(t, filepath.Join(root, "data")+"/../outside/x.txt", filepath.Join(canon, "outside", "x.txt"))
}

func TestCanonicalSymlinkLoopIsAnError(t *testing.T) {
	root, _ := canonTree(t)
	a, b, self := filepath.Join(root, "data", "a"), filepath.Join(root, "data", "b"), filepath.Join(root, "data", "self")
	symlink(t, b, a)
	symlink(t, a, b)
	symlink(t, "self", self)
	for _, p := range []string{a, filepath.Join(a, "x.txt"), self} {
		if got, err := Canonical(p); err == nil {
			t.Errorf("Canonical(%q) = %q, want a symlink-loop error", p, got)
		}
		if c, err := New(FSWrite, p); err == nil {
			t.Errorf("New(fs_write, %q) = %s, want an error", p, c)
		}
	}
}

func TestCanonicalCapsSymlinkHops(t *testing.T) {
	root, canon := canonTree(t)
	chain := func(name string, n int) string {
		// name0 -> name1 -> ... -> name<n> (dangling, in outside/)
		for i := 0; i < n; i++ {
			symlink(t, fmt.Sprintf("%s%d", name, i+1), filepath.Join(root, "data", fmt.Sprintf("%s%d", name, i)))
		}
		symlink(t, filepath.Join(root, "outside", name+".txt"), filepath.Join(root, "data", fmt.Sprintf("%s%d", name, n)))
		return filepath.Join(root, "data", name+"0")
	}
	// 31 links (plus /var -> /private/var on macOS) is under the 40-hop cap; 61 is over it.
	wantCanonical(t, chain("short", 30), filepath.Join(canon, "outside", "short.txt"))
	if got, err := Canonical(chain("long", 60)); err == nil {
		t.Fatalf("Canonical through 61 links = %q, want a too-many-links error", got)
	}
}

func TestCanonicalRejectsRelativePath(t *testing.T) {
	if got, err := Canonical("relative/path"); err == nil {
		t.Fatalf("Canonical(relative) = %q, want error", got)
	}
}
