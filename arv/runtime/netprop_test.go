package runtime

import (
	"net/netip"
	"strings"
	"testing"
)

// netVocab is every net scope shape: names, wildcards, IPs (public and
// internal), CIDR ranges, each with and without a port where allowed.
func netVocab(t *testing.T) []Capability {
	hosts := []string{"*", "*.example.com", "example.com", "api.example.com", "localhost",
		"127.0.0.1", "10.1.2.3", "8.8.8.8", "::1", "fd00::1"}
	cidrs := []string{"10.0.0.0/8", "10.1.0.0/16", "0.0.0.0/0", "::/0", "fd00::/8", "8.8.8.0/24",
		"::ffff:0.0.0.0/96", "::ffff:10.0.0.0/104", "64:ff9b::/96", "2002::/16"}
	var caps []Capability
	for _, h := range hosts {
		for _, port := range []string{"", "443", "8080"} {
			s := h
			if port != "" {
				if strings.Contains(h, ":") {
					s = "[" + h + "]"
				}
				s += ":" + port
			}
			caps = append(caps, mustCap(t, Net, s))
		}
	}
	for _, c := range cidrs {
		caps = append(caps, mustCap(t, Net, c))
	}
	return caps
}

// I2, property form: whenever a parent grant g admits a child grant c
// (g.Covers(c), as Attenuate checks), every request c covers is covered by
// g, and every address c may dial, g may dial too. The vocabulary is small
// enough to check every pair exhaustively. Probes include the shapes a
// spawn request can take besides host:port: portless hosts (every port)
// and CIDR ranges, in IPv4, IPv6, mapped, NAT64 and 6to4 forms.
func TestNetAttenuationNeverGrantsMore(t *testing.T) {
	vocab := netVocab(t)
	var probes []Capability
	var dials []netip.AddrPort
	for _, h := range []string{"example.com", "api.example.com", "localhost", "127.0.0.1",
		"10.1.2.3", "10.200.0.1", "8.8.8.8", "8.8.8.9", "[::1]", "[fd00::1]", "169.254.169.254"} {
		for _, port := range []string{"80", "443", "8080"} {
			probes = append(probes, mustCap(t, Net, h+":"+port))
		}
		probes = append(probes, mustCap(t, Net, h)) // portless: every port
	}
	for _, r := range []string{"10.1.0.0/16", "10.0.0.0/8", "10.1.2.3/32", "0.0.0.0/0", "::/0",
		"fd00::/8", "fd00::/16", "::ffff:0.0.0.0/96", "::ffff:127.0.0.0/104", "64:ff9b::/96",
		"64:ff9b::a01:203/128", "2002::/16", "2002:a01:203::/48", "8.8.8.0/24", "8.8.0.0/16"} {
		probes = append(probes, mustCap(t, Net, r))
	}
	for _, a := range []string{"127.0.0.1", "10.1.2.3", "10.200.0.1", "8.8.8.8", "::1", "fd00::1", "169.254.169.254",
		"::ffff:10.1.2.3", "64:ff9b::a01:203", "2002:a01:203::1"} {
		for _, port := range []uint16{80, 443, 8080} {
			dials = append(dials, netip.AddrPortFrom(netip.MustParseAddr(a), port))
		}
	}
	for _, g := range vocab {
		if !g.Covers(g) {
			t.Errorf("%s does not cover itself", g)
		}
		for _, c := range vocab {
			if !g.Covers(c) {
				continue
			}
			for _, p := range probes {
				if c.Covers(p) && !g.Covers(p) {
					t.Errorf("child %s of %s covers %s, which the parent does not", c, g, p)
				}
			}
			for _, d := range dials {
				if NewCapSet(c).AllowsDial(d) && !NewCapSet(g).AllowsDial(d) {
					t.Errorf("child %s of %s may dial %s, which the parent may not", c, g, d)
				}
			}
		}
	}
}

// FuzzNetScope: normalising is idempotent, a valid scope covers itself,
// and Covers never panics on hand-built scopes.
func FuzzNetScope(f *testing.F) {
	for _, s := range []string{"api.example.com", "*.example.com:443", "[::1]:80", "::1",
		"10.0.0.0/8", "fd00::/8", "*", "*:443", "127.0.0.1:8080", "host:", "[::1]x", "a:b:c", "..", "host..:80",
		"::ffff:10.0.0.0/104", "::ffff:127.0.0.1", "[::ffff:127.0.0.1]:0080", "fe80::1%en0", "10.0.0.1/8",
		"127.0.0.0/8:80", "host:65536", "[fd00::1]:443", "LOCALHOST.:443", "*.example.com.:1"} {
		f.Add(s, "example.com:443")
	}
	f.Fuzz(func(t *testing.T, s, other string) {
		(Capability{Kind: Net, Scope: s}).Covers(Capability{Kind: Net, Scope: other})
		c, err := New(Net, s)
		if err != nil {
			return
		}
		again, err := New(Net, c.Scope)
		if err != nil || again != c {
			t.Fatalf("New(net, %q) = %s, but New of that = %s, %v", s, c, again, err)
		}
		if !c.Covers(c) {
			t.Fatalf("%s does not cover itself", c)
		}
	})
}
