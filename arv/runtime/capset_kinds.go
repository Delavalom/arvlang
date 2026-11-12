package runtime

// NewCapSetKinds is NewCapSet for a run whose host declared kinds k
// (Phase 8.2): grants and requests of those kinds are checked with k's
// coverage rule. Every set Attenuate derives from it keeps k.
func NewCapSetKinds(k *Kinds, caps ...Capability) CapSet {
	s := NewCapSet(caps...)
	s.kinds = k
	return s
}

// Kinds returns the host kinds s checks (nil if none).
func (s CapSet) Kinds() *Kinds { return s.kinds }

// allowsHost is Allows for a request of a host kind.
func (s CapSet) allowsHost(r Capability) bool {
	for _, c := range s.caps {
		if s.kinds.covers(c, r) {
			return true
		}
	}
	return false
}

// admitHost is AdmitCharged for a request of a host kind: there is
// nothing to canonicalise or charge, so it validates r (Kinds.Literal)
// and checks it. Any failure is a denial naming r.
func (s CapSet) admitHost(r Capability) (Capability, error) {
	c, err := s.kinds.Literal(r.Kind, r.Scope)
	if err != nil || !s.allowsHost(c) {
		return Capability{}, &DeniedError{Want: r, Held: s.List()}
	}
	return c, nil
}

// covers is Capability.Covers for a host kind: g covers r if both are of
// that kind, neither scope is empty, and the kind's Covers (or equality)
// says so.
func (k *Kinds) covers(g, r Capability) bool {
	if k == nil {
		return false
	}
	spec, ok := k.specs[g.Kind]
	if !ok || g.Kind != r.Kind || g.Scope == "" || r.Scope == "" {
		return false
	}
	if spec.Covers == nil {
		return g.Scope == r.Scope
	}
	return spec.Covers(g.Scope, r.Scope)
}
