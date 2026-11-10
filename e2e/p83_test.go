package e2e

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Phase 8.3, net hardening through the real binary: attenuation at spawn
// (I2) with ports, CIDR ranges and the wildcard, the dial check in a child
// task, and scope parsing of capability values. phase83_test.go has the
// CLI grant cases; this file adds what a script can do with them.

// spawnGet fetches url from a child holding caps (an Arv list literal),
// under the root grants allow. ok means the child's fetch succeeds.
func spawnGet(name string, allow []string, caps, url string, ok bool, stderrHas ...string) spec {
	s := spec{
		name: name, script: "p83_spawn_get.arv", allow: allow,
		vars: map[string]string{"URL": url, "CAPS": caps},
	}
	if ok {
		s.stdout = "200\n"
		s.verify = okHitsWant(1)
	} else {
		s.code = 1
		s.stderrHas = stderrHas
		s.verify = okHitsWant(0)
	}
	return s
}

func okHitsWant(n int) func(t *testing.T) {
	return func(t *testing.T) {
		if got := hits(t, "/ok"); got != n {
			t.Errorf("server saw %d requests for /ok, want %d", got, n)
		}
	}
}

// I2 at spawn: a child gets only what its parent's grants cover, with the
// port and address rules of 8.3; a refused spawn runs nothing.
func TestP83SpawnNetAttenuation(t *testing.T) {
	t.Parallel()
	port := srvPort()
	other := "1"
	if port == other {
		other = "2"
	}
	ipURL := "http://127.0.0.1:" + port + "/ok"
	nameURL := "http://localhost:" + port + "/ok"
	denied := func(want string) string { return `spawn in task root: capability denied: net("` + want + `")` }
	runAll(t, []spec{
		spawnGet("a portless parent grant covers a child port grant", []string{"net=127.0.0.1"},
			`[net("127.0.0.1:`+port+`")]`, ipURL, true),
		spawnGet("a port grant does not cover a portless child (every port)", []string{"net=127.0.0.1:" + port},
			`[net("127.0.0.1")]`, ipURL, false, denied("127.0.0.1")),
		spawnGet("a port grant does not cover another port", []string{"net=127.0.0.1:" + port},
			`[net("127.0.0.1:`+other+`")]`, ipURL, false, denied("127.0.0.1:"+other)),
		spawnGet("the wildcard cannot be narrowed to an internal address", []string{"net=*"},
			`[net("127.0.0.1")]`, ipURL, false, denied("127.0.0.1")),
		spawnGet("the wildcard cannot be narrowed to a CIDR range", []string{"net=*"},
			`[net("0.0.0.0/0")]`, ipURL, false, denied("0.0.0.0/0")),
		spawnGet("a CIDR grant covers an address in it", []string{"net=127.0.0.0/8"},
			`[net("127.0.0.1:`+port+`")]`, ipURL, true),
		spawnGet("a CIDR grant covers a narrower range", []string{"net=127.0.0.0/8"},
			`[net("127.0.0.0/16")]`, ipURL, true),
		spawnGet("a CIDR grant does not cover a wider range", []string{"net=127.0.0.0/16"},
			`[net("127.0.0.0/8")]`, ipURL, false, denied("127.0.0.0/8")),
		spawnGet("a CIDR grant does not cover an address outside it", []string{"net=10.0.0.0/8"},
			`[net("127.0.0.1")]`, ipURL, false, denied("127.0.0.1")),
		spawnGet("an IPv4-mapped child grant is the IPv4 address", []string{"net=127.0.0.1"},
			`[net("::ffff:127.0.0.1")]`, ipURL, true),
		// The parent holds the name and the IP; the child keeps only the
		// name, so its own dial to loopback is refused.
		spawnGet("a child without the IP grant is refused at the dial", []string{"net=localhost", "net=127.0.0.1"},
			`[net("localhost")]`, nameURL, false,
			"capability denied: net(", "localhost:"+port+" reached a loopback address"),
		spawnGet("a child with the name and the IP reaches it", []string{"net=localhost", "net=127.0.0.1"},
			`[net("localhost:`+port+`"), net("127.0.0.1:`+port+`")]`, nameURL, true),
	})
}

// Scope parsing of capability values: valid forms normalise, malformed
// ones are Arv errors that stop the program.
func TestP83NetValues(t *testing.T) {
	t.Parallel()
	val := func(scope, out string) spec {
		return spec{name: scope, script: "p83_net_value.arv", vars: map[string]string{"SCOPE": scope}, stdout: out}
	}
	bad := func(scope, why string) spec {
		return spec{
			name: scope, script: "p83_net_value.arv", vars: map[string]string{"SCOPE": scope},
			code: 1, stderrHas: []string{why}, stderrNot: goCrash,
		}
	}
	runAll(t, []spec{
		val("LOCALHOST.:0443", `net("localhost:443")`+"\n"),
		val("::ffff:127.0.0.1", `net("127.0.0.1")`+"\n"),
		val("[::FFFF:127.0.0.1]:80", `net("127.0.0.1:80")`+"\n"),
		val("[FD00::1]:443", `net("[fd00::1]:443")`+"\n"),
		val("::ffff:10.0.0.0/104", `net("10.0.0.0/8")`+"\n"),
		val("fd00::/8", `net("fd00::/8")`+"\n"),
		val("*:443", `net("*:443")`+"\n"),
		bad("10.0.0.1/8", "did you mean 10.0.0.0/8"),
		bad("10.0.0.0/33", "want a bare host name"),
		bad("127.0.0.0/8:80", "want a bare host name"),
		bad("fe80::1%en0", "zone is not allowed"),
		bad("[fe80::1%en0]:80", "zone is not allowed"),
		bad("host:0", "must be a number from 1 to 65535"),
		bad("host:65536", "must be a number from 1 to 65535"),
		bad("host:-1", "must be a number from 1 to 65535"),
		bad("host:+80", "must be a number from 1 to 65535"),
		bad("[::1]x", "brackets must hold exactly an IPv6 address"),
		bad("a:b:c", "not a host name"),
		bad("host..", "empty host"),
		bad("*.10.0.0.1", "wildcards cannot cover IP addresses"),
	})
}

// redirector is a second local server for one test: /r/HOSTPORT/PATH
// answers 302 to http://HOSTPORT/PATH?QUERY. It counts its requests.
func redirector(t *testing.T) (url string, count func() int) {
	t.Helper()
	var mu sync.Mutex
	n := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		mu.Unlock()
		to := "http://" + strings.TrimPrefix(r.URL.EscapedPath(), "/r/") + "?" + r.URL.RawQuery
		http.Redirect(w, r, to, http.StatusFound)
	}))
	t.Cleanup(s.Close)
	return s.URL, func() int { mu.Lock(); defer mu.Unlock(); return n }
}

// Every redirect hop is checked like the first request: the host and the
// port it names (the scheme default when it names none), and then the
// address the dial reaches.
func TestP83RedirectHops(t *testing.T) {
	t.Parallel()
	rURL, rCount := redirector(t)
	rPort := rURL[strings.LastIndex(rURL, ":")+1:]
	port := srvPort()
	t.Cleanup(func() { // after the parallel subtests
		if n := rCount(); n < 6 {
			t.Errorf("redirector saw %d requests, want at least 6", n)
		}
	})
	via := func(name, to string, allow []string, ok bool, stderrHas ...string) spec {
		s := spec{
			name: name, script: "http_get_url.arv",
			vars:  map[string]string{"URL": rURL + "/r/" + to + "/ok"},
			allow: append([]string{"net=127.0.0.1:" + rPort}, allow...),
		}
		want := 0
		if ok {
			s.stdout, want = "200\n"+okBody+"\n", 1
		} else {
			s.code, s.stderrHas = 1, stderrHas
		}
		s.verify = okHitsWant(want)
		return s
	}
	runAll(t, []spec{
		via("a hop to a granted name that resolves to loopback is refused at the dial",
			"localhost:"+port, []string{"net=localhost"}, false, "localhost:"+port+" reached a loopback address"),
		via("an IP grant for the hop's port lets it through",
			"localhost:"+port, []string{"net=localhost", "net=127.0.0.1:" + port}, true),
		via("the wildcard does not let a hop reach a loopback literal",
			"127.0.0.1:"+port, []string{"net=*"}, false, `capability denied: net("127.0.0.1:`+port+`")`),
		via("a hop without a port needs a grant for the scheme default",
			"localhost", []string{"net=localhost:" + port, "net=127.0.0.1"}, false,
			`capability denied: net("localhost:80")`),
		via("a hop to an IPv4-mapped literal is checked as IPv4",
			"[::ffff:127.0.0.1]:"+port, []string{"net=127.0.0.1:" + port}, true),
		via("a hop to a zoned address is refused",
			"[fe80::1%25lo0]:"+port, []string{"net=fe80::/10"}, false, "zone is not allowed"),
	})
}

// A connect to 0.0.0.0 lands on this host's loopback, so a grant for
// 0.0.0.0 (or 0.0.0.0/8) alone must not reach the loopback server; it
// needs the loopback grant too. Before this fix net=0.0.0.0 fetched it.
func TestP83UnspecifiedAddressNeedsLoopbackGrant(t *testing.T) {
	t.Parallel()
	port := srvPort()
	get := func(name string, allow []string, ok bool) spec {
		s := spec{
			name: name, script: "http_get_url.arv", allow: allow,
			vars: map[string]string{"URL": "http://0.0.0.0:" + port + "/ok"},
		}
		if ok {
			s.stdout, s.verify = "200\n"+okBody+"\n", okHitsWant(1)
		} else {
			s.code, s.verify = 1, okHitsWant(0)
			s.stderrHas = []string{`capability denied: net("127.0.0.1:` + port + `")`,
				"0.0.0.0:" + port + " reached an unspecified address"}
		}
		return s
	}
	runAll(t, []spec{
		get("a grant for 0.0.0.0 alone is refused", []string{"net=0.0.0.0"}, false),
		get("a grant for 0.0.0.0/8 alone is refused", []string{"net=0.0.0.0/8"}, false),
		get("with the loopback grant it is allowed", []string{"net=0.0.0.0", "net=127.0.0.1:" + port}, true),
	})
}
