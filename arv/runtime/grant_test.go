package runtime

import (
	"path/filepath"
	"testing"
)

func TestGrantPicksTheMostSpecificCover(t *testing.T) {
	dir, _ := canonTree(t)
	outer := mustCap(t, FSRead, dir)
	inner := mustCap(t, FSRead, filepath.Join(dir, "a"))
	write := mustCap(t, FSWrite, filepath.Join(dir, "a", "b"))
	s := NewCapSet(outer, inner, write)

	for _, c := range []struct {
		req  Capability
		want Capability
		ok   bool
	}{
		{mustCap(t, FSRead, filepath.Join(dir, "a", "b", "f")), inner, true},
		{mustCap(t, FSRead, filepath.Join(dir, "a")), inner, true},
		{mustCap(t, FSRead, filepath.Join(dir, "z")), outer, true},
		{mustCap(t, FSWrite, filepath.Join(dir, "a", "b", "f")), write, true},
		{mustCap(t, FSWrite, filepath.Join(dir, "z")), Capability{}, false},
		{mustCap(t, Env, "HOME"), Capability{}, false},
	} {
		got, ok := s.Grant(c.req)
		if ok != c.ok || got != c.want {
			t.Errorf("Grant(%s) = %s, %v; want %s, %v", c.req, got, ok, c.want, c.ok)
		}
	}
}
