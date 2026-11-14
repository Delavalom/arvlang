package runtime

import "net/netip"

// reserved lists internal ranges that netip's predicates do not cover.
var reserved = []struct {
	prefix netip.Prefix
	class  string
}{
	{netip.MustParsePrefix("0.0.0.0/8"), "this-network"}, // 0.0.0.0 reaches localhost on Linux
	{netip.MustParsePrefix("100.64.0.0/10"), "shared (CGNAT)"},
	{netip.MustParsePrefix("192.0.0.0/24"), "IETF protocol"},
	{netip.MustParsePrefix("198.18.0.0/15"), "benchmarking"},
	{netip.MustParsePrefix("240.0.0.0/4"), "reserved"}, // includes 255.255.255.255
	{netip.MustParsePrefix("64:ff9b:1::/48"), "local NAT64"},
	{netip.MustParsePrefix("2001::/32"), "Teredo"},
	{netip.MustParsePrefix("fec0::/10"), "site-local"},
}

var (
	nat64     = netip.MustParsePrefix("64:ff9b::/96") // IPv4 in the last 4 bytes
	sixToFour = netip.MustParsePrefix("2002::/16")    // IPv4 in bytes 2-5
	v4compat  = netip.MustParsePrefix("::/96")        // deprecated IPv4-compatible: IPv4 in the last 4 bytes
)

// AddrClass names the kind of internal address a is, or returns "" if a
// is public. Internal means unspecified, loopback, private (RFC 1918 and
// IPv6 ULA, which holds AWS's fd00:ec2::254), link-local (which holds the
// 169.254.169.254 metadata service), multicast, or one of the reserved
// ranges above. IPv4-mapped IPv6 addresses are classed as their IPv4 form,
// and NAT64, 6to4 and IPv4-compatible (::a.b.c.d) addresses by the IPv4
// address they embed.
func AddrClass(a netip.Addr) string {
	a = a.Unmap()
	switch {
	case !a.IsValid():
		return "invalid"
	case a.IsUnspecified():
		return "unspecified"
	case a.IsLoopback():
		return "loopback"
	case a.IsPrivate():
		return "private"
	case a.IsLinkLocalUnicast():
		return "link-local"
	case a.IsMulticast():
		return "multicast"
	}
	if a.Is6() {
		b := a.As16()
		switch {
		case nat64.Contains(a):
			return AddrClass(netip.AddrFrom4([4]byte(b[12:16])))
		case sixToFour.Contains(a):
			return AddrClass(netip.AddrFrom4([4]byte(b[2:6])))
		case v4compat.Contains(a): // :: and ::1 were classed above
			return AddrClass(netip.AddrFrom4([4]byte(b[12:16])))
		}
	}
	for _, r := range reserved {
		if r.prefix.Contains(a) {
			return r.class
		}
	}
	return ""
}
