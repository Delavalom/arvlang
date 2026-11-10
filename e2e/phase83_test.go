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
