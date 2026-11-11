package check

import (
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
func least(needs []Need) []Need {
	var out []Need
	for i, n := range needs {
		if !covered(needs, i) {
			out = append(out, n)
		}
	}
	return out
}

func covered(needs []Need, i int) bool {
	n := needs[i]
	if n.Kind != runtime.Net && n.Kind != runtime.Env {
		return false
	}
	nc := runtime.Capability{Kind: n.Kind, Scope: n.Scope}
	for j, m := range needs {
		if j == i || m.Kind != n.Kind {
			continue
		}
		mc := runtime.Capability{Kind: m.Kind, Scope: m.Scope}
		if mc.Covers(nc) && (j < i || !nc.Covers(mc)) {
			return true
		}
	}
	return false
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
