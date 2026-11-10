package runtime

import (
	"net/netip"
	"testing"
)

func TestAddrClass(t *testing.T) {
	for in, want := range map[string]string{
		"8.8.8.8": "", "2606:4700::1111": "", "64:ff9b::808:808": "", "2002:808:808::1": "",
		"127.0.0.1": "loopback", "127.1.2.3": "loopback", "::1": "loopback",
		"::ffff:127.0.0.1": "loopback", "64:ff9b::7f00:1": "loopback", "2002:7f00:1::": "loopback",
		"10.1.2.3": "private", "172.16.0.1": "private", "192.168.1.1": "private",
		"fd00:ec2::254": "private", "169.254.169.254": "link-local", "fe80::1": "link-local",
		"0.0.0.0": "unspecified", "::": "unspecified", "0.1.2.3": "this-network",
		"224.0.0.1": "multicast", "ff02::1": "multicast", "100.100.100.200": "shared (CGNAT)",
		"192.0.0.192": "IETF protocol", "198.18.0.1": "benchmarking",
		"255.255.255.255": "reserved", "fec0::1": "site-local", "2001::1": "Teredo",
	} {
		if got := AddrClass(netip.MustParseAddr(in)); got != want {
			t.Errorf("AddrClass(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestNetScopeCIDR(t *testing.T) {
	for in, want := range map[string]string{
		"10.0.0.0/8": "10.0.0.0/8", "FD00::/8": "fd00::/8", "0.0.0.0/0": "0.0.0.0/0",
		"10.1.2.3/32": "10.1.2.3/32",
	} {
		c, err := New(Net, in)
		if err != nil || c.Scope != want {
			t.Errorf("New(net, %q) = %q, %v; want %q", in, c.Scope, err, want)
		}
	}
	for _, in := range []string{"10.0.0.1/8", "10.0.0.0/33", "10.0.0.0/8:443", "[fd00::]/8", "host/8", "10.0.0.0/ 8"} {
		if c, err := New(Net, in); err == nil {
			t.Errorf("New(net, %q) = %s, want an error", in, c)
		}
	}
}

func TestNetCoverageCIDRAndInternal(t *testing.T) {
	for _, c := range []struct {
		held, want string
		ok         bool
	}{
		{"10.0.0.0/8", "10.1.2.3:443", true},
		{"10.0.0.0/8", "10.1.2.3", true},
		{"10.0.0.0/8", "10.1.0.0/16", true},
		{"10.1.0.0/16", "10.0.0.0/8", false},
		{"10.0.0.0/8", "10.0.0.0/8", true},
		{"10.0.0.0/8", "10.0.0.0/7", false}, // one bit wider: also 11.0.0.0/8
		{"10.0.0.0/8", "10.0.0.0/9", true},
		{"127.0.0.0/16", "127.0.0.0/15", false},
		{"10.1.2.3/32", "10.1.2.2/31", false},
		{"fd00::/8", "fc00::/7", false},
		{"fd00::/8", "fd00::/9", true},
		{"10.0.0.0/8", "11.0.0.1:80", false},
		{"10.0.0.0/8", "ten.example:80", false},
		{"fd00::/8", "[fd00::1]:80", true},
		{"*", "8.8.8.8:443", true},
		{"*", "127.0.0.1:80", false},
		{"*", "10.0.0.0/8", false},
		{"*", "8.8.8.0/24", false},
		{"*:443", "169.254.169.254:443", false},
		{"127.0.0.1", "127.0.0.1:8080", true},
		{"localhost", "127.0.0.1:80", false},
	} {
		if got := mustCap(t, Net, c.held).Covers(mustCap(t, Net, c.want)); got != c.ok {
			t.Errorf("net(%q) covers net(%q) = %v, want %v", c.held, c.want, got, c.ok)
		}
	}
}
