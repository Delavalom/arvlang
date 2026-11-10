package runtime

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"unicode"
)

// netScope normalises a net scope: a host pattern, optionally followed by
// ":port". The host part takes the form url.Hostname() gives for the same
// host, so a grant can match a request: lower case, one trailing dot
// dropped, IP literals in netip's canonical form (an IPv4-mapped address
// as plain IPv4; a zone is rejected, see ipLiteral). A port is a decimal
// number from 1 to 65535 (leading zeros dropped). An IPv6 address with a
// port is bracketed ("[::1]:443"); without one it is bare ("::1").
//
// A scope without a port covers every port; one with a port covers only
// that port. Requests (http_get and every redirect) always name a port,
// the scheme's default when the URL has none (see Covers).
//
// A CIDR range ("10.0.0.0/8", "fd00::/8") covers the IP addresses in it,
// on every port; an IPv4-mapped range of /96 or longer is written as the
// IPv4 range it holds. Together with IP literals, it is the only kind of grant
// that reaches an internal address (see AddrClass and Task.RequireDial).
//
// It rejects anything that could never match a host (a scheme, a path,
// userinfo, whitespace, an empty or out-of-range port) and wildcards other
// than "*" alone or a leading "*." on a DNS name.
func netScope(s string) (string, error) {
	if p, err := netip.ParsePrefix(s); err == nil {
		if p != p.Masked() {
			return "", fmt.Errorf("host %q: a CIDR range must not set bits past its prefix (did you mean %s?)", s, p.Masked())
		}
		if p.Addr().Is4In6() && p.Bits() >= 96 { // the dial check sees IPv4
			p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
		}
		return p.String(), nil // a CIDR range takes no port
	}
	if strings.ContainsAny(s, "/@?#") || strings.IndexFunc(s, isSpaceOrControl) >= 0 {
		return "", fmt.Errorf("host %q: want a bare host name, optionally with a port (no scheme, path, userinfo or spaces)", s)
	}
	host, port, err := splitPort(s)
	if err != nil {
		return "", err
	}
	h, err := normHost(host, s)
	if err != nil || port == "" {
		return h, err
	}
	if strings.Contains(h, ":") {
		return "[" + h + "]:" + port, nil
	}
	return h + ":" + port, nil
}

// splitPort splits "host:port" or "[v6]:port" into host and normalised
// port. A scope with no port, a bracketed IPv6 address without one, or a
// bare IPv6 address returns port "".
func splitPort(s string) (host, port string, err error) {
	switch {
	case strings.HasPrefix(s, "["):
		end := strings.Index(s, "]")
		if end < 0 || end == len(s)-1 {
			return s, "", nil // normHost checks the brackets
		}
		if s[end+1] != ':' {
			return "", "", fmt.Errorf("host %q: brackets must hold exactly an IPv6 address", s)
		}
		host, port = s[:end+1], s[end+2:]
	case strings.Count(s, ":") == 1:
		host, port, _ = strings.Cut(s, ":")
	default:
		return s, "", nil
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || n == 0 || strings.Trim(port, "0123456789") != "" {
		return "", "", fmt.Errorf("host %q: port %q must be a number from 1 to 65535", s, port)
	}
	return host, strconv.FormatUint(n, 10), nil
}

// normHost normalises the host part of scope s (see netScope).
func normHost(h, s string) (string, error) {
	if h == "*" {
		return h, nil
	}
	h = strings.ToLower(h)
	if strings.HasPrefix(h, "[") {
		inner, ok := strings.CutSuffix(h[1:], "]")
		if a, err := netip.ParseAddr(inner); ok && err == nil && a.Is6() {
			return ipLiteral(a, s)
		}
		return "", fmt.Errorf("host %q: brackets must hold exactly an IPv6 address", s)
	}
	if strings.Contains(h, ":") {
		if a, err := netip.ParseAddr(h); err == nil && a.Is6() {
			return ipLiteral(a, s)
		}
		return "", fmt.Errorf("host %q: not a host name, an IPv6 address or host:port", s)
	}
	h = strings.TrimSuffix(h, ".")
	if h == "" || strings.HasSuffix(h, ".") { // "..": dropping one dot must leave a name
		return "", fmt.Errorf("host %q: empty host", s)
	}
	if a, err := netip.ParseAddr(h); err == nil {
		return ipLiteral(a, s)
	}
	name, wild := strings.CutPrefix(h, "*.")
	if name == "" || strings.Contains(name, "*") {
		return "", fmt.Errorf("host %q: a wildcard must be \"*\" or a leading \"*.\" on a domain", s)
	}
	if wild && isNumeric(name[strings.LastIndex(name, ".")+1:]) {
		// No TLD is all digits, so this is (part of) an IP literal.
		return "", fmt.Errorf("host %q: wildcards cannot cover IP addresses", s)
	}
	return h, nil
}

// ipLiteral normalises an IP address in scope s to the form the dial check
// compares (see dialDenied): an IPv4-mapped IPv6 address becomes its IPv4
// address, so a grant written either way reaches it. A zone is rejected:
// the dial check ignores zones, so a zoned grant could never pass it, and
// dropping the zone would widen the grant to every interface.
func ipLiteral(a netip.Addr, s string) (string, error) {
	if a.Zone() != "" {
		return "", fmt.Errorf("host %q: an IPv6 zone is not allowed in a net scope", s)
	}
	return a.Unmap().String(), nil
}

// splitScope splits a scope already normalised by netScope into its host
// and port ("" for any port).
func splitScope(s string) (host, port string) {
	if strings.HasPrefix(s, "[") {
		if i := strings.LastIndex(s, "]:"); i > 0 {
			return s[1:i], s[i+2:]
		}
		return strings.Trim(s, "[]"), ""
	}
	if strings.Count(s, ":") == 1 {
		host, port, _ = strings.Cut(s, ":")
		return host, port
	}
	return s, ""
}

// netCovers reports whether net grant g covers request r, both normalised.
// A grant without a port covers every port; one with a port covers only a
// request for that port, so it never covers a portless request (which
// stands for every port, e.g. at spawn).
func netCovers(g, r string) bool {
	gh, gp := splitScope(g)
	rh, rp := splitScope(r)
	if gp != "" && gp != rp {
		return false
	}
	return hostCovers(gh, rh)
}

// hostCovers reports whether host pattern g covers host r. A CIDR grant
// covers the addresses and the narrower ranges inside it. An IP address is
// covered by the same address, by a CIDR range holding it, and by "*" only
// if it is public: "*" grants every host name, but no internal address, so
// a child cannot attenuate "*" into an IP grant that reaches one (I2). No
// other pattern covers a CIDR range.
func hostCovers(g, r string) bool {
	if p, err := netip.ParsePrefix(g); err == nil {
		if a, err := netip.ParseAddr(r); err == nil {
			return p.Contains(a)
		}
		q, err := netip.ParsePrefix(r)
		return err == nil && q.Bits() >= p.Bits() && p.Contains(q.Addr())
	}
	if a, err := netip.ParseAddr(r); err == nil {
		return g == r || (g == "*" && AddrClass(a) == "")
	}
	if strings.Contains(r, "/") {
		return false
	}
	if g == "*" || g == r {
		return true
	}
	if strings.HasPrefix(g, "*.") {
		return strings.HasSuffix(r, g[1:]) // "*.x.com" covers "a.x.com", not "x.com"
	}
	return false
}

func isSpaceOrControl(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }

func isNumeric(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}
