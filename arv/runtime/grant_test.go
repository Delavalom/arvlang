package runtime

import (
	"path/filepath"
	"testing"
)

// Grant picks the outermost cover: the root the fs effects open must not
// itself sit inside another grant, where it could be swapped for a symlink.
func TestGrantPicksTheOutermostCover(t *testing.T) {
	dir, _ := canonTree(t)
	outer := mustCap(t, FSRead, dir)
	inner := mustCap(t, FSRead, filepath.Join(dir, "a"))
	file := mustCap(t, FSRead, filepath.Join(dir, "b", "f"))
	write := mustCap(t, FSWrite, filepath.Join(dir, "a", "b"))
	writeFile := mustCap(t, FSWrite, filepath.Join(dir, "a", "b", "f"))
	// Order must not matter: put the narrower grants first.
	s := NewCapSet(writeFile, file, inner, write, outer)

	for _, c := range []struct {
		req  Capability
		want Capability
		ok   bool
	}{
		{mustCap(t, FSRead, filepath.Join(dir, "a", "b", "f")), outer, true},
		{mustCap(t, FSRead, filepath.Join(dir, "a")), outer, true},
		{mustCap(t, FSRead, filepath.Join(dir, "b", "f")), outer, true},
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

	// With only a file grant and nothing around it, the file grant is it.
	only := NewCapSet(file)
	if got, ok := only.Grant(file); !ok || got != file {
		t.Errorf("Grant(%s) = %s, %v; want the file grant", file, got, ok)
	}
}
