package runtime

import (
	"fmt"
	"net/netip"
)

// AllowsDial reports whether s may connect to addr, the address a dial
// actually reached after DNS resolution. The request's host name has
// already been checked (http_get, CheckRedirect); a public address needs
// nothing more. An internal address (see AddrClass) needs a net grant for
// that IP address or a CIDR range holding it, with a matching port if the
// grant names one: a host name grant, even "*", never reaches one. So a
// granted name that resolves, or re-resolves, to an internal address
// (DNS rebinding, SSRF) is refused at the dial.
func (s CapSet) AllowsDial(addr netip.AddrPort) bool {
	return dialDenied(s, addr) == nil
}

// dialDenied returns the capability addr would need, or nil if s may dial it.
func dialDenied(s CapSet, addr netip.AddrPort) *Capability {
	a := addr.Addr().Unmap().WithZone("")
	if AddrClass(a) == "" {
		return nil
	}
	want := Capability{Kind: Net, Scope: netip.AddrPortFrom(a, addr.Port()).String()}
	if s.Allows(want) {
		return nil
	}
	return &want
}

// RequireDial is the dial-time check (see AllowsDial). dialed is what the
// request asked for ("host:port"); the error names the capability that
// would allow addr and says which kind of internal address it is.
func (t *Task) RequireDial(dialed string, addr netip.AddrPort) error {
	want := dialDenied(t.Caps, addr)
	if want == nil {
		return nil
	}
	return &DeniedError{
		Task: t.ID, Want: *want, Held: t.Caps.List(),
		Note: fmt.Sprintf("%s reached a %s address, which only an IP or CIDR grant covers",
			dialed, AddrClass(addr.Addr())),
	}
}
