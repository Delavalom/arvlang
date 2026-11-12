package check

import (
	"errors"
	"net/netip"
	"path/filepath"

	"github.com/delavalom/arvlang/arv/runtime"
)

// grantIndex holds a header's grants by kind and scope, so that Outside
// compares each site with the few grants that can cover it rather than
// with every grant. The header and the body both come from the script that
// arv check inspects, and CapSet.Admit scans every grant (and, on a
// denial, copies them all into its error), so comparing all of one with
// all of the other was quadratic: a 730 KB file stalled arv check, and run
// --manifest before --timeout starts, for 10 s.
type grantIndex struct {
	scopes map[runtime.Kind]map[string]runtime.Capability
	bits   map[int]bool                  // the prefix lengths of net CIDR grants
	lens   map[runtime.Kind]map[int]bool // the lengths of fs grant scopes
}

func newGrantIndex(grants []runtime.Capability) *grantIndex {
	x := &grantIndex{scopes: map[runtime.Kind]map[string]runtime.Capability{}, bits: map[int]bool{},
		lens: map[runtime.Kind]map[int]bool{}}
	for _, g := range grants {
		if x.scopes[g.Kind] == nil {
			x.scopes[g.Kind] = map[string]runtime.Capability{}
			x.lens[g.Kind] = map[int]bool{}
		}
		x.scopes[g.Kind][g.Scope] = g
		x.lens[g.Kind][len(g.Scope)] = true
		if p, err := netip.ParsePrefix(g.Scope); g.Kind == runtime.Net && err == nil {
			x.bits[p.Bits()] = true
		}
	}
	return x
}

// admit is CapSet.Admit's decision (runtime/capability.go, AdmitCharged)
// with Allows answered by the index: it canonicalises r (runtime.New,
// which is what Admit's walk computes when nothing is charged) and allows
// it if some grant covers the result. A path that fails to canonicalise
// at a component some grant covers returns the OS error, as Admit returns
// its *runtime.CanonicalError; any other failure is a plain denial (nil).
// TestOutsideMatchesAdmit holds it to Admit's answer.
func (x *grantIndex) admit(r runtime.Capability) (bool, error) {
	c, err := runtime.New(r.Kind, r.Scope)
	if err == nil {
		return x.allows(c), nil
	}
	var ce *runtime.CanonicalError
	if errors.As(err, &ce) && ce.Resolved != "" && x.allows(runtime.Capability{Kind: r.Kind, Scope: ce.Resolved}) {
		return false, ce.Err
	}
	return false, nil
}

// allows reports whether a grant covers c, a canonical request, deciding
// with Covers (through covers) on the grants that can: for a path, a grant
// of "/", of the path itself, or of one of its ancestors, looked up only
// at the lengths some grant has; for net and env, the scopes coverers
// lists. A candidate missed here would refuse a program the runtime
// admits, never admit one it refuses.
func (x *grantIndex) allows(c runtime.Capability) bool {
	scopes := x.scopes[c.Kind]
	if len(scopes) == 0 {
		return false
	}
	try := func(s string) bool {
		g, ok := scopes[s]
		return ok && covers(g, c)
	}
	switch c.Kind {
	case runtime.FSRead, runtime.FSWrite:
		if try(string(filepath.Separator)) || try(c.Scope) {
			return true
		}
		lens := x.lens[c.Kind]
		for i := 1; i < len(c.Scope); i++ {
			if c.Scope[i] == filepath.Separator && lens[i] && try(c.Scope[:i]) {
				return true
			}
		}
		return false
	}
	for _, s := range coverers(Need{Kind: c.Kind, Scope: c.Scope}, x.bits) {
		if try(s) {
			return true
		}
	}
	return false
}
