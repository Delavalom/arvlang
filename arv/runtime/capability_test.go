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
		{mustCap(t, FSRead, "/usr"), mustCap(t, FSRead, "/usr/../etc/passwd"), false},
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

// A Capability built without New may have an empty scope. It must never
// cover anything: an empty fs prefix would otherwise become "/".
func TestCoversRefusesEmptyScope(t *testing.T) {
	hosts := mustCap(t, FSRead, "/etc/hosts")
	for _, held := range []Capability{{Kind: FSRead}, {Kind: FSWrite}, {Kind: Net}, {Kind: Env}} {
		for _, want := range []Capability{hosts, {Kind: held.Kind}} {
			if held.Covers(want) {
				t.Errorf("%s covers %s; an empty scope must cover nothing", held, want)
			}
		}
	}
}

// "fs_read=$UNSET" must not silently grant the base directory.
func TestParseRejectsEmptyScope(t *testing.T) {
	base := t.TempDir()
	for _, s := range []string{"fs_read=", "fs_write=", "net=", "env="} {
		if c, err := Parse(s, base); err == nil {
			t.Errorf("Parse(%q) = %s, want an empty-scope error", s, c)
		}
	}
}

// Relative grants keep their ".." for Canonical, which applies it after
// following symlinks (the kernel's rule), not lexically before.
func TestParseRelativeDotDotFollowsSymlinks(t *testing.T) {
	root, canon := canonTree(t)
	if err := os.Mkdir(filepath.Join(root, "outside", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlink(t, filepath.Join(root, "outside", "inner"), filepath.Join(root, "data", "link"))
	c, err := Parse("fs_read=data/link/..", root)
	if err != nil || c.Scope != filepath.Join(canon, "outside") {
		t.Fatalf("Parse(data/link/..) = %v, %v; want scope %s", c, err, filepath.Join(canon, "outside"))
	}
	if got := Resolve("/base", "a/../b"); got != "/base/a/../b" {
		t.Errorf("Resolve must not clean: got %q", got)
	}
	if got := Resolve("/base", "/abs/../x"); got != "/abs/../x" {
		t.Errorf("Resolve must leave absolute paths alone: got %q", got)
	}
}

func TestNewNormalisesNetScopes(t *testing.T) {
	for in, want := range map[string]string{
		"API.GitHub.com":  "api.github.com",
		"localhost.":      "localhost",
		"127.0.0.1":       "127.0.0.1",
		"[::1]":           "::1",
		"::1":             "::1", // url.Hostname() strips the brackets
		"[0:0::1]":        "::1",
		"*":               "*",
		"*.GitHub.com.":   "*.github.com",
		"under_score.dev": "under_score.dev",
	} {
		c, err := New(Net, in)
		if err != nil || c.Scope != want {
			t.Errorf("New(net, %q) = %q, %v; want %q", in, c.Scope, err, want)
		}
	}
}

// Each of these would be accepted but never match a request (url.Hostname
// has no port, scheme or brackets), or would match far more than intended.
func TestNewRejectsBadNetScopes(t *testing.T) {
	for _, in := range []string{
		"", ".", "127.0.0.1:8080", "localhost:80", "[::1]:80", "https://host",
		"host/path", "user@host", "a b", " host", "host\t", "host\n", "host\x00",
		"[127.0.0.1]", "[host]", "[::1", "*.0.0.1", "*.127.0.0.1", "*.10",
		"*.", "*x.com", "a.*.com", "**.com", "*.*.com", "*.[::1]",
	} {
		if c, err := New(Net, in); err == nil {
			t.Errorf("New(net, %q) = %s, want an error", in, c)
		}
	}
}

func TestNetCoverage(t *testing.T) {
	cases := []struct {
		held, want string
		ok         bool
	}{
		{"localhost", "LOCALHOST.", true},
		{"localhost.", "localhost", true},
		{"[::1]", "::1", true},
		{"*.github.com", "API.GITHUB.COM.", true},
		{"*.github.com", "evilgithub.com", false},
	}
	for _, c := range cases {
		if got := mustCap(t, Net, c.held).Covers(mustCap(t, Net, c.want)); got != c.ok {
			t.Errorf("net(%q) covers net(%q) = %v, want %v", c.held, c.want, got, c.ok)
		}
	}
	// Defence in depth for a Capability built without New.
	if (Capability{Kind: Net, Scope: "*.0.0.1"}).Covers(mustCap(t, Net, "127.0.0.1")) {
		t.Error("a DNS wildcard must not cover an IP literal")
	}
}
