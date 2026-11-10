package e2e

import (
	"strings"
	"testing"
)

// Phase 8.3: network hardening (ports, CIDR grants, the dial-time address
// check). Every case runs against the local test server on 127.0.0.1.

// srvPort is the test server's port.
func srvPort() string { return srv.URL[strings.LastIndex(srv.URL, ":")+1:] }

// net=host:port restricts the grant to that port, on the first request and
// on every redirect hop.
func TestNetPorts(t *testing.T) {
	t.Parallel()
	port := srvPort()
	other := "1"
	if port == other {
		other = "2"
	}
	get := func(name, route string, allow []string, stdout string, hitsWant map[string]int) spec {
		s := spec{
			name: name, script: "http_get.arv", vars: map[string]string{"ROUTE": route},
			allow: allow, stdout: stdout,
			verify: func(t *testing.T) {
				for path, n := range hitsWant {
					if got := hits(t, path); got != n {
						t.Errorf("server saw %d requests for %s, want %d", got, path, n)
					}
				}
			},
		}
		if stdout == "" {
			s.code = 1
		}
		return s
	}
	deny := func(s spec, want string) spec {
		s.stderrHas = []string{`capability denied: net("` + want + `")`}
		return s
	}
	runAll(t, []spec{
		get("the granted port is allowed", "/ok", []string{"net=127.0.0.1:" + port},
			"200\n"+okBody+"\n", map[string]int{"/ok": 1}),
		deny(get("another port is denied", "/ok", []string{"net=127.0.0.1:" + other},
			"", map[string]int{"/ok": 0}), "127.0.0.1:"+port),
		get("a redirect to a granted host and port succeeds", "/redirect-out",
			[]string{"net=127.0.0.1:" + port, "net=localhost:" + port},
			"200\n"+targetBody+"\n", map[string]int{"/redirect-out": 1, "/redirect-target": 1}),
		deny(get("a redirect to an ungranted port is denied", "/redirect-out",
			[]string{"net=127.0.0.1:" + port, "net=localhost:" + other},
			"", map[string]int{"/redirect-out": 1, "/redirect-target": 0}), "localhost:"+port),
	})
}

// A CIDR grant covers the IP addresses in it; "*" covers every host name
// but no internal address, so it cannot reach the loopback server.
func TestNetCIDRAndWildcard(t *testing.T) {
	t.Parallel()
	port := srvPort()
	url := "http://127.0.0.1:" + port + "/ok"
	get := func(name string, allow []string, ok bool) spec {
		s := spec{name: name, script: "http_get_url.arv", vars: map[string]string{"URL": url}, allow: allow}
		if ok {
			s.stdout = "200\n" + okBody + "\n"
		} else {
			s.code = 1
			s.stderrHas = []string{`capability denied: net("127.0.0.1:` + port + `")`}
			s.verify = func(t *testing.T) {
				if n := hits(t, "/ok"); n != 0 {
					t.Errorf("server saw %d requests for /ok, want 0", n)
				}
			}
		}
		return s
	}
	runAll(t, []spec{
		get("a CIDR grant covers an address in it", []string{"net=127.0.0.0/8"}, true),
		get("a CIDR grant does not cover an address outside it", []string{"net=10.0.0.0/8"}, false),
		get("the wildcard does not cover a loopback address", []string{"net=*"}, false),
		{
			name: "a CIDR grant with bits past its prefix is rejected", script: "basics.arv",
			allow: []string{"net=127.0.0.1/8"}, code: 2, stderrHas: []string{"did you mean 127.0.0.0/8"},
		},
	})
}

// SSRF / DNS rebinding (acceptance for 8.3): a granted name that resolves to
// an internal address is refused at the dial. "localhost" stands in for a
// rebinding name: the system resolver maps it to loopback from /etc/hosts,
// so no DNS stub is needed. An IP grant for the address is the explicit
// path that lets the e2e suite reach its 127.0.0.1 server.
func TestNetDialChecksResolvedAddress(t *testing.T) {
	t.Parallel()
	port := srvPort()
	get := func(name string, allow []string) spec {
		return spec{
			name: name, script: "http_get_url.arv",
			vars:  map[string]string{"URL": "http://localhost:" + port + "/ok"},
			allow: allow,
		}
	}
	refused := get("a name that resolves to loopback is refused", []string{"net=localhost"})
	refused.code = 1
	refused.stderrHas = []string{"capability denied: net(", "localhost:" + port + " reached a loopback address"}
	sawOK := func(want int) func(t *testing.T) {
		return func(t *testing.T) {
			if n := hits(t, "/ok"); n != want {
				t.Errorf("server saw %d requests for /ok, want %d", n, want)
			}
		}
	}
	refused.verify = sawOK(0)
	allowed := get("an IP grant lets the dial reach it", []string{"net=localhost", "net=127.0.0.1"})
	allowed.stdout = "200\n" + okBody + "\n"
	allowed.verify = sawOK(1) // the counter does see this test's requests
	portOnly := get("an IP grant for another port does not", []string{"net=localhost", "net=127.0.0.1:1"})
	portOnly.code = 1
	portOnly.stderrHas = []string{"reached a loopback address"}
	portOnly.verify = sawOK(0) // refused at the dial, before any request
	runAll(t, []spec{refused, allowed, portOnly})
}
