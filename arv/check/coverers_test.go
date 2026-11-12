package check

import (
	"testing"

	rt "github.com/delavalom/arvlang/arv/runtime"
)

// For env, only "*" covers another name, so least and Outside ask Covers
// about an env need only against "*" or the need's own name. A wider
// candidate list keeps the answers (Covers still decides) but costs a
// Covers call per extra candidate on every env need: a list that named
// any other variable, or every variable, is the quadratic comparison that
// coverers exists to avoid.
func TestEnvCoverersAreOnlyTheNameAndStar(t *testing.T) {
	old := covers
	defer func() { covers = old }()
	var bad []string
	covers = func(c, r rt.Capability) bool {
		if r.Kind == rt.Env && c.Scope != "*" && c.Scope != r.Scope {
			bad = append(bad, "env="+c.Scope+" asked about env="+r.Scope)
		}
		return old(c, r)
	}
	names := []string{"PATH", "HOME", "A", "PATH", "*", "LANG"}
	for _, want := range [][]string{{"PATH", "*"}, {"HOME", "*"}, {"*", "*"}} {
		got := coverers(Need{Kind: rt.Env, Scope: want[0]}, nil)
		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("coverers(env=%s) = %q, want %q", want[0], got, want)
		}
	}

	var needs []Need
	for _, s := range names {
		needs = append(needs, Need{Kind: rt.Env, Scope: s})
	}
	if got := least(distinct(needs)); len(got) != 1 || got[0].Scope != "*" {
		t.Errorf("least kept %v, want only env=*", got)
	}
	if got := least(distinct(needs[:4])); len(got) != 3 {
		t.Errorf("least kept %v, want PATH, HOME and A", got)
	}

	var grants []rt.Capability
	for _, s := range []string{"PATH", "HOME", "LANG"} {
		grants = append(grants, rt.Capability{Kind: rt.Env, Scope: s})
	}
	r := &Report{Sites: []Need{{Kind: rt.Env, Scope: "HOME"}, {Kind: rt.Env, Scope: "A"},
		{Kind: rt.Env, Scope: "LANG"}, {Kind: rt.Env, Scope: "B"}}}
	if got := Outside(r, grants, t.TempDir()); len(got) != 2 || got[0].Scope != "A" || got[1].Scope != "B" {
		t.Errorf("Outside = %v, want A and B", got)
	}
	for _, b := range bad {
		t.Error(b)
	}
}
