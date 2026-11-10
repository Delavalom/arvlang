package evaluator

import (
	"fmt"
	"net"
	"net/url"

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
