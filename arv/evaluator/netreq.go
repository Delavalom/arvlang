package evaluator

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// defaultPorts are the ports a URL without one reaches, by scheme.
var defaultPorts = map[string]string{"http": "80", "https": "443"}

// netRequest is the capability a request for u needs: its host and the
// port it will reach (the scheme's default when u names none), normalised
// by runtime.New so it compares with grants. http_get checks it before the
// first request and CheckRedirect before every redirect, so a port grant
// (net("host:443")) holds across redirects too.
func netRequest(u *url.URL) (runtime.Capability, error) {
	port := u.Port()
	if port == "" {
		var ok bool
		if port, ok = defaultPorts[u.Scheme]; !ok {
			return runtime.Capability{}, fmt.Errorf("unsupported URL scheme %q", u.Scheme)
		}
	}
	return runtime.New(runtime.Net, net.JoinHostPort(u.Hostname(), port))
}

// transportFor returns a transport for one http_get by task t. Every
// connection it opens is checked after DNS resolution, on the address it
// actually reaches (net.Dialer.Control runs once per address tried): an
// internal address needs an IP or CIDR grant (runtime.Task.RequireDial),
// so a granted name that resolves, or re-resolves, to one is refused
// (SSRF, DNS rebinding). There is no separate resolve-then-check step for
// a rebinding answer to race.
//
// It is never shared: a pooled connection checked against one task's
// grants must not serve another task. Keep-alives are off, so no idle
// connection (and its goroutines) outlives the call, and no proxy is used,
// since the dial would then reach the proxy and the check would see the
// proxy's address instead of the target's.
func transportFor(t *runtime.Task) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: 30 * time.Second,
				Control: func(_, address string, _ syscall.RawConn) error {
					ap, err := netip.ParseAddrPort(address)
					if err != nil {
						return fmt.Errorf("dial %s: unexpected address %q: %w", addr, address, err)
					}
					return t.RequireDial(addr, ap)
				},
			}
			return d.DialContext(ctx, network, addr)
		},
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
