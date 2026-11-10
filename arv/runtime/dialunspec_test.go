package runtime

import (
	"net/netip"
	"strings"
	"testing"
)

// A connect to the unspecified address (0.0.0.0, ::) reaches this host's
// loopback interface on Linux and macOS, so it needs the grant a loopback
// dial needs: an IP grant for 0.0.0.0 alone, or a CIDR range holding it,
// must not open every local service. Found by e2e probing in Phase 8.3:
// net=0.0.0.0 fetched http://0.0.0.0:PORT from a server bound to
// 127.0.0.1 only.
func TestDialToUnspecifiedNeedsALoopbackGrant(t *testing.T) {
	set := func(scopes ...string) CapSet {
		var caps []Capability
		for _, s := range scopes {
			caps = append(caps, mustCap(t, Net, s))
		}
		return NewCapSet(caps...)
	}
	for _, c := range []struct {
		held []string
		addr string
		ok   bool
	}{
		{[]string{"0.0.0.0"}, "0.0.0.0:80", false},
		{[]string{"0.0.0.0:80"}, "0.0.0.0:80", false},
		{[]string{"0.0.0.0/8"}, "0.0.0.0:80", false},
		{[]string{"::"}, "[::]:80", false},
		{[]string{"::ffff:0.0.0.0"}, "[::ffff:0.0.0.0]:80", false},
		{[]string{"0.0.0.0", "127.0.0.1"}, "0.0.0.0:80", true},
		{[]string{"127.0.0.1:80"}, "0.0.0.0:80", true},
		{[]string{"127.0.0.1:80"}, "0.0.0.0:81", false},
		{[]string{"127.0.0.0/8"}, "0.0.0.0:80", true},
		{[]string{"::1"}, "[::]:80", true},
		{[]string{"::1"}, "0.0.0.0:80", false},
		{[]string{"0.0.0.0/0"}, "0.0.0.0:80", true}, // holds 127.0.0.1 too
		{[]string{"::/0"}, "[::]:80", true},
	} {
		if got := set(c.held...).AllowsDial(netip.MustParseAddrPort(c.addr)); got != c.ok {
			t.Errorf("%v AllowsDial(%s) = %v, want %v", c.held, c.addr, got, c.ok)
		}
	}
	task := &Task{ID: "root", Caps: set("0.0.0.0")}
	err := task.RequireDial("0.0.0.0:8080", netip.MustParseAddrPort("0.0.0.0:8080"))
	if err == nil || !strings.Contains(err.Error(), `net("127.0.0.1:8080")`) ||
		!strings.Contains(err.Error(), "reached an unspecified address") {
		t.Fatalf("got %v, want a denial naming the loopback grant", err)
	}
}
