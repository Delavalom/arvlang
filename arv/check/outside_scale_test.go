package check

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	rt "github.com/delavalom/arvlang/arv/runtime"
)

// scaleCase is n header grants and n sites. Half of the sites are covered
// (a net host, a file under a granted directory), half are not (a net
// host and an env name the header does not grant), so both what Outside
// does for an allowed site and for a refused one are measured.
func scaleCase(t *testing.T, n int) (*Report, []rt.Capability, string) {
	t.Helper()
	dir := t.TempDir()
	var entries []Entry
	r := &Report{}
	for i := 0; i < n/4; i++ {
		entries = append(entries,
			Entry{Text: fmt.Sprintf("net=h%d.example", i)},
			Entry{Text: fmt.Sprintf("fs_read=d%d", i)},
			Entry{Text: fmt.Sprintf("env=E%d", i)},
			Entry{Text: fmt.Sprintf("net=10.%d.%d.0/24", i/256, i%256)})
		r.Sites = append(r.Sites,
			Need{Kind: rt.Net, Scope: fmt.Sprintf("h%d.example", i)},
			Need{Kind: rt.FSRead, Scope: fmt.Sprintf("d%d/f", i)},
			Need{Kind: rt.Net, Scope: fmt.Sprintf("x%d.example:443", i)},
			Need{Kind: rt.Env, Scope: fmt.Sprintf("V%d", i)})
	}
	grants, err := Grants(entries, dir)
	if err != nil {
		t.Fatal(err)
	}
	return r, grants, dir
}

// Outside compares each site with the few grants that can cover it, not
// with every grant: the header and the body both come from the script, and
// comparing all of one with all of the other let a 730 KB file stall
// arv check, and run --manifest before --timeout starts, for 10 s.
func TestOutsideIsNotQuadratic(t *testing.T) {
	old := covers
	defer func() { covers = old }()
	calls := 0
	covers = func(c, r rt.Capability) bool { calls++; return old(c, r) }
	const n = 4000
	r, grants, dir := scaleCase(t, n)
	got := Outside(r, grants, dir)
	if len(got) != n/2 {
		t.Fatalf("Outside found %d misses, want %d", len(got), n/2)
	}
	if calls == 0 || calls > 4*len(r.Sites) {
		t.Errorf("Outside made %d grant comparisons (through covers) for %d sites, want between 1 and %d",
			calls, len(r.Sites), 4*len(r.Sites))
	}
}

// A refused site costs Outside a bounded amount of memory, whatever the
// size of the header: CapSet.Admit's denial copied every grant into its
// error, so the allocation grew with header lines times refused sites.
func TestOutsideMemoryIsLinear(t *testing.T) {
	perSite := func(n int) float64 {
		r, grants, dir := scaleCase(t, n)
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		Outside(r, grants, dir)
		runtime.ReadMemStats(&after)
		return float64(after.TotalAlloc-before.TotalAlloc) / float64(n)
	}
	small, large := perSite(1000), perSite(8000)
	if large > 2*small+512 {
		t.Errorf("Outside allocated %.0f bytes per site with 1000 grants and %.0f with 8000: it grows with the header",
			small, large)
	}
}

// outsideNaive is Outside as first built: CapSet.Admit against every
// grant. It is the oracle for Outside's index.
func outsideNaive(r *Report, grants []rt.Capability, cwd string) []Miss {
	set := rt.NewCapSet(grants...)
	seen := map[string]bool{}
	var out []Miss
	for _, s := range r.Sites {
		if seen[s.String()] {
			continue
		}
		seen[s.String()] = true
		scope := s.Scope
		if (s.Kind == rt.FSRead || s.Kind == rt.FSWrite) && !filepath.IsAbs(scope) {
			scope = rt.Resolve(cwd, scope)
		}
		lit, err := rt.Literal(s.Kind, scope)
		if err == nil {
			_, err = set.Admit(lit)
		}
		var ce *rt.CanonicalError
		switch {
		case errors.As(err, &ce):
			out = append(out, Miss{s, ce.Err})
		case err != nil:
			out = append(out, Miss{s, nil})
		}
	}
	return out
}
