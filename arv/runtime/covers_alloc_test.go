package runtime

import (
	"strings"
	"testing"
)

// Covers compares an fs grant with a request in place. It built
// c.Scope + "/" on every check, a copy of the grant's scope (up to
// MaxScopeLen bytes), uncharged, and Allows, Grant and spawn's Admit run
// it once per held grant.
func TestCoversAllocatesNothing(t *testing.T) {
	long := "/" + strings.Repeat("a", 3000)
	g := Capability{Kind: FSRead, Scope: long}
	for _, r := range []string{long + "/x", long + "b", "/b", long} {
		req := Capability{Kind: FSRead, Scope: r}
		if n := testing.AllocsPerRun(100, func() { g.Covers(req) }); n != 0 {
			t.Errorf("Covers(%.20s...) allocates %v times", r, n)
		}
	}
	// The same answers as before.
	for _, tt := range []struct {
		g, r string
		want bool
	}{
		{"/a", "/a/b", true}, {"/a", "/ab", false}, {"/a", "/a", true}, {"/a/b", "/a", false},
		{"/", "/a", true}, {"/a/", "/a//b", true}, {"/a/", "/a/b", false},
	} {
		got := Capability{Kind: FSWrite, Scope: tt.g}.Covers(Capability{Kind: FSWrite, Scope: tt.r})
		if got != tt.want {
			t.Errorf("%s covers %s = %v, want %v", tt.g, tt.r, got, tt.want)
		}
	}
}
