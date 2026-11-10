package runtime

import (
	"math/rand/v2"
	"net/netip"
	"strings"
	"testing"
)

// gen draws random scopes for the I2 property test.
type gen struct {
	r    *rand.Rand
	root string
}

var (
	pathComps = []string{"g", "a", "b", "f", "file", "in", "up", "esc", "rel", "dang", "loop",
		"chain", "out", "s", "back", "missing", "..", ".", ""}
	netHosts = []string{"*", "*.example.com", "example.com", "api.example.com", "a.api.example.com",
		"localhost", "127.0.0.1", "10.1.2.3", "8.8.8.8", "::1", "fd00::1", "::ffff:10.1.2.3", "169.254.169.254"}
	netCIDRs = []string{"10.0.0.0/8", "10.1.0.0/16", "127.0.0.0/8", "0.0.0.0/0", "::/0", "fd00::/8",
		"8.8.8.0/24", "::ffff:0.0.0.0/96", "::ffff:10.0.0.0/104", "64:ff9b::/96", "2002::/16", "169.254.0.0/16"}
	netPorts = []string{"", "80", "443", "8080"}
	envNames = []string{"*", "HOME", "PATH", "API_KEY"}
	dialAddr = []string{"127.0.0.1", "10.1.2.3", "8.8.8.8", "::1", "fd00::1", "169.254.169.254",
		"::ffff:10.1.2.3", "64:ff9b::a01:203", "2002:a01:203::1", "93.184.216.34"}
)

func (g gen) pick(xs []string) string { return xs[g.r.IntN(len(xs))] }

// scope draws a scope of kind k: a random path under the tree, or a net
// host (with or without a port) or CIDR range, or an env name.
func (g gen) scope(k Kind) string {
	switch k {
	case FSRead, FSWrite:
		n := 1 + g.r.IntN(5)
		parts := make([]string, n)
		for i := range parts {
			parts[i] = g.pick(pathComps)
		}
		return fuzzPath(g.root, strings.Join(parts, "/"))
	case Net:
		if g.r.IntN(4) == 0 {
			return g.pick(netCIDRs)
		}
		h, p := g.pick(netHosts), g.pick(netPorts)
		if p == "" {
			return h
		}
		if strings.Contains(h, ":") {
			h = "[" + h + "]"
		}
		return h + ":" + p
	}
	return g.pick(envNames)
}

// narrow draws a request a child might ask for: often a scope inside one
// the parent holds (a longer path, a port on a host), sometimes any scope.
func (g gen) narrow(k Kind, held []Capability) string {
	if len(held) == 0 || g.r.IntN(3) == 0 {
		return g.scope(k)
	}
	base := held[g.r.IntN(len(held))].Scope
	switch k {
	case FSRead, FSWrite:
		return base + "/" + g.pick(pathComps)
	case Net:
		if _, p := splitScope(base); p == "" && !strings.Contains(base, "/") {
			return base + ":" + g.pick(netPorts[1:])
		}
	}
	return base
}

// I2, property form: for random grant sets of every kind and random
// narrowings of them, every request the child admits the parent admits,
// as the same canonical capability, and every address the child may dial
// the parent may dial. The chain goes one level further (a grandchild), so
// attenuating an attenuated set is covered too. Fixed seeds keep it
// reproducible; FuzzAdmit explores beyond them.
func TestAttenuationNeverGrantsMoreRandom(t *testing.T) {
	root := fuzzTree(t, t.TempDir())
	var dials []netip.AddrPort
	for _, a := range dialAddr {
		for _, p := range []uint16{80, 443, 8080} {
			dials = append(dials, netip.AddrPortFrom(netip.MustParseAddr(a), p))
		}
	}
	admitted := 0
	for seed := uint64(0); seed < 3000; seed++ {
		g := gen{r: rand.New(rand.NewPCG(seed, 0x83)), root: root}
		k := fuzzKinds[g.r.IntN(len(fuzzKinds))]
		var held []Capability
		for range 1 + g.r.IntN(4) {
			if c, err := New(k, g.scope(k)); err == nil {
				held = append(held, c)
			}
		}
		chain := []CapSet{NewCapSet(held...)}
		for len(chain) < 3 {
			parent := chain[len(chain)-1]
			var reqs []Capability
			for range 1 + g.r.IntN(3) {
				reqs = append(reqs, Capability{Kind: k, Scope: g.narrow(k, parent.List())})
			}
			child, err := parent.Attenuate(reqs)
			if err != nil {
				break
			}
			chain = append(chain, child)
		}
		for i := 1; i < len(chain); i++ {
			parent, child := chain[i-1], chain[i]
			for range 40 {
				p := Capability{Kind: k, Scope: g.narrow(k, child.List())}
				pc, err := child.Admit(p)
				if err != nil {
					continue
				}
				admitted++
				if g, _ := parent.Grant(pc); (k == FSRead || k == FSWrite) && !componentsInside(g.Scope, pc.Scope) {
					t.Fatalf("seed %d: %s is admitted under %s, which does not hold it", seed, pc, g)
				}
				if pp, err := parent.Admit(p); err != nil || pp != pc {
					t.Fatalf("seed %d: child %v admits %s as %s; parent %v gives %s, %v",
						seed, child.List(), p, pc, parent.List(), pp, err)
				}
			}
			for _, d := range dials {
				if child.AllowsDial(d) && !parent.AllowsDial(d) {
					t.Fatalf("seed %d: child %v may dial %s; parent %v may not", seed, child.List(), d, parent.List())
				}
			}
		}
	}
	if admitted < 1000 {
		t.Fatalf("only %d child admissions were checked; the generator is too narrow", admitted)
	}
	t.Logf("%d child admissions checked against their parents", admitted)
}
