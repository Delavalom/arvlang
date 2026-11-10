package runtime

import (
	"errors"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var fuzzKinds = []Kind{FSRead, FSWrite, Net, Env}

// fuzzScope turns a fuzzed string into a scope of kind k: fs scopes are
// placed under the fuzz tree, the others are used as given.
func fuzzScope(root string, k Kind, s string) string {
	if k == FSRead || k == FSWrite {
		return fuzzPath(root, s)
	}
	return s
}

// FuzzAdmit: CapSet.Admit (which spawn and the fs effects use) is
// consistent and never grants more than the set holds.
//
//   - Admitted: the result is canonical (a fixpoint of New and Admit), the
//     set allows it, and Grant finds a cover for it.
//   - Refused: a *DeniedError naming the request as given, or a
//     *CanonicalError at a component some grant covers.
//   - Attenuate agrees with Admit.
//   - I2: a child attenuated to the request admits only what the parent
//     admits (the same canonical capability), and dials only what the
//     parent may dial.
func FuzzAdmit(f *testing.F) {
	root := fuzzTree(f, f.TempDir())
	for i, s := range pathSeeds {
		f.Add(uint8(i), "g", "out/back/b", s, pathSeeds[(i+3)%len(pathSeeds)], []byte{127, 0, 0, 1}, uint16(80))
	}
	for _, n := range [][4]string{
		{"127.0.0.1", "*", "127.0.0.1:443", "127.0.0.1:443"},
		{"10.0.0.0/8", "example.com:443", "10.1.0.0/16", "10.1.2.3:80"},
		{"*", "*.example.com", "api.example.com:443", "a.api.example.com"},
		{"::ffff:10.0.0.0/104", "[::1]:443", "10.0.0.0/9", "[::ffff:10.0.0.1]:80"},
		{"localhost:8080", "0.0.0.0/0", "8.8.8.8", "8.8.8.8:53"},
		{"127.0.0.1:443", "fd00::/8", "127.0.0.1", "127.0.0.1:80"},
	} {
		f.Add(uint8(2), n[0], n[1], n[2], n[3], []byte{10, 1, 2, 3}, uint16(80))
	}
	f.Add(uint8(3), "*", "HOME", "PATH", "PATH", []byte{}, uint16(0))
	f.Fuzz(func(t *testing.T, kind uint8, g1, g2, req, probe string, addr []byte, port uint16) {
		k := fuzzKinds[int(kind)%len(fuzzKinds)]
		var held []Capability
		for _, g := range []string{g1, g2} {
			if c, err := New(k, fuzzScope(root, k, g)); err == nil {
				held = append(held, c)
			}
		}
		s := NewCapSet(held...)
		r := Capability{Kind: k, Scope: fuzzScope(root, k, req)}
		c, err := s.Admit(r)
		_, attErr := s.Attenuate([]Capability{r})
		if (err == nil) != (attErr == nil) {
			t.Fatalf("Admit(%s) err = %v, but Attenuate err = %v", r, err, attErr)
		}
		if err != nil {
			checkRefusal(t, s, r, err)
			return
		}
		checkAdmitted(t, s, r, c)

		child, err := s.Attenuate([]Capability{r})
		if err != nil {
			t.Fatalf("Attenuate(%s): %v", r, err)
		}
		p := Capability{Kind: k, Scope: fuzzScope(root, k, probe)}
		if pc, err := child.Admit(p); err == nil {
			if pp, err := s.Admit(p); err != nil || pp != pc {
				t.Fatalf("I2: child %v admits %s as %s; parent %v gives %s, %v", child.List(), p, pc, held, pp, err)
			}
		}
		if a, ok := netip.AddrFromSlice(addr); ok && k == Net {
			d := netip.AddrPortFrom(a, port)
			if child.AllowsDial(d) && !s.AllowsDial(d) {
				t.Fatalf("I2: child %v may dial %s; parent %v may not", child.List(), d, held)
			}
		}
	})
}

func checkRefusal(t *testing.T, s CapSet, r Capability, err error) {
	t.Helper()
	var de *DeniedError
	var ce *CanonicalError
	switch {
	case errors.As(err, &de):
		if de.Want != r {
			t.Fatalf("Admit(%s) denial names %s, want the request as given", r, de.Want)
		}
	case errors.As(err, &ce):
		if ce.Resolved == "" || !s.Allows(Capability{Kind: r.Kind, Scope: ce.Resolved}) {
			t.Fatalf("Admit(%s) shows %v from outside every grant", r, err)
		}
	default:
		t.Fatalf("Admit(%s) failed with %T %v", r, err, err)
	}
}

func checkAdmitted(t *testing.T, s CapSet, r, c Capability) {
	t.Helper()
	if c.Kind != r.Kind || !s.Allows(c) {
		t.Fatalf("Admit(%s) = %s, which the set does not allow", r, c)
	}
	if again, err := New(c.Kind, c.Scope); err != nil || again != c {
		t.Fatalf("Admit(%s) = %s, but New of that = %s, %v", r, c, again, err)
	}
	if again, err := s.Admit(c); err != nil || again != c {
		t.Fatalf("Admit(%s) = %s, but Admit of that = %s, %v", r, c, again, err)
	}
	g, ok := s.Grant(c)
	if !ok || !g.Covers(c) {
		t.Fatalf("Admit(%s) = %s, but Grant finds %s, %v", r, c, g, ok)
	}
	if (c.Kind == FSRead || c.Kind == FSWrite) && !componentsInside(g.Scope, c.Scope) {
		t.Fatalf("Admit(%s) = %s, which is not %s or a path beneath it", r, c, g)
	}
}

// componentsInside reports whether path p is dir or lies beneath it,
// comparing whole path components (an oracle independent of Covers).
func componentsInside(dir, p string) bool {
	split := func(s string) []string {
		var out []string
		for _, c := range strings.Split(s, string(filepath.Separator)) {
			if c != "" {
				out = append(out, c)
			}
		}
		return out
	}
	d, q := split(dir), split(p)
	return len(q) >= len(d) && slices.Equal(d, q[:len(d)])
}
