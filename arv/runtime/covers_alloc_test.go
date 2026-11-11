package runtime

import (
	"math/rand/v2"
	"net/netip"
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

// Net Covers allocates nothing either. netip.ParsePrefix and ParseAddr
// each built an error value (about 80 bytes in 2 allocations) whenever the
// grant or the request was a host name, so spawn's Admit and http_get's
// checks churned memory in proportion to the net grants held, uncharged:
// one spawn with 200,000 requests against 960 grants churned 750 MiB under
// alloc_mb: 1 (review round 9).
func TestNetCoversAllocatesNothing(t *testing.T) {
	grants := []string{"api.x.com", "*.x.com", "*", "x.com:443", "10.0.0.0/8", "127.0.0.1", "::1", "[::1]:80", "fd00::/8", "1.2.3"}
	reqs := []string{"api.x.com:443", "a.x.com", "x.com", "127.0.0.1:80", "10.1.2.3", "8.8.8.8:53", "[::1]:80", "fd00::/16", "10.0.0.0/16", "1.2.3:80", "1.2.3"}
	for _, g := range grants {
		gc := Capability{Kind: Net, Scope: g}
		for _, r := range reqs {
			rc := Capability{Kind: Net, Scope: r}
			if n := testing.AllocsPerRun(100, func() { gc.Covers(rc) }); n != 0 {
				t.Errorf("net(%q) covers %q allocates %v times", g, r, n)
			}
		}
	}
	var held []Capability
	for i := 0; i < 1000; i++ {
		held = append(held, Capability{Kind: Net, Scope: strings.Repeat("h", i%200+1) + ".com"})
	}
	s := NewCapSet(held...)
	for _, r := range []string{"127.0.0.1:80", "a.org:443", "1.2.3"} {
		rc := Capability{Kind: Net, Scope: r}
		if n := testing.AllocsPerRun(10, func() { s.Allows(rc); s.Grant(rc) }); n != 0 {
			t.Errorf("Allows and Grant over 1000 net grants for %q allocate %v times", r, n)
		}
	}
}

// The same answers as hostCovers gave when it parsed every grant and
// request with netip.
func TestNetCoversAnswers(t *testing.T) {
	for _, tt := range []struct {
		g, r string
		want bool
	}{
		{"*", "a.com", true}, {"*", "8.8.8.8", true}, {"*", "127.0.0.1", false}, {"*", "10.0.0.0/8", false},
		{"*.x.com", "a.x.com", true}, {"*.x.com", "x.com", false}, {"*.x.com", "a.x.com:80", true},
		{"x.com", "x.com", true}, {"x.com", "y.com", false}, {"x.com:443", "x.com:443", true},
		{"x.com:443", "x.com:80", false}, {"x.com:443", "x.com", false},
		{"10.0.0.0/8", "10.1.2.3:80", true}, {"10.0.0.0/8", "11.0.0.1", false},
		{"10.0.0.0/8", "10.1.0.0/16", true}, {"10.1.0.0/16", "10.0.0.0/8", false},
		{"10.0.0.0/8", "a.com", false}, {"127.0.0.1", "127.0.0.1:80", true},
		{"127.0.0.1", "127.0.0.2", false}, {"::1", "[::1]:80", true}, {"fd00::/8", "fd00::1", true},
		{"fd00::/8", "fd00::/16", true}, {"1.2.3", "1.2.3:80", true}, {"1.2.3", "1.2.4", false},
		{"*", "1.2.3", true}, {"a.com", "10.0.0.0/8", false}, {"*.x.com", "10.0.0.0/8", false},
	} {
		got := Capability{Kind: Net, Scope: tt.g}.Covers(Capability{Kind: Net, Scope: tt.r})
		if got != tt.want {
			t.Errorf("net(%q) covers %q = %v, want %v", tt.g, tt.r, got, tt.want)
		}
		if got := NewCapSet(Capability{Kind: Net, Scope: tt.g}).Allows(Capability{Kind: Net, Scope: tt.r}); got != tt.want {
			t.Errorf("Allows: net(%q) covers %q = %v, want %v", tt.g, tt.r, got, tt.want)
		}
	}
}

// isIPv4 accepts exactly the IPv4 strings netip.ParseAddr does, so
// skipping ParseAddr when it is false changes no answer.
func TestIsIPv4MatchesNetip(t *testing.T) {
	check := func(s string) {
		a, err := netip.ParseAddr(s)
		if want := err == nil && a.Is4(); isIPv4(s) != want {
			t.Errorf("isIPv4(%q) = %v, netip says %v", s, !want, want)
		}
	}
	for _, s := range []string{"", "1.2.3.4", "0.0.0.0", "255.255.255.255", "256.1.1.1", "01.2.3.4",
		"1.2.3", "1.2.3.4.5", "1..3.4", ".1.2.3", "1.2.3.", "1.2.3.4 ", "1000.1.1.1", "a.b.c.d", "1.2.3.04", "10.0.0.0/8"} {
		check(s)
	}
	r := rand.New(rand.NewPCG(1, 2))
	const alphabet = "0123456789.25"
	for i := 0; i < 200000; i++ {
		b := make([]byte, r.IntN(16))
		for j := range b {
			b[j] = alphabet[r.IntN(len(alphabet))]
		}
		check(string(b))
	}
}
