package runtime

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestAllowsDial(t *testing.T) {
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
		{[]string{"example.com"}, "93.184.215.14:443", true},
		{[]string{"localhost"}, "127.0.0.1:80", false},
		{[]string{"*"}, "[::1]:80", false},
		{[]string{"*"}, "169.254.169.254:80", false},
		{[]string{"*"}, "[::ffff:127.0.0.1]:80", false},
		{[]string{"localhost", "127.0.0.1"}, "127.0.0.1:80", true},
		{[]string{"127.0.0.1:8080"}, "127.0.0.1:8080", true},
		{[]string{"127.0.0.1:8080"}, "127.0.0.1:9090", false},
		{[]string{"10.0.0.0/8"}, "10.9.9.9:5432", true},
		{[]string{"10.0.0.0/8"}, "[::ffff:10.9.9.9]:5432", true},
		{[]string{"::1"}, "[::1%lo0]:80", true},
	} {
		if got := set(c.held...).AllowsDial(netip.MustParseAddrPort(c.addr)); got != c.ok {
			t.Errorf("%v AllowsDial(%s) = %v, want %v", c.held, c.addr, got, c.ok)
		}
	}
}

func TestRequireDialNamesTheGrantItNeeds(t *testing.T) {
	task := &Task{ID: "root", Caps: NewCapSet(mustCap(t, Net, "localhost"))}
	err := task.RequireDial("localhost:8080", netip.MustParseAddrPort("127.0.0.1:8080"))
	var denied *DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("got %v, want *DeniedError", err)
	}
	for _, frag := range []string{`net("127.0.0.1:8080")`, "in task root", "localhost:8080 reached a loopback address", `holds: [net("localhost")]`} {
		if !strings.Contains(err.Error(), frag) {
			t.Errorf("error %q lacks %q", err, frag)
		}
	}
	if err := task.RequireDial("example.com:443", netip.MustParseAddrPort("93.184.215.14:443")); err != nil {
		t.Errorf("public address refused: %v", err)
	}
}
