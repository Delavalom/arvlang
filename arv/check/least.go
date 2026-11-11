package check

import (
	"net/netip"
	"strings"

	"github.com/delavalom/arvlang/arv/runtime"
)

// least drops each net or env need that another need covers: net("x")
// covers the x:443 that http_get("https://x/") needs, so granting x is
// enough. It decides with runtime.Capability.Covers, the check the runtime
// itself makes, which for these kinds reads nothing outside the program.
// Paths are kept as written, and only exact duplicates are dropped (by
// distinct): whether one path lies under another depends on symlinks,
// and arv check does not look at the filesystem. If two needs cover each
// other, the first is kept.
//
// arv check reads untrusted scripts, so least must not compare every pair.
// It looks up, for each need, only the scopes that could cover it (see
// coverers) and asks Covers about those. A scope coverers missed would
// only keep a need that could have gone, never drop one.
func least(needs []Need) []Need {
	idx := make(map[[2]string]int, len(needs)) // kind, scope: the first need with it
	bits := map[int]bool{}                     // the prefix lengths of net CIDR needs
	for i, n := range needs {
		k := [2]string{string(n.Kind), n.Scope}
		if _, ok := idx[k]; !ok {
			idx[k] = i
		}
		if p, err := netip.ParsePrefix(n.Scope); n.Kind == runtime.Net && err == nil {
			bits[p.Bits()] = true
		}
	}
	var out []Need
	for i, n := range needs {
		if !covered(needs, i, idx, bits) {
			out = append(out, n)
		}
	}
	return out
}

// covers is runtime.Capability.Covers (a variable so a test can count calls).
var covers = runtime.Capability.Covers

func covered(needs []Need, i int, idx map[[2]string]int, bits map[int]bool) bool {
	n := needs[i]
	nc := runtime.Capability{Kind: n.Kind, Scope: n.Scope}
	for _, s := range coverers(n, bits) {
		j, ok := idx[[2]string{string(n.Kind), s}]
		if !ok || j == i {
			continue
		}
		mc := runtime.Capability{Kind: n.Kind, Scope: s}
		if covers(mc, nc) && (j < i || !covers(nc, mc)) {
			return true
		}
	}
	return false
}

// coverers are the scopes that can cover need n, n's own among them (a
// duplicate). For env, only "*" covers another name. For net (see
// runtime's netCovers), a grant covers a request only on the same port or
// on every port, and its host only if it is the same host, "*", a "*."
// suffix of a name, or a CIDR range holding an address or a narrower range:
// one lookup per suffix and per prefix length that some need has.
func coverers(n Need, bits map[int]bool) []string {
	switch n.Kind {
	case runtime.Env:
		return []string{n.Scope, "*"}
	case runtime.Net:
	default:
		return nil
	}
	host, port := splitNetScope(n.Scope)
	hosts := []string{host, "*"}
	var a netip.Addr
	var abits int
	if p, err := netip.ParsePrefix(host); err == nil {
		a, abits = p.Addr(), p.Bits()
	} else if a, err = netip.ParseAddr(host); err == nil {
		abits = a.BitLen()
	} else {
		for i := 0; i < len(host); i++ {
			if host[i] == '.' {
				hosts = append(hosts, "*"+host[i:])
			}
		}
	}
	var out []string
	for _, h := range hosts {
		out = append(out, h)
		if port != "" {
			if strings.Contains(h, ":") {
				out = append(out, "["+h+"]:"+port)
			} else {
				out = append(out, h+":"+port)
			}
		}
	}
	if a.IsValid() {
		for b := range bits {
			if b <= abits {
				if p, err := a.Prefix(b); err == nil {
					out = append(out, p.String()) // a CIDR range takes no port
				}
			}
		}
	}
	return out
}

// splitNetScope splits a net scope, normalised by the runtime, into its
// host and port ("" for every port), as the runtime's splitScope does.
func splitNetScope(s string) (host, port string) {
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

// Quoted is the need in --allow form, quoted for a POSIX shell when it
// holds anything but the characters a grant usually has. Control
// characters are escaped ($'...'), so a scope can never print as more
// than one line.
func (n Need) Quoted() string { return quoteArg(n.String()) }

func quoteArg(s string) string {
	plain := s != ""
	for i := 0; i < len(s); i++ {
		if !safeByte(s[i]) {
			plain = false
			break
		}
	}
	if plain {
		return s
	}
	var b strings.Builder
	b.WriteString("$'")
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' || c == '\'':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 0x20 || c >= 0x7f:
			const hex = "0123456789abcdef"
			b.WriteString(`\x`)
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

func safeByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		strings.IndexByte("_-.,/:=@%+[]", c) >= 0
}
