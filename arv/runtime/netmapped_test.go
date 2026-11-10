package runtime

import (
	"net/netip"
	"testing"
)

// An IPv4-mapped IPv6 grant used to keep its "::ffff:" form while the dial
// check unmaps the address it reaches, so the grant could never pass the
// dial. Mapped addresses and ranges are normalised to their IPv4 form, and
// zones (which the dial check cannot match) are rejected.
func TestNetScopeMappedAndZoned(t *testing.T) {
	for in, want := range map[string]string{
		"::ffff:127.0.0.1":        "127.0.0.1",
		"[::ffff:127.0.0.1]":      "127.0.0.1",
		"[::ffff:127.0.0.1]:8080": "127.0.0.1:8080",
		"::FFFF:7f00:1":           "127.0.0.1",
		"::ffff:10.0.0.0/104":     "10.0.0.0/8",
		"::ffff:0.0.0.0/96":       "0.0.0.0/0",
		"::ffff:10.1.2.3/128":     "10.1.2.3/32",
	} {
		c, err := New(Net, in)
		if err != nil || c.Scope != want {
			t.Errorf("New(net, %q) = %q, %v; want %q", in, c.Scope, err, want)
		}
	}
	for _, in := range []string{"fe80::1%en0", "[fe80::1%en0]", "[fe80::1%en0]:80", "fe80::1%25en0"} {
		if c, err := New(Net, in); err == nil {
			t.Errorf("New(net, %q) = %s, want an error", in, c)
		}
	}
}

// A grant written in mapped form reaches the address it names at the dial,
// whichever form the dialer reports.
func TestMappedGrantPassesTheDial(t *testing.T) {
	for _, g := range []string{"::ffff:127.0.0.1", "[::ffff:127.0.0.1]:8080", "::ffff:127.0.0.0/104"} {
		s := NewCapSet(mustCap(t, Net, g))
		for _, a := range []string{"127.0.0.1:8080", "[::ffff:127.0.0.1]:8080"} {
			if !s.AllowsDial(netip.MustParseAddrPort(a)) {
				t.Errorf("net(%q) refuses the dial to %s", g, a)
			}
		}
		if !s.Allows(mustCap(t, Net, "[::ffff:127.0.0.1]:8080")) {
			t.Errorf("net(%q) refuses the request it names", g)
		}
	}
}
