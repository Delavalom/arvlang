package runtime

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// hostScope draws a short scope over an alphabet where the rules'
// separators ('/' for subtree, '.' for domain) are common.
func hostScope(r *rand.Rand) string {
	const alphabet = "ab/.*"
	n := r.IntN(7)
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[r.IntN(len(alphabet))]
	}
	return string(b)
}

// I2 for host kinds, as a property: along random chains of attenuation
// (up to five levels, mixing two host kinds with different rules and a
// built-in kind), every set allows nothing its parent does not, so nothing
// the root does not; Attenuate fails exactly when the parent does not
// admit some request; and a child holds exactly its distinct requests.
func TestPropAttenuateHostKindsNeverGrantsMore(t *testing.T) {
	r := rand.New(rand.NewPCG(82, 22))
	iters := 20000
	if testing.Short() {
		iters = 2000
	}
	for i := 0; i < iters; i++ {
		ra, rb := hostRules[r.IntN(len(hostRules))], hostRules[r.IntN(len(hostRules))]
		k, err := NewKinds(KindSpec{Kind: "ka", Covers: ra.fn}, KindSpec{Kind: "kb", Covers: rb.fn})
		if err != nil {
			t.Fatal(err)
		}
		pick := func() Capability {
			switch r.IntN(5) {
			case 0:
				return Capability{Env, []string{"*", "HOME", "PATH"}[r.IntN(3)]}
			case 1, 2:
				return Capability{"ka", hostScope(r)}
			}
			return Capability{"kb", hostScope(r)}
		}
		var grants []Capability
		for j := 1 + r.IntN(4); j > 0; j-- {
			if c := pick(); c.Scope != "" {
				grants = append(grants, c)
			}
		}
		root := NewCapSetKinds(k, grants...)
		probes := slices.Clone(grants)
		for j := 0; j < 24; j++ {
			probes = append(probes, pick())
		}
		chain := []CapSet{root}
		for depth := 0; depth < 5; depth++ {
			parent := chain[len(chain)-1]
			reqs := make([]Capability, 1+r.IntN(3))
			for j := range reqs {
				// Mostly narrow something the parent holds, sometimes anything.
				if held := parent.List(); len(held) > 0 && r.IntN(3) > 0 {
					c := held[r.IntN(len(held))]
					c.Scope += []string{"", "/a", ".b", "a", "/", "*"}[r.IntN(6)]
					reqs[j] = c
				} else {
					reqs[j] = pick()
				}
			}
			probes = append(probes, reqs...)
			child, err := parent.Attenuate(reqs)
			admitsAll := true
			for _, q := range reqs {
				if _, err := parent.Admit(q); err != nil {
					admitsAll = false
				}
			}
			if (err == nil) != admitsAll {
				t.Fatalf("parent %v, requests %v: Attenuate err = %v, but Admit says all admitted = %v", parent.List(), reqs, err, admitsAll)
			}
			if err != nil {
				break
			}
			if child.Kinds() != k {
				t.Fatal("the child lost the registry")
			}
			for _, c := range child.List() {
				if !slices.Contains(reqs, c) {
					t.Fatalf("requests %v: the child holds %s", reqs, c)
				}
			}
			chain = append(chain, child)
		}
		for _, p := range probes {
			for lvl := 1; lvl < len(chain); lvl++ {
				if chain[lvl].Allows(p) && !chain[lvl-1].Allows(p) {
					t.Fatalf("rules %s/%s: level %d %v allows %s; its parent %v does not",
						ra.name, rb.name, lvl, chain[lvl].List(), p, chain[lvl-1].List())
				}
				if chain[lvl].Allows(p) && !root.Allows(p) {
					t.Fatalf("level %d allows %s; the root %v does not", lvl, p, grants)
				}
			}
		}
	}
}
