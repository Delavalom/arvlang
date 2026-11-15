package runtime

import (
	"iter"
	"slices"
)

// LineageCaps returns the grants of t and of every task that spawned it:
// t's own first, then its parent's, up to the root's. It is not authority:
// checks use t.Caps alone. It tells the fs effects where a writer could
// tamper with the tree. A spawned task's grant may be nested inside a
// grant only an ancestor holds, and a writer inside that one can swap a
// directory on the way to the task's grant, so the open must start from
// the outermost such directory, as it does for one task holding both
// (review round 3). Attenuation only narrows, so every grant listed is
// covered by one of the root's.
func (t *Task) LineageCaps() []Capability { return slices.Collect(t.Lineage()) }

// Lineage yields what LineageCaps returns, in the same order, without
// copying it: the fs effects walk it on every call.
func (t *Task) Lineage() iter.Seq[Capability] {
	return func(yield func(Capability) bool) {
		for p := t; p != nil; p = p.parent {
			for _, c := range p.Caps.caps {
				if !yield(c) {
					return
				}
			}
		}
	}
}
