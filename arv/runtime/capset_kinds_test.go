package runtime

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"strings"
	"testing"
)

func mustKinds(t *testing.T, specs ...KindSpec) *Kinds {
	t.Helper()
	k, err := NewKinds(specs...)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// subtree covers a request equal to the grant or below it ("team/a"
// under "team"): reflexive and transitive, as KindSpec requires.
func subtree(g, r string) bool { return r == g || strings.HasPrefix(r, g+"/") }

func TestCapSetChecksHostKinds(t *testing.T) {
	k := mustKinds(t, KindSpec{Kind: "k8s"}, KindSpec{Kind: "team", Covers: subtree})
	s := NewCapSetKinds(k, Capability{"k8s", "staging"}, Capability{"team", "infra"})
	if s.Kinds() != k {
		t.Fatal("Kinds() lost the registry")
	}
	for _, tt := range []struct {
		want Capability
		ok   bool
	}{
		{Capability{"k8s", "staging"}, true},
		{Capability{"k8s", "prod"}, false},
		{Capability{"k8s", "Staging"}, false}, // exact match: no normalisation
		{Capability{"k8s", "staging/x"}, false},
		{Capability{"team", "infra"}, true},
		{Capability{"team", "infra/db"}, true},
		{Capability{"team", "infrastructure"}, false},
		{Capability{"team", "ops"}, false},
		{Capability{"k8s", ""}, false},
	} {
		if got := s.Allows(tt.want); got != tt.ok {
			t.Errorf("Allows(%s) = %v, want %v", tt.want, got, tt.ok)
		}
		c, err := s.Admit(tt.want)
		var denied *DeniedError
		switch {
		case tt.ok && (err != nil || c != tt.want):
			t.Errorf("Admit(%s) = %v, %v; want it as is", tt.want, c, err)
		case !tt.ok && !errors.As(err, &denied):
			t.Errorf("Admit(%s) err = %v, want a denial", tt.want, err)
		case !tt.ok && denied.Want != tt.want:
			t.Errorf("Admit(%s) denial names %s", tt.want, denied.Want)
		}
	}
}

// Without the registry a host kind is unknown: a Capability literal of it
// grants nothing, and a request for it is denied.
func TestCapSetWithoutKindsDeniesHostKinds(t *testing.T) {
	s := NewCapSet(Capability{"k8s", "staging"})
	if s.Allows(Capability{"k8s", "staging"}) {
		t.Fatal("a host kind was allowed without its registry")
	}
	var denied *DeniedError
	if _, err := s.Admit(Capability{"k8s", "staging"}); !errors.As(err, &denied) {
		t.Fatalf("Admit err = %v, want a denial", err)
	}
}

// A child set keeps the registry, holds exactly what it asked for, and
// can be narrowed again (I2).
func TestAttenuateHostKinds(t *testing.T) {
	k := mustKinds(t, KindSpec{Kind: "k8s"}, KindSpec{Kind: "team", Covers: subtree})
	s := NewCapSetKinds(k, Capability{"k8s", "staging"}, Capability{"team", "infra"}, Capability{Net, "example.com"})
	child, err := s.Attenuate([]Capability{{"team", "infra/db"}, {"k8s", "staging"}})
	if err != nil {
		t.Fatal(err)
	}
	if child.Kinds() != k {
		t.Fatal("the child lost the registry")
	}
	if !child.Allows(Capability{"team", "infra/db/eu"}) || child.Allows(Capability{"team", "infra/web"}) {
		t.Fatal("the child holds the wrong team grants")
	}
	if child.Allows(Capability{Net, "example.com"}) {
		t.Fatal("the child holds a grant it did not ask for")
	}
	if _, err := child.Attenuate([]Capability{{"team", "infra"}}); err == nil {
		t.Fatal("a grandchild widened team/infra/db to team/infra")
	}
	if _, err := s.Attenuate([]Capability{{"k8s", "prod"}}); err == nil {
		t.Fatal("a child got k8s(prod) from a parent holding k8s(staging)")
	}
	root := NewRoot(context.Background(), child, io.Discard)
	if err := root.Require(Capability{"k8s", "staging"}); err != nil {
		t.Fatalf("Require: %v", err)
	}
	if err := root.Require(Capability{"k8s", "prod"}); err == nil || !strings.Contains(err.Error(), `k8s("prod")`) {
		t.Fatalf("Require(prod) = %v", err)
	}
}

// I2 for host kinds: whatever a set admits, its child allows nothing its
// parent does not, for a random mix of exact and subtree kinds.
func TestAttenuateHostKindsNeverWidens(t *testing.T) {
	k := mustKinds(t, KindSpec{Kind: "k8s"}, KindSpec{Kind: "team", Covers: subtree})
	r := rand.New(rand.NewPCG(82, 2))
	scopes := []string{"a", "b", "a/x", "a/y", "a/x/1", "b/x", "ax", "a/"}
	kindsOf := []Kind{"k8s", "team"}
	pick := func() Capability {
		return Capability{kindsOf[r.IntN(2)], scopes[r.IntN(len(scopes))]}
	}
	for i := 0; i < 3000; i++ {
		grants := make([]Capability, 1+r.IntN(3))
		for j := range grants {
			grants[j] = pick()
		}
		parent := NewCapSetKinds(k, grants...)
		reqs := make([]Capability, 1+r.IntN(3))
		for j := range reqs {
			reqs[j] = pick()
		}
		child, err := parent.Attenuate(reqs)
		if err != nil {
			continue
		}
		for _, probe := range kindsOf {
			for _, sc := range scopes {
				c := Capability{probe, sc}
				if child.Allows(c) && !parent.Allows(c) {
					t.Fatalf("parent %v, requests %v: the child allows %s", grants, reqs, c)
				}
			}
		}
	}
}
