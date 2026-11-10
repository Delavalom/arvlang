package runtime

import "testing"

func TestNetScopePorts(t *testing.T) {
	for in, want := range map[string]string{
		"API.Example.com:443": "api.example.com:443",
		"localhost.:8080":     "localhost:8080",
		"127.0.0.1:0080":      "127.0.0.1:80",
		"[::1]:443":           "[::1]:443",
		"[0:0::1]:443":        "[::1]:443",
		"*:443":               "*:443",
		"*.Example.com:443":   "*.example.com:443",
		"host:65535":          "host:65535",
	} {
		c, err := New(Net, in)
		if err != nil || c.Scope != want {
			t.Errorf("New(net, %q) = %q, %v; want %q", in, c.Scope, err, want)
		}
	}
	for _, in := range []string{
		"host:", ":80", "host:0", "host:65536", "host:+80", "host:-1", "host:8a",
		"host:80:81", "[::1]:", "[::1]80", "[::1]:0", "a:b:c", "*.0.0.1:80", "host :80",
		"..", "host..", "..:80", // found by FuzzNetScope: ".." normalised to "."
	} {
		if c, err := New(Net, in); err == nil {
			t.Errorf("New(net, %q) = %s, want an error", in, c)
		}
	}
}

// A grant without a port covers every port; a grant with one covers only
// requests for that port. A portless request stands for every port (as at
// spawn), so only a portless grant covers it (I2).
func TestNetPortCoverage(t *testing.T) {
	for _, c := range []struct {
		held, want string
		ok         bool
	}{
		{"example.com", "example.com:443", true},
		{"example.com", "example.com", true},
		{"example.com:443", "example.com:443", true},
		{"example.com:443", "example.com:80", false},
		{"example.com:443", "example.com", false},
		{"example.com:443", "other.com:443", false},
		{"*.example.com:443", "api.example.com:443", true},
		{"*.example.com:443", "api.example.com:8443", false},
		{"*:443", "anything.io:443", true},
		{"*:443", "anything.io:80", false},
		{"*", "anything.io:80", true},
		{"[::1]:8080", "::1", false},
		{"::1", "[::1]:8080", true},
		{"127.0.0.1:8080", "127.0.0.1:8080", true},
	} {
		if got := mustCap(t, Net, c.held).Covers(mustCap(t, Net, c.want)); got != c.ok {
			t.Errorf("net(%q) covers net(%q) = %v, want %v", c.held, c.want, got, c.ok)
		}
	}
}

// I2: a child cannot widen a port grant into every port by asking for the
// bare host at spawn.
func TestAttenuateKeepsPorts(t *testing.T) {
	parent := NewCapSet(mustCap(t, Net, "api.example.com:443"))
	if _, err := parent.Attenuate([]Capability{mustCap(t, Net, "api.example.com")}); err == nil {
		t.Error("a portless request was admitted from a port grant")
	}
	if _, err := parent.Attenuate([]Capability{mustCap(t, Net, "api.example.com:443")}); err != nil {
		t.Errorf("the same port was refused: %v", err)
	}
}
