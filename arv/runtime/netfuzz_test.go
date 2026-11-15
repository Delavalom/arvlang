package runtime

import (
	"net"
	"net/netip"
	"strconv"
	"testing"
)

// FuzzNetCovers: for any three valid net scopes g, c and p, coverage is
// transitive (g covers c and c covers p imply g covers p), so a child
// attenuated from g to c never reaches a request g refuses (I2), and a
// child holding c never dials an address g may not dial. Ports parse to
// 1-65535 and a host:port grant covers exactly that port of its host.
func FuzzNetCovers(f *testing.F) {
	for _, s := range [][3]string{
		{"*", "example.com:443", "example.com:443"},
		{"*", "127.0.0.1", "127.0.0.1:80"},
		{"10.0.0.0/8", "10.1.0.0/16", "10.1.2.3:8080"},
		{"::ffff:10.0.0.0/104", "10.0.0.1", "[::ffff:10.0.0.1]:80"},
		{"0.0.0.0/0", "::ffff:0.0.0.0/96", "[::ffff:1.2.3.4]:1"},
		{"*.example.com", "*.a.example.com:443", "x.a.example.com:443"},
		{"[::1]:443", "::1", "[::1]:0443"},
		{"64:ff9b::/96", "64:ff9b::a00:1", "10.0.0.1:80"},
		{"localhost.", "LOCALHOST:65535", "localhost:65535"},
		{"fe80::1%en0", "fe80::/10", "[fe80::1]:22"},
	} {
		f.Add(s[0], s[1], s[2], []byte{10, 0, 0, 1}, uint16(80))
	}
	f.Fuzz(func(t *testing.T, gs, cs, ps string, addr []byte, port uint16) {
		g, err1 := New(Net, gs)
		c, err2 := New(Net, cs)
		p, err3 := New(Net, ps)
		if err1 == nil {
			checkPort(t, g)
		}
		if err1 != nil || err2 != nil || !g.Covers(c) {
			return
		}
		if err3 == nil && c.Covers(p) && !g.Covers(p) {
			t.Fatalf("%s covers %s, which covers %s, but %s does not", g, c, p, g)
		}
		if a, ok := netip.AddrFromSlice(addr); ok {
			d := netip.AddrPortFrom(a, port)
			if NewCapSet(c).AllowsDial(d) && !NewCapSet(g).AllowsDial(d) {
				t.Fatalf("child %s of %s may dial %s, which the parent may not", c, g, d)
			}
		}
	})
}

// checkPort: a normalised scope with a port covers its host on that port,
// not on another, and not portless (every port).
func checkPort(t *testing.T, g Capability) {
	t.Helper()
	host, port := splitScope(g.Scope)
	if port == "" {
		return
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
		t.Fatalf("%s has port %q", g, port)
	}
	if host == "*" || host[0] == '*' {
		host = "x" + host[1:] // a name the wildcard covers
		if host == "x" {
			host = "example.com"
		}
	}
	same := Capability{Kind: Net, Scope: net.JoinHostPort(host, port)}
	other := Capability{Kind: Net, Scope: net.JoinHostPort(host, strconv.Itoa(n%65535+1))}
	if !g.Covers(same) || g.Covers(other) || g.Covers(Capability{Kind: Net, Scope: host}) {
		t.Fatalf("%s: covers %s = %v (want true), %s = %v, %s = %v (want false)",
			g, same, g.Covers(same), other, g.Covers(other), host, g.Covers(Capability{Kind: Net, Scope: host}))
	}
}
