package runtime

import (
	"errors"
	"fmt"
	"slices"
)

// KindSpec declares a capability kind of a Go host's own (Phase 8.2), for
// the host's custom effects. Covers reports whether a grant's scope covers
// a request's scope; nil means they must be equal. A host's Covers must be
// reflexive and transitive, as exact match is: attenuation (spawn ... with)
// checks each request against the parent's grants, so a child can hold
// only what the parent's grants cover, and transitivity is what makes
// everything the child's grants cover covered by the parent's too (I2).
// Covers is called from many tasks at once, so it must be safe for
// concurrent use; it should also be cheap and must not block, since every
// check of the kind runs it.
type KindSpec struct {
	Kind   Kind
	Covers func(grant, request string) bool
}

// MaxKindLen bounds the name of a host kind.
const MaxKindLen = 64

// Kinds is an immutable registry of host kinds. A nil *Kinds declares
// none, so every method is safe on nil.
type Kinds struct {
	specs map[Kind]KindSpec
}

// NewKinds validates specs and returns their registry. A kind's name is
// a lower-case letter, then lower-case letters, digits or '_', at most
// MaxKindLen bytes; it may not be a built-in kind or appear twice.
func NewKinds(specs ...KindSpec) (*Kinds, error) {
	k := &Kinds{specs: make(map[Kind]KindSpec, len(specs))}
	for _, s := range specs {
		if err := checkKindName(s.Kind); err != nil {
			return nil, err
		}
		if IsBuiltin(s.Kind) {
			return nil, fmt.Errorf("custom kind %q is built in; a host kind needs a name of its own", s.Kind)
		}
		if _, dup := k.specs[s.Kind]; dup {
			return nil, fmt.Errorf("custom kind %q declared twice", s.Kind)
		}
		k.specs[s.Kind] = s
	}
	return k, nil
}

func checkKindName(k Kind) error {
	if k == "" {
		return errors.New("custom kind: empty name")
	}
	if len(k) > MaxKindLen {
		return fmt.Errorf("custom kind: name is longer than %d bytes", MaxKindLen)
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		lower := 'a' <= c && c <= 'z'
		if !lower && (i == 0 || !('0' <= c && c <= '9' || c == '_')) {
			return fmt.Errorf("custom kind %q: want a lower-case letter, then lower-case letters, digits or '_'", k)
		}
	}
	return nil
}

// IsBuiltin reports whether k is one of the runtime's own kinds.
func IsBuiltin(k Kind) bool { return kinds[k] }

// Has reports whether k declares kind.
func (k *Kinds) Has(kind Kind) bool {
	if k == nil {
		return false
	}
	_, ok := k.specs[kind]
	return ok
}

// List returns the declared kinds, sorted.
func (k *Kinds) List() []Kind {
	if k == nil {
		return nil
	}
	out := make([]Kind, 0, len(k.specs))
	for kind := range k.specs {
		out = append(out, kind)
	}
	slices.Sort(out)
	return out
}

// Literal is runtime.Literal for every kind, k's included: a host kind's
// scope must be non-empty and at most MaxScopeLen bytes, and is kept as
// written. It never touches the OS.
func (k *Kinds) Literal(kind Kind, scope string) (Capability, error) {
	if !k.Has(kind) {
		return Literal(kind, scope)
	}
	if scope == "" {
		return Capability{}, fmt.Errorf("%s: empty scope", kind)
	}
	if err := CheckScopeLen(kind, scope); err != nil {
		return Capability{}, err
	}
	return Capability{Kind: kind, Scope: scope}, nil
}

// New is runtime.New for every kind, k's included (which it validates as
// Literal does: a host kind has nothing to canonicalise).
func (k *Kinds) New(kind Kind, scope string) (Capability, error) {
	if !k.Has(kind) {
		return New(kind, scope)
	}
	return k.Literal(kind, scope)
}
