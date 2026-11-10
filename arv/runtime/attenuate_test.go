package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// linkTree returns dir with link -> target (a directory beside it) and a
// self-referencing loop, plus target's canonical path.
func linkTree(t *testing.T) (dir, canonTarget string) {
	t.Helper()
	dir = t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	for link, to := range map[string]string{"link": target, "loop": filepath.Join(dir, "loop")} {
		if err := os.Symlink(to, filepath.Join(dir, link)); err != nil {
			t.Fatal(err)
		}
	}
	canon, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	return dir, canon
}

// Attenuate takes requests as the script wrote them (made absolute, not
// canonical) and canonicalises each before checking the parent covers it.
// The child then holds the canonical form, like every other grant (I2, I3).
func TestAttenuateCanonicalisesRequests(t *testing.T) {
	dir, canonTarget := linkTree(t)
	parent := NewCapSet(mustCap(t, FSRead, canonTarget))
	child, err := parent.Attenuate([]Capability{{Kind: FSRead, Scope: filepath.Join(dir, "link")}})
	if err != nil {
		t.Fatalf("request through a symlink into the grant: %v", err)
	}
	if got := child.List(); len(got) != 1 || got[0].Scope != canonTarget {
		t.Fatalf("child holds %v, want the canonical %s", got, canonTarget)
	}
}

// A denied request names the path as the script wrote it, never what it
// resolves to, and a path that cannot be canonicalised is plainly denied:
// spawn reveals nothing about the filesystem beyond "denied".
func TestAttenuateDenialRevealsNothing(t *testing.T) {
	dir, canonTarget := linkTree(t)
	parent := NewCapSet(mustCap(t, FSRead, filepath.Join(dir, "other")))
	for _, raw := range []string{filepath.Join(dir, "link"), filepath.Join(dir, "loop", "x"), "relative"} {
		_, err := parent.Attenuate([]Capability{{Kind: FSRead, Scope: raw}})
		var denied *DeniedError
		if !errors.As(err, &denied) {
			t.Fatalf("Attenuate(%s) = %v, want *DeniedError", raw, err)
		}
		msg := err.Error()
		if !strings.Contains(msg, `fs_read("`+raw+`")`) || strings.Contains(msg, canonTarget) || strings.Contains(msg, "symbolic") {
			t.Errorf("Attenuate(%s) error %q: want the raw path only", raw, msg)
		}
	}
}

// Literal validates without touching the filesystem: no lstat, no readlink.
func TestLiteralDoesNotResolve(t *testing.T) {
	dir, _ := linkTree(t)
	for _, p := range []string{filepath.Join(dir, "link"), filepath.Join(dir, "loop", "x"), dir + "/a/../b"} {
		c, err := Literal(FSWrite, p)
		if err != nil || c.Scope != p {
			t.Errorf("Literal(fs_write, %s) = %v, %v; want the path unchanged", p, c, err)
		}
	}
	for _, bad := range []struct {
		k Kind
		s string
	}{{FSRead, "relative"}, {FSRead, ""}, {"disk", "/"}, {Net, "host:80"}} {
		if _, err := Literal(bad.k, bad.s); err == nil {
			t.Errorf("Literal(%s, %q) succeeded, want error", bad.k, bad.s)
		}
	}
	if c, _ := Literal(Net, "API.Example.COM."); c.Scope != "api.example.com" {
		t.Errorf("Literal net = %s, want the normalised host", c)
	}
}
